package hiflow

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"
)

// Webhook 事件类型
const (
	EventInstanceStarted   = "instance.started"
	EventInstanceCompleted = "instance.completed"
	EventInstanceCanceled  = "instance.canceled"
	EventTaskCreated       = "task.created"
	EventTaskCompleted     = "task.completed"
	EventTaskCanceled      = "task.canceled"
	// EventTaskAssigned 任务的处理人变化（认领、转办），Data.Task.Assignee 为新处理人
	EventTaskAssigned = "task.assigned"
	// EventTaskReturned 任务被退回（Comment 为“退回到 <节点>：意见”），目标节点随后产生 task.created
	EventTaskReturned = "task.returned"
	// EventTaskOverdue 待办超时（节点配置 timeout.action = remind，每个待办一次）
	EventTaskOverdue = "task.overdue"
	// EventTaskFailed 服务任务调用失败且重试已用尽（Comment 为失败原因），可在管理后台重试或代为完成
	EventTaskFailed = "task.failed"
	// EventCCCreated 产生抄送记录（每位被抄送人一个），Data.CC 为抄送信息，Data.Task 为触发抄送的任务
	EventCCCreated = "cc.created"
	// EventTaskAssigneeInactive 待办的处理人在组织架构中已停用或不存在（每个待办一次，之后按目录配置交接）
	EventTaskAssigneeInactive = "task.assignee_inactive"
	// EventWebhookTest 管理后台“测试推送”
	EventWebhookTest = "webhook.test"
)

// SignatureHeader 推送签名所在的请求头
const SignatureHeader = "X-HiFlow-Signature"

// DefaultWebhookTolerance 签名时间与本机时间允许的偏差
const DefaultWebhookTolerance = 5 * time.Minute

var (
	// ErrWebhookSignature 签名缺失或不匹配
	ErrWebhookSignature = errors.New("hiflow: invalid webhook signature")
	// ErrWebhookTimestamp 签名时间超出允许偏差（可能是重放）
	ErrWebhookTimestamp = errors.New("hiflow: webhook timestamp outside tolerance")
)

// WebhookEvent HiFlow 推送的事件。同一事件可能重复推送（至少投递一次），请按 ID 去重；不保证顺序，
// 需要最新状态时按 Data.Instance.ID 查询实例详情
type WebhookEvent struct {
	ID        string          `json:"id"`
	Type      string          `json:"type"`
	CreatedAt time.Time       `json:"created_at"`
	Data      json.RawMessage `json:"data"`
}

// EventInstance 事件中的实例摘要（不含流程变量）
type EventInstance struct {
	ID                int64      `json:"id"`
	DefinitionKey     string     `json:"definition_key"`
	DefinitionVersion int        `json:"definition_version"`
	DefinitionName    string     `json:"definition_name"`
	BusinessKey       string     `json:"business_key"`
	Title             string     `json:"title"`
	Starter           string     `json:"starter"`
	Status            string     `json:"status"`
	StartedAt         time.Time  `json:"started_at"`
	EndedAt           *time.Time `json:"ended_at"`
	EndReason         string     `json:"end_reason"`
}

// EventTask 事件中的任务
type EventTask struct {
	ID     int64  `json:"id"`
	NodeID string `json:"node_id"`
	Name   string `json:"name"`
	// Kind user | service | receive
	Kind     string `json:"kind"`
	Assignee string `json:"assignee"`
	// DelegatedFrom 按委托规则交给代理人时的原处理人
	DelegatedFrom string     `json:"delegated_from"`
	Candidates    []string   `json:"candidates"`
	Status        string     `json:"status"`
	Operator      string     `json:"operator"`
	Comment       string     `json:"comment"`
	CreatedAt     time.Time  `json:"created_at"`
	CompletedAt   *time.Time `json:"completed_at"`
}

// EventCC 事件中的抄送
type EventCC struct {
	ID     int64  `json:"id"`
	User   string `json:"user"`
	Sender string `json:"sender"`
	NodeID string `json:"node_id"`
}

// EventData 流程事件的数据：Instance 总是有，Task 在 task.* 与 cc.created 事件中有，CC 只在 cc.created 中有；Operator / Comment 为触发事件的操作者与意见
type EventData struct {
	Instance EventInstance `json:"instance"`
	Task     *EventTask    `json:"task"`
	CC       *EventCC      `json:"cc"`
	Operator string        `json:"operator"`
	Comment  string        `json:"comment"`
}

// Flow 解析流程事件（instance.* / task.* / cc.*）的数据
func (e *WebhookEvent) Flow() (*EventData, error) {
	if !strings.HasPrefix(e.Type, "instance.") && !strings.HasPrefix(e.Type, "task.") && !strings.HasPrefix(e.Type, "cc.") {
		return nil, fmt.Errorf("hiflow: %s is not a flow event", e.Type)
	}
	var d EventData
	if err := json.Unmarshal(e.Data, &d); err != nil {
		return nil, err
	}
	return &d, nil
}

// VerifyWebhook 校验签名头（t={Unix 秒},v1={hex(HMAC-SHA256(secret, "{t}.{body}"))}）与签名时间，
// 通过后解析事件。tolerance 为 0 时使用 DefaultWebhookTolerance。body 须为原始请求体（未经重新序列化）。
func VerifyWebhook(secret, signature string, body []byte, tolerance time.Duration) (*WebhookEvent, error) {
	if tolerance <= 0 {
		tolerance = DefaultWebhookTolerance
	}
	var ts int64
	var sigs [][]byte
	for _, part := range strings.Split(signature, ",") {
		k, v, ok := strings.Cut(strings.TrimSpace(part), "=")
		if !ok {
			continue
		}
		switch k {
		case "t":
			ts, _ = strconv.ParseInt(v, 10, 64)
		case "v1":
			if b, err := hex.DecodeString(v); err == nil {
				sigs = append(sigs, b)
			}
		}
	}
	if ts == 0 || len(sigs) == 0 {
		return nil, ErrWebhookSignature
	}
	mac := hmac.New(sha256.New, []byte(secret))
	fmt.Fprintf(mac, "%d.", ts)
	mac.Write(body)
	expected := mac.Sum(nil)
	matched := false
	for _, s := range sigs {
		matched = hmac.Equal(s, expected) || matched
	}
	if !matched {
		return nil, ErrWebhookSignature
	}
	if d := time.Since(time.Unix(ts, 0)); d > tolerance || d < -tolerance {
		return nil, ErrWebhookTimestamp
	}
	var e WebhookEvent
	if err := json.Unmarshal(body, &e); err != nil {
		return nil, fmt.Errorf("hiflow: decode webhook event: %w", err)
	}
	return &e, nil
}

// WebhookHandler 返回校验签名后调用 fn 的 http.Handler：签名无效返回 401；fn 返回错误时返回 500，HiFlow 会按退避重试。
//
//	http.Handle("/hiflow/events", hiflow.WebhookHandler(secret, func(r *http.Request, e *hiflow.WebhookEvent) error {
//		if e.Type == hiflow.EventTaskCreated { d, _ := e.Flow(); notify(d.Task.Assignee, d.Instance.Title) }
//		return nil
//	}))
func WebhookHandler(secret string, fn func(r *http.Request, e *WebhookEvent) error) http.Handler {
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
		if err := fn(r, e); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	})
}
