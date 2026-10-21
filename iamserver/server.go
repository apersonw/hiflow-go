package iamserver

import (
	"context"
	"crypto/hmac"
	"encoding/json"
	"io"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/apersonw/hiflow-go/hiflow"
)

// Options 目录接口的服务端配置
type Options struct {
	// DirectoryID / Secret HiFlow 管理后台新建目录连接时生成的目录 ID 与目录密钥（必填）
	DirectoryID string
	Secret      string
	// Provider 能力声明中的实现说明（可选，如 "acme-iam 3.2"）
	Provider string
	// MaxBatchSize 批量查询一次最多的 ID 数，默认 200
	MaxBatchSize int
	// MaxManagerLevels 汇报链最多返回几级，默认 5（实现了 Managers 时）
	MaxManagerLevels int
	// RecursiveMembers 部门成员支持 recursive=true；RoleDepartmentScope 角色成员支持 department_id
	RecursiveMembers    bool
	RoleDepartmentScope bool
	// Nonces 防重放的 Nonce 记录，默认进程内存（多实例部署时建议换成 Redis 等共享存储）
	Nonces NonceStore
	// OnError 方法返回非 *Error 的错误（响应 500）时调用，用于记录日志
	OnError func(r *http.Request, err error)
	// Now 当前时间（测试用）
	Now func() time.Time
}

// NonceStore 记录 10 分钟内用过的 Nonce：首次出现返回 true
type NonceStore interface {
	Use(ctx context.Context, nonce string, ttl time.Duration) (bool, error)
}

const (
	maxSkew  = 5 * time.Minute
	nonceTTL = 10 * time.Minute
	maxBody  = 1 << 20
)

type server struct {
	dir  Directory
	opt  Options
	caps capabilities
}

type features struct {
	Search              bool `json:"search"`
	Managers            bool `json:"managers"`
	Departments         bool `json:"departments"`
	RecursiveMembers    bool `json:"recursive_members"`
	Roles               bool `json:"roles"`
	RoleDepartmentScope bool `json:"role_department_scope"`
}

type capabilities struct {
	SpecVersion      string   `json:"spec_version"`
	Provider         string   `json:"provider,omitempty"`
	MaxBatchSize     int      `json:"max_batch_size"`
	MaxManagerLevels int      `json:"max_manager_levels,omitempty"`
	Features         features `json:"features"`
}

// New 目录接口的 http.Handler。能力声明按 dir 实现了哪些可选接口自动生成
func New(dir Directory, opt Options) http.Handler {
	if opt.DirectoryID == "" || opt.Secret == "" {
		panic("iamserver: DirectoryID 与 Secret 必填")
	}
	if opt.MaxBatchSize <= 0 {
		opt.MaxBatchSize = DefaultMaxBatchSize
	}
	if opt.MaxManagerLevels <= 0 {
		opt.MaxManagerLevels = 5
	}
	opt.MaxManagerLevels = min(opt.MaxManagerLevels, MaxManagerLevels)
	if opt.Nonces == nil {
		opt.Nonces = &memoryNonces{seen: map[string]time.Time{}}
	}
	if opt.Now == nil {
		opt.Now = time.Now
	}
	s := &server{dir: dir, opt: opt}
	_, s.caps.Features.Managers = dir.(Managers)
	_, s.caps.Features.Departments = dir.(Departments)
	_, s.caps.Features.Roles = dir.(Roles)
	_, s.caps.Features.Search = dir.(Searcher)
	s.caps.Features.RecursiveMembers = s.caps.Features.Departments && opt.RecursiveMembers
	s.caps.Features.RoleDepartmentScope = s.caps.Features.Roles && opt.RoleDepartmentScope
	s.caps.SpecVersion, s.caps.Provider, s.caps.MaxBatchSize = SpecVersion, opt.Provider, opt.MaxBatchSize
	if s.caps.Features.Managers {
		s.caps.MaxManagerLevels = opt.MaxManagerLevels
	}
	return s
}

func (s *server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	out, err := s.handle(r)
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	if err != nil {
		e := asError(err)
		if e.Status == http.StatusInternalServerError && s.opt.OnError != nil {
			s.opt.OnError(r, err)
		}
		w.WriteHeader(e.Status)
		_ = json.NewEncoder(w).Encode(map[string]string{"code": e.Code, "message": e.Message})
		return
	}
	_ = json.NewEncoder(w).Encode(out)
}

