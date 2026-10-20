package hiflow

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

const (
	testID     = "hf_test"
	testSecret = "test-secret"
)

// fakeServer 校验签名后交给 handler；handler 返回 data 或 *Error
func fakeServer(t *testing.T, handler func(r *http.Request, body []byte) (any, *Error)) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		hash := r.Header.Get(headerContentSHA256)
		if hash != hashHex(body) {
			t.Errorf("content hash mismatch")
		}
		// 模拟反向代理剥掉 /api 前缀：签名的是服务端看到的路径
		sts := stringToSign(r.Method, strings.TrimPrefix(r.URL.Path, "/api"), r.URL.Query(), r.Header.Get(headerClientID), r.Header.Get(headerTimestamp), r.Header.Get(headerNonce), hash)
		if r.Header.Get(headerSignature) != sign(testSecret, sts) {
			w.WriteHeader(http.StatusUnauthorized)
			_ = json.NewEncoder(w).Encode(map[string]any{"code": CodeSignature, "message": "签名校验失败"})
			return
		}
		data, e := handler(r, body)
		w.Header().Set("Content-Type", "application/json")
		if e != nil {
			w.WriteHeader(e.StatusCode)
			_ = json.NewEncoder(w).Encode(map[string]any{"code": e.Code, "message": e.Message, "request_id": "rid", "timestamp": time.Now().UnixMilli()})
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"code": 0, "message": "ok", "data": data})
	}))
	t.Cleanup(srv.Close)
	return srv
}

func newTestClient(t *testing.T, srv *httptest.Server, opts ...Option) *Client {
	t.Helper()
	// 带反向代理前缀：前缀不参与签名
	c, err := New(srv.URL+"/api", testID, testSecret, opts...)
	if err != nil {
		t.Fatal(err)
	}
	return c
}

func TestStartAndComplete(t *testing.T) {
	srv := fakeServer(t, func(r *http.Request, body []byte) (any, *Error) {
		switch r.URL.Path {
		case "/api/open/v1/instances":
			var req StartRequest
			if err := json.Unmarshal(body, &req); err != nil || req.Key != "leave" || req.Variables["days"] != float64(3) {
				t.Errorf("start body = %s", body)
			}
			return map[string]any{"id": 7, "status": "running", "definition_key": "leave",
				"tasks": []any{map[string]any{"id": 11, "status": "active", "assignee": "u7", "node_id": "manager"}}}, nil
		case "/api/open/v1/tasks/11/complete":
			var req CompleteRequest
			_ = json.Unmarshal(body, &req)
			if req.Operator != "u7" || req.Variables["approved"] != true {
				t.Errorf("complete body = %s", body)
			}
			return map[string]any{"id": 7, "status": "completed"}, nil
		}
		return nil, &Error{StatusCode: 404, Code: CodeNotFound, Message: "接口不存在"}
	})
	c := newTestClient(t, srv)
	ctx := context.Background()
	in, err := c.Start(ctx, &StartRequest{Key: "leave", Starter: "u1", Variables: map[string]any{"days": 3}})
	if err != nil {
		t.Fatal(err)
	}
	if in.ID != 7 || !in.Running() || len(in.ActiveTasks()) != 1 {
		t.Fatalf("instance = %+v", in)
	}
	done, err := c.Complete(ctx, in.ActiveTasks()[0].ID, &CompleteRequest{Operator: "u7", Variables: map[string]any{"approved": true}})
	if err != nil {
		t.Fatal(err)
	}
	if done.Status != InstanceCompleted {
		t.Fatalf("status = %s", done.Status)
	}
}

func TestTodoQuery(t *testing.T) {
	srv := fakeServer(t, func(r *http.Request, _ []byte) (any, *Error) {
		q := r.URL.Query()
		if q.Get("user") != "张 三" || q.Get("status") != TaskActive || q.Get("page_size") != "50" {
			t.Errorf("query = %v", q)
		}
		return map[string]any{"list": []any{map[string]any{"id": 1, "name": "经理审批"}}, "total": 1, "page": 1, "page_size": 50}, nil
	})
	page, err := newTestClient(t, srv).Todo(context.Background(), "张 三", &TaskQuery{PageSize: 50, Status: "completed"})
	if err != nil {
		t.Fatal(err)
	}
	if page.Total != 1 || page.List[0].Name != "经理审批" {
		t.Fatalf("page = %+v", page)
	}
}

func TestErrorDecoding(t *testing.T) {
	srv := fakeServer(t, func(*http.Request, []byte) (any, *Error) {
		return nil, &Error{StatusCode: 403, Code: CodeNotAssignee, Message: "当前用户不是任务的处理人"}
	})
	_, err := newTestClient(t, srv).Complete(context.Background(), 1, &CompleteRequest{Operator: "x"})
	var e *Error
	if !errors.As(err, &e) || e.Code != CodeNotAssignee || e.StatusCode != 403 || e.RequestID != "rid" {
		t.Fatalf("err = %v", err)
	}
	if !IsCode(err, CodeNotAssignee) || IsNotFound(err) {
		t.Fatal("IsCode mismatch")
	}
}

func TestRetryOnlyGet(t *testing.T) {
	var calls atomic.Int32
	srv := fakeServer(t, func(r *http.Request, _ []byte) (any, *Error) {
		if calls.Add(1) == 1 {
			return nil, &Error{StatusCode: 503, Code: CodeDependency, Message: "暂不可用"}
		}
		return map[string]any{"id": 3}, nil
	})
	c := newTestClient(t, srv)
	c.backoff = time.Millisecond
	if _, err := c.Task(context.Background(), 3); err != nil || calls.Load() != 2 {
		t.Fatalf("GET retry: err=%v calls=%d", err, calls.Load())
	}
	// 写操作不重试，避免重复发起
	calls.Store(0)
	if _, err := c.Start(context.Background(), &StartRequest{Key: "k"}); !IsCode(err, CodeDependency) || calls.Load() != 1 {
		t.Fatalf("POST should not retry: err=%v calls=%d", err, calls.Load())
	}
}

func TestClockSkew(t *testing.T) {
	serverNow := time.Now().Add(time.Hour)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ts, _ := strconv.ParseInt(r.Header.Get(headerTimestamp), 10, 64)
		if d := serverNow.Sub(time.Unix(ts, 0)); d > time.Minute || d < -time.Minute {
			w.WriteHeader(http.StatusUnauthorized)
			_ = json.NewEncoder(w).Encode(map[string]any{"code": CodeTimestampExpired, "message": "请求时间戳已过期", "timestamp": serverNow.UnixMilli()})
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"code": 0, "data": map[string]any{"id": 1, "key": "leave"}})
	}))
	defer srv.Close()
	c, _ := New(srv.URL, testID, testSecret)
	// 写操作同样在校准时钟后重试一次（超窗请求在验签前即被拒绝，不会重复执行）
	d, err := c.Definition(context.Background(), "leave")
	if err != nil || d.Key != "leave" {
		t.Fatalf("definition = %+v, err = %v", d, err)
	}
}

func TestNewValidation(t *testing.T) {
	for _, base := range []string{"", "ftp://x", "http://"} {
		if _, err := New(base, testID, testSecret); err == nil {
			t.Errorf("base %q should fail", base)
		}
	}
	if _, err := New("http://x", "", testSecret); err == nil {
		t.Error("empty id should fail")
	}
}
