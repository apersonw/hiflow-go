package hiflow

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
)

// 回调类型：HiFlow 同步回调业务系统（客户端的回调地址），签名格式同 Webhook，密钥为客户端密钥
const (
	// CallbackResolveUsers 处理人规则解析：模型中的 @规则(参数) 推进到节点时回调，响应 ResolveResponse
	CallbackResolveUsers = "resolve.users"
	// CallbackServiceInvoke 服务任务调用：响应 ServiceResponse，失败（非 2xx）时 HiFlow 退避重试
	CallbackServiceInvoke = "service.invoke"
)

// ServiceRequest 服务任务调用请求；同一任务可能被调用多次（重试），请按 TaskID 保证幂等
type ServiceRequest struct {
	Service string         `json:"service"`
	Params  map[string]any `json:"params"`
	NodeID  string         `json:"node_id"`
	TaskID  int64          `json:"task_id"`
	// Attempt 第几次调用（从 1 开始）
	Attempt   int            `json:"attempt"`
	Instance  EventInstance  `json:"instance"`
	Variables map[string]any `json:"variables"`
}

// ServiceResponse 服务任务结果：Variables 合并到流程变量后继续推进
type ServiceResponse struct {
	Variables map[string]any `json:"variables,omitempty"`
}

// Service 解析 service.invoke 回调的数据
func (e *WebhookEvent) Service() (*ServiceRequest, error) {
	if e.Type != CallbackServiceInvoke {
		return nil, fmt.Errorf("hiflow: %s is not %s", e.Type, CallbackServiceInvoke)
	}
	var r ServiceRequest
	if err := json.Unmarshal(e.Data, &r); err != nil {
		return nil, err
	}
	return &r, nil
}

// ResolveRequest 处理人规则解析请求：@leader(${starter}) → Rule "leader"，Args ["u1"]（变量已替换）
type ResolveRequest struct {
	Rule     string        `json:"rule"`
	Args     []string      `json:"args"`
	NodeID   string        `json:"node_id"`
	Instance EventInstance `json:"instance"`
	// Variables 当前流程变量
	Variables map[string]any `json:"variables"`
}

// ResolveResponse 解析结果：处理人规则返回多人时作为候选人，返回零人时推进失败
type ResolveResponse struct {
	Users []string `json:"users"`
}

// Resolve 解析 resolve.users 回调的数据
func (e *WebhookEvent) Resolve() (*ResolveRequest, error) {
	if e.Type != CallbackResolveUsers {
		return nil, fmt.Errorf("hiflow: %s is not %s", e.Type, CallbackResolveUsers)
	}
	var r ResolveRequest
	if err := json.Unmarshal(e.Data, &r); err != nil {
		return nil, err
	}
	return &r, nil
}

// CallbackHandler 返回处理 HiFlow 回调的 http.Handler：校验签名（secret 为客户端密钥）后调用 fn，把返回值编码为 JSON 响应；
// 签名无效返回 401，fn 返回错误时返回 500（HiFlow 的本次推进失败并回滚）。
//
//	http.Handle("/hiflow/callback", hiflow.CallbackHandler(clientSecret, func(r *http.Request, e *hiflow.WebhookEvent) (any, error) {
//		req, err := e.Resolve()
//		if err != nil {
//			return nil, err
//		}
//		switch req.Rule {
//		case "leader":
//			return hiflow.ResolveResponse{Users: []string{leaderOf(req.Args[0])}}, nil
//		}
//		return nil, fmt.Errorf("unknown rule %s", req.Rule)
//	}))
func CallbackHandler(secret string, fn func(r *http.Request, e *WebhookEvent) (any, error)) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		body, err := io.ReadAll(io.LimitReader(r.Body, 1<<20))
		if err != nil {
			http.Error(w, "read body failed", http.StatusBadRequest)
			return
		}
		e, err := VerifyWebhook(secret, r.Header.Get(SignatureHeader), body, 0)
		if err != nil {
			http.Error(w, err.Error(), http.StatusUnauthorized)
			return
		}
		out, err := fn(r, e)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(out)
	})
}
