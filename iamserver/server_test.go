package iamserver

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"testing"
	"time"

	"github.com/apersonw/hiflow-go/hiflow"
)

const (
	testID     = "hfdir_demo"
	testSecret = "demo-secret"
)

// 对接指南中的签名示例向量（HiFlow 服务端与契约自检共用）
func TestSharedSignatureVector(t *testing.T) {
	got := hiflow.Sign(testSecret, hiflow.SignParams{
		Method: "GET", Path: "/users/u1/managers", Query: url.Values{"levels": {"2"}}, ClientID: testID,
		Timestamp: "1700000000", Nonce: "0123456789abcdef0123456789abcdef", ContentSHA256: hiflow.ContentSHA256(nil),
	})
	if got != "5jQothWde7V1E7buK/4g4BsnOG+suyNeoY4KXKohl0o=" {
		t.Fatalf("signature = %s", got)
	}
}

func testData() *StaticData {
	var d StaticData
	_ = json.Unmarshal([]byte(`{
		"users":[
			{"id":"ceo","name":"CEO","department_ids":["hq"]},
			{"id":"mgr","name":"经理","manager_id":"ceo","department_ids":["sales"],"status":"disabled"},
			{"id":"u1","name":"张三","manager_id":"mgr","department_ids":["east"]},
			{"id":"u2","name":"李四","manager_id":"mgr","department_ids":["east"]},
			{"id":"u3","name":"王五","manager_id":"ceo","department_ids":["sales"]}],
		"departments":[
			{"id":"hq","name":"总部","head_user_ids":["ceo"]},
			{"id":"sales","name":"销售","parent_id":"hq","head_user_ids":["mgr"]},
			{"id":"east","name":"华东","parent_id":"sales"}],
		"roles":[{"code":"fin","name":"财务","members":["u2","u3","mgr"]}]}`), &d)
	return &d
}

type env struct {
	t   *testing.T
	srv *httptest.Server
	now time.Time
}

func newEnv(t *testing.T, dir Directory, opt Options) *env {
	e := &env{t: t, now: time.Now()}
	opt.DirectoryID, opt.Secret = testID, testSecret
	opt.Now = func() time.Time { return e.now }
	mux := http.NewServeMux()
	mux.Handle("/hiflow/v1/", http.StripPrefix("/hiflow/v1", New(dir, opt)))
	e.srv = httptest.NewServer(mux)
	t.Cleanup(e.srv.Close)
	return e
}

var nonceSeq int

// call 按 HiFlow 的方式签名请求；tamper 在签名后修改请求
func (e *env) call(method, path string, query url.Values, body any, tamper func(*http.Request)) (int, map[string]any) {
	e.t.Helper()
	var raw []byte
	if body != nil {
		raw, _ = json.Marshal(body)
	}
	nonceSeq++
	ts, nonce := strconv.FormatInt(e.now.Unix(), 10), fmt.Sprintf("nonce-%026d", nonceSeq)
	hash := hiflow.ContentSHA256(raw)
	u := e.srv.URL + "/hiflow/v1" + path
	if len(query) > 0 {
		u += "?" + query.Encode()
	}
	req, _ := http.NewRequest(method, u, bytes.NewReader(raw))
	req.Header.Set("X-Client-Id", testID)
	req.Header.Set("X-Timestamp", ts)
	req.Header.Set("X-Nonce", nonce)
	req.Header.Set("X-Content-SHA256", hash)
	req.Header.Set("X-Signature", hiflow.Sign(testSecret, hiflow.SignParams{Method: method, Path: path, Query: query, ClientID: testID, Timestamp: ts, Nonce: nonce, ContentSHA256: hash}))
	if tamper != nil {
		tamper(req)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		e.t.Fatal(err)
	}
	defer resp.Body.Close()
	var out map[string]any
	_ = json.NewDecoder(resp.Body).Decode(&out)
	return resp.StatusCode, out
}

func TestAuth(t *testing.T) {
	e := newEnv(t, NewStatic(testData()), Options{})
	if st, _ := e.call("GET", "/capabilities", nil, nil, nil); st != 200 {
		t.Fatalf("signed request = %d", st)
	}
	cases := map[string]func(*http.Request){
		"no signature":  func(r *http.Request) { r.Header.Del("X-Signature") },
		"bad signature": func(r *http.Request) { r.Header.Set("X-Signature", "AAAA") },
		"wrong id":      func(r *http.Request) { r.Header.Set("X-Client-Id", "other") },
		"stale": func(r *http.Request) {
			r.Header.Set("X-Timestamp", strconv.FormatInt(time.Now().Add(-time.Hour).Unix(), 10))
		},
		"body tampered": func(r *http.Request) { r.Header.Set("X-Content-SHA256", hiflow.ContentSHA256([]byte("x"))) },
	}
	for name, tamper := range cases {
		if st, body := e.call("GET", "/capabilities", nil, nil, tamper); st != 401 || body["code"] != CodeUnauthorized {
			t.Errorf("%s: %d %v", name, st, body)
		}
	}
	// 重放
	var replay *http.Request
	e.call("GET", "/users/u1", nil, nil, func(r *http.Request) { replay = r.Clone(context.Background()) })
	resp, err := http.DefaultClient.Do(replay)
	if err != nil || resp.StatusCode != 401 {
		t.Fatalf("replay = %v %v", resp.StatusCode, err)
	}
	resp.Body.Close()
}