var notImplemented = &Error{Status: http.StatusNotFound, Code: CodeNotImplemented, Message: "未实现该能力"}

func (s *server) handle(r *http.Request) (any, error) {
	body, err := io.ReadAll(io.LimitReader(r.Body, maxBody+1))
	if err != nil || len(body) > maxBody {
		return nil, BadRequest("请求体无效或过大")
	}
	if err := s.verify(r, body); err != nil {
		return nil, err
	}
	ctx, q := r.Context(), r.URL.Query()
	segs := strings.Split(strings.Trim(r.URL.Path, "/"), "/")
	get, post := r.Method == http.MethodGet, r.Method == http.MethodPost
	f := s.caps.Features
	route := func(n int, parts ...string) bool {
		if len(segs) != n {
			return false
		}
		for i, p := range parts {
			if p != "*" && segs[i] != p {
				return false
			}
		}
		return true
	}
	page := func() (PageRequest, error) { return pageOf(q) }

	switch {
	case get && route(1, "capabilities"):
		return s.caps, nil
	case post && route(2, "users", "batch-get"):
		return s.batch(ctx, body)
	case get && route(1, "users"):
		if !f.Search {
			return nil, notImplemented
		}
		p, err := page()
		if err != nil {
			return nil, err
		}
		return nonNilPage(s.dir.(Searcher).SearchUsers(ctx, q.Get("keyword"), q.Get("department_id"), p))
	case get && route(2, "users"):
		return orNotFound(s.dir.User(ctx, segs[1]))
	case get && route(3, "users", "*", "managers"):
		if !f.Managers {
			return nil, notImplemented
		}
		levels := 1
		if v := q.Get("levels"); v != "" {
			n, err := strconv.Atoi(v)
			if err != nil || n < 1 || n > MaxManagerLevels {
				return nil, BadRequest("levels 需在 1 ~ %d 之间", MaxManagerLevels)
			}
			levels = min(n, s.opt.MaxManagerLevels)
		}
		items, err := s.dir.(Managers).Managers(ctx, segs[1], levels)
		if err != nil {
			return nil, err
		}
		if items == nil {
			items = []Manager{}
		}
		return map[string]any{"items": items}, nil
	case get && route(1, "departments"):
		if !f.Departments {
			return nil, notImplemented
		}
		p, err := page()
		if err != nil {
			return nil, err
		}
		return nonNilPage(s.dir.(Departments).Departments(ctx, q.Get("parent_id"), p))
	case get && route(2, "departments"):
		if !f.Departments {
			return nil, notImplemented
		}
		return orNotFound(s.dir.(Departments).Department(ctx, segs[1]))
	case get && route(3, "departments", "*", "members"):
		recursive := q.Get("recursive") == "true"
		if !f.Departments || (recursive && !f.RecursiveMembers) {
			return nil, notImplemented
		}
		p, err := page()
		if err != nil {
			return nil, err
		}
		return nonNilPage(s.dir.(Departments).Members(ctx, segs[1], recursive, p))
	case get && route(1, "roles"):
		if !f.Roles {
			return nil, notImplemented
		}
		p, err := page()
		if err != nil {
			return nil, err
		}
		return nonNilPage(s.dir.(Roles).Roles(ctx, p))
	case get && route(3, "roles", "*", "members"):
		dept := q.Get("department_id")
		if !f.Roles || (dept != "" && !f.RoleDepartmentScope) {
			return nil, notImplemented
		}
		p, err := page()
		if err != nil {
			return nil, err
		}
		return nonNilPage(s.dir.(Roles).RoleMembers(ctx, segs[1], dept, p))
	}
	return nil, NotFound("接口不存在：%s %s", r.Method, r.URL.Path)
}

// orNotFound 方法返回 nil 且没有错误时视为不存在
func orNotFound[T any](v *T, err error) (any, error) {
	if err != nil {
		return nil, err
	}
	if v == nil {
		return nil, ErrNotFound
	}
	return v, nil
}

func nonNilPage[T any](p *Page[T], err error) (any, error) {
	if err != nil {
		return nil, err
	}
	if p == nil {
		p = &Page[T]{}
	}
	if p.Items == nil {
		p.Items = []T{}
	}
	return p, nil
}