func TestStaticDirectory(t *testing.T) {
	e := newEnv(t, NewStatic(testData()), Options{RecursiveMembers: true, RoleDepartmentScope: true, MaxManagerLevels: 3})
	_, caps := e.call("GET", "/capabilities", nil, nil, nil)
	f := caps["features"].(map[string]any)
	for _, k := range []string{"search", "managers", "departments", "recursive_members", "roles", "role_department_scope"} {
		if f[k] != true {
			t.Errorf("feature %s = %v", k, f[k])
		}
	}
	if caps["max_manager_levels"].(float64) != 3 || caps["spec_version"] != SpecVersion {
		t.Errorf("caps = %v", caps)
	}
	if st, u := e.call("GET", "/users/mgr", nil, nil, nil); st != 200 || u["status"] != StatusDisabled {
		t.Errorf("disabled user = %d %v", st, u)
	}
	if st, b := e.call("GET", "/users/nobody", nil, nil, nil); st != 404 || b["code"] != CodeNotFound {
		t.Errorf("missing user = %d %v", st, b)
	}
	_, batch := e.call("POST", "/users/batch-get", nil, map[string]any{"ids": []string{"u1", "u1", "x"}}, nil)
	if len(batch["items"].([]any)) != 1 || len(batch["missing_ids"].([]any)) != 1 {
		t.Errorf("batch = %v", batch)
	}
	// 停用的直属上级跳过但计入级数
	_, ms := e.call("GET", "/users/u1/managers", url.Values{"levels": {"5"}}, nil, nil)
	items := ms["items"].([]any)
	if len(items) != 1 || items[0].(map[string]any)["level"].(float64) != 2 {
		t.Errorf("managers = %v", ms)
	}
	// 分页：含下级的成员逐页读取
	var ids []string
	token := ""
	for i := 0; i < 5; i++ {
		q := url.Values{"recursive": {"true"}, "page_size": {"1"}}
		if token != "" {
			q.Set("page_token", token)
		}
		_, page := e.call("GET", "/departments/sales/members", q, nil, nil)
		for _, it := range page["items"].([]any) {
			ids = append(ids, it.(map[string]any)["id"].(string))
		}
		if token, _ = page["next_page_token"].(string); token == "" {
			break
		}
	}
	if fmt.Sprint(ids) != "[u1 u2 u3]" {
		t.Errorf("recursive members = %v", ids)
	}
	_, rm := e.call("GET", "/roles/fin/members", url.Values{"department_id": {"east"}}, nil, nil)
	if len(rm["items"].([]any)) != 1 {
		t.Errorf("scoped role members = %v", rm)
	}
	if st, b := e.call("GET", "/departments", url.Values{"page_size": {"0"}}, nil, nil); st != 400 || b["code"] != CodeBadRequest {
		t.Errorf("bad page_size = %d %v", st, b)
	}
	if st, _ := e.call("GET", "/departments", url.Values{"page_token": {"bogus"}}, nil, nil); st != 400 {
		t.Errorf("bad page_token = %d", st)
	}
}

// minimal 只实现必须的接口
type minimal struct{}

func (minimal) User(_ context.Context, id string) (*User, error) {
	if id == "u1" {
		return &User{ID: "u1", Name: "张三", Status: StatusActive}, nil
	}
	return nil, nil
}
func (minimal) BatchUsers(context.Context, []string) ([]User, error) {
	return nil, fmt.Errorf("db down")
}

func TestMinimalDirectory(t *testing.T) {
	var logged error
	e := newEnv(t, minimal{}, Options{RecursiveMembers: true, OnError: func(_ *http.Request, err error) { logged = err }})
	_, caps := e.call("GET", "/capabilities", nil, nil, nil)
	for k, v := range caps["features"].(map[string]any) {
		if v != false {
			t.Errorf("feature %s should be off", k)
		}
	}
	for _, p := range []string{"/users", "/users/u1/managers", "/departments", "/departments/d/members", "/roles", "/roles/r/members"} {
		if st, b := e.call("GET", p, nil, nil, nil); st != 404 || b["code"] != CodeNotImplemented {
			t.Errorf("%s = %d %v", p, st, b)
		}
	}
	if st, _ := e.call("GET", "/users/u2", nil, nil, nil); st != 404 {
		t.Errorf("nil user should be 404, got %d", st)
	}
	if st, b := e.call("POST", "/users/batch-get", nil, map[string]any{"ids": []string{"u1"}}, nil); st != 500 || b["code"] != CodeInternal || logged == nil {
		t.Errorf("internal error = %d %v %v", st, b, logged)
	}
}