func pageOf(q map[string][]string) (PageRequest, error) {
	p := PageRequest{Size: DefaultPageSize}
	if v := q["page_size"]; len(v) > 0 && v[0] != "" {
		n, err := strconv.Atoi(v[0])
		if err != nil || n < 1 || n > MaxPageSize {
			return p, BadRequest("page_size 需在 1 ~ %d 之间", MaxPageSize)
		}
		p.Size = n
	}
	if v := q["page_token"]; len(v) > 0 {
		p.Token = v[0]
	}
	return p, nil
}

func (s *server) batch(ctx context.Context, body []byte) (any, error) {
	var req struct {
		IDs []string `json:"ids"`
	}
	if err := json.Unmarshal(body, &req); err != nil || len(req.IDs) == 0 {
		return nil, BadRequest("ids 不能为空")
	}
	if len(req.IDs) > s.opt.MaxBatchSize {
		return nil, BadRequest("ids 最多 %d 个", s.opt.MaxBatchSize)
	}
	seen := map[string]bool{}
	ids := make([]string, 0, len(req.IDs))
	for _, id := range req.IDs {
		if id != "" && !seen[id] {
			seen[id] = true
			ids = append(ids, id)
		}
	}
	users, err := s.dir.BatchUsers(ctx, ids)
	if err != nil {
		return nil, err
	}
	found := map[string]bool{}
	items := make([]User, 0, len(users))
	for _, u := range users {
		if seen[u.ID] && !found[u.ID] {
			found[u.ID] = true
			items = append(items, u)
		}
	}
	missing := []string{}
	for _, id := range ids {
		if !found[id] {
			missing = append(missing, id)
		}
	}
	return map[string]any{"items": items, "missing_ids": missing}, nil
}

// verify 校验签名、目录 ID、时间戳（±5 分钟）、Nonce（10 分钟内不重复）与请求体摘要
func (s *server) verify(r *http.Request, body []byte) error {
	h := r.Header
	id, ts, nonce, hash, sig := h.Get("X-Client-Id"), h.Get("X-Timestamp"), h.Get("X-Nonce"), h.Get("X-Content-SHA256"), h.Get("X-Signature")
	unauthorized := func(msg string) error {
		return &Error{Status: http.StatusUnauthorized, Code: CodeUnauthorized, Message: msg}
	}
	if id == "" || ts == "" || nonce == "" || hash == "" || sig == "" {
		return unauthorized("缺少签名请求头")
	}
	if id != s.opt.DirectoryID {
		return unauthorized("目录 ID 不匹配")
	}
	sec, err := strconv.ParseInt(ts, 10, 64)
	if err != nil || s.opt.Now().Sub(time.Unix(sec, 0)).Abs() > maxSkew {
		return unauthorized("时间戳无效或已过期")
	}
	if len(nonce) < 16 || len(nonce) > 64 {
		return unauthorized("Nonce 长度需在 16 ~ 64 之间")
	}
	if hiflow.ContentSHA256(body) != hash {
		return unauthorized("请求体摘要不符")
	}
	want := hiflow.Sign(s.opt.Secret, hiflow.SignParams{Method: r.Method, Path: r.URL.Path, Query: r.URL.Query(), ClientID: id, Timestamp: ts, Nonce: nonce, ContentSHA256: hash})
	if !hmac.Equal([]byte(want), []byte(sig)) {
		return unauthorized("签名无效")
	}
	ok, err := s.opt.Nonces.Use(r.Context(), nonce, nonceTTL)
	if err != nil {
		return Unavailable("Nonce 存储不可用")
	}
	if !ok {
		return unauthorized("Nonce 重复")
	}
	return nil
}

type memoryNonces struct {
	mu   sync.Mutex
	seen map[string]time.Time
	last time.Time
}

func (m *memoryNonces) Use(_ context.Context, nonce string, ttl time.Duration) (bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	now := time.Now()
	if now.Sub(m.last) > time.Minute {
		for k, exp := range m.seen {
			if now.After(exp) {
				delete(m.seen, k)
			}
		}
		m.last = now
	}
	if exp, ok := m.seen[nonce]; ok && now.Before(exp) {
		return false, nil
	}
	m.seen[nonce] = now.Add(ttl)
	return true, nil
}
