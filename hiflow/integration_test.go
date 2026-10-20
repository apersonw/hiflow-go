//go:build integration

// 端到端测试：需 HiFlow 已启动（make run），go test -tags=integration ./...
//
//	HIFLOW_BASE_URL     默认 http://127.0.0.1:8081
//	HIFLOW_ADMIN_API_KEY 管理密钥（用于创建测试客户端与流程模型）
package hiflow

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"sync"
	"testing"
	"time"
)

func baseURL() string {
	if v := os.Getenv("HIFLOW_BASE_URL"); v != "" {
		return v
	}
	return "http://127.0.0.1:8081"
}

// admin 调用管理接口
func admin(t *testing.T, method, path string, body any) json.RawMessage {
	t.Helper()
	key := os.Getenv("HIFLOW_ADMIN_API_KEY")
	if key == "" {
		t.Skip("HIFLOW_ADMIN_API_KEY 未设置")
	}
	b, _ := json.Marshal(body)
	req, _ := http.NewRequest(method, baseURL()+path, bytes.NewReader(b))
	req.Header.Set("X-Admin-Key", key)
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var env envelope
	if err := json.NewDecoder(resp.Body).Decode(&env); err != nil || env.Code != 0 {
		t.Fatalf("%s %s: code=%d %s %v", method, path, env.Code, env.Message, err)
	}
	return env.Data
}

const leaveModel = `{
  "nodes": [
    {"id": "start", "type": "start", "name": "开始"},
    {"id": "manager", "type": "user_task", "name": "经理审批", "assignee": "${manager}"},
    {"id": "gw", "type": "exclusive_gateway", "name": "审批结果"},
    {"id": "hr", "type": "user_task", "name": "人事备案", "candidates": ["hr1", "hr2"]},
    {"id": "approved", "type": "end", "name": "通过"},
    {"id": "rejected", "type": "end", "name": "驳回"}
  ],
  "edges": [
    {"id": "e1", "source": "start", "target": "manager"},
    {"id": "e2", "source": "manager", "target": "gw"},
    {"id": "e3", "source": "gw", "target": "hr", "condition": "approved && days > 3"},
    {"id": "e4", "source": "gw", "target": "rejected", "condition": "!approved"},
    {"id": "e5", "source": "gw", "target": "approved", "default": true},
    {"id": "e6", "source": "hr", "target": "approved"}
  ]
}`

const countersignModel = `{
  "nodes": [
    {"id": "start", "type": "start"},
    {"id": "fork", "type": "parallel_gateway"},
    {"id": "legal", "type": "user_task", "name": "法务", "assignee": "legal"},
    {"id": "finance", "type": "user_task", "name": "财务", "assignee": "finance"},
    {"id": "join", "type": "parallel_gateway"},
    {"id": "end", "type": "end"}
  ],
  "edges": [
    {"id": "e1", "source": "start", "target": "fork"},
    {"id": "e2", "source": "fork", "target": "legal"},
    {"id": "e3", "source": "fork", "target": "finance"},
    {"id": "e4", "source": "legal", "target": "join"},
    {"id": "e5", "source": "finance", "target": "join"},
    {"id": "e6", "source": "join", "target": "end"}
  ]
}`

func setup(t *testing.T) *Client {
	c, _ := setupClient(t)
	return c
}

// setupClient 创建测试客户端并发布流程模型，返回 SDK 客户端与客户端主键（管理接口使用）
func setupClient(t *testing.T) (*Client, int64) {
	t.Helper()
	var client struct {
		ID           int64  `json:"id"`
		ClientID     string `json:"client_id"`
		ClientSecret string `json:"client_secret"`
	}
	_ = json.Unmarshal(admin(t, http.MethodPost, "/admin/v1/clients", map[string]any{"client_name": "sdk-go-it-" + time.Now().Format("150405.000")}), &client)
	for key, content := range map[string]string{"leave": leaveModel, "countersign": countersignModel} {
		var m struct{ ID int64 }
		_ = json.Unmarshal(admin(t, http.MethodPost, "/admin/v1/models", map[string]any{
			"client_id": client.ID, "key": key, "name": key, "content": json.RawMessage(content),
		}), &m)
		admin(t, http.MethodPost, fmt.Sprintf("/admin/v1/models/%d/publish", m.ID), nil)
	}
	c, err := New(baseURL(), client.ClientID, client.ClientSecret)
	if err != nil {
		t.Fatal(err)
	}
	return c, client.ID
}

func TestIntegration(t *testing.T) {
	c := setup(t)
	ctx := context.Background()

	def, err := c.Definition(ctx, "leave")
	if err != nil || def.Version != 1 {
		t.Fatalf("definition = %+v, err = %v", def, err)
	}

	t.Run("approve then hr claims", func(t *testing.T) {
		in, err := c.Start(ctx, &StartRequest{Key: "leave", BusinessKey: "L-1", Starter: "u1", Variables: map[string]any{"manager": "boss", "days": 5}})
		if err != nil {
			t.Fatal(err)
		}
		todo, err := c.Todo(ctx, "boss", nil)
		if err != nil || todo.Total != 1 || todo.List[0].BusinessKey != "L-1" {
			t.Fatalf("todo = %+v, err = %v", todo, err)
		}
		task := todo.List[0]
		if _, err := c.Complete(ctx, task.ID, &CompleteRequest{Operator: "intruder"}); !IsCode(err, CodeNotAssignee) {
			t.Fatalf("intruder: %v", err)
		}
		in, err = c.Complete(ctx, task.ID, &CompleteRequest{Operator: "boss", Comment: "同意", Variables: map[string]any{"approved": true}})
		if err != nil {
			t.Fatal(err)
		}
		if _, err := c.Complete(ctx, task.ID, &CompleteRequest{Operator: "boss"}); !IsCode(err, CodeTaskNotActive) {
			t.Fatalf("complete twice: %v", err)
		}
		hr := in.ActiveTasks()
		if len(hr) != 1 || hr[0].NodeID != "hr" || hr[0].Assignee != "" {
			t.Fatalf("hr task = %+v", hr)
		}
		if page, _ := c.Todo(ctx, "hr2", nil); page.Total != 1 {
			t.Fatalf("hr2 todo = %d", page.Total)
		}
		claimed, err := c.Claim(ctx, hr[0].ID, "hr2")
		if err != nil || claimed.Assignee != "hr2" {
			t.Fatalf("claim = %+v, %v", claimed, err)
		}
		if page, _ := c.Todo(ctx, "hr1", nil); page.Total != 0 {
			t.Fatalf("hr1 should lose the task after claim, todo = %d", page.Total)
		}
		in, err = c.Complete(ctx, hr[0].ID, &CompleteRequest{Operator: "hr2"})
		if err != nil || in.Status != InstanceCompleted || in.EndedAt == nil {
			t.Fatalf("final = %+v, %v", in, err)
		}
		if list, _ := c.Instances(ctx, &InstanceQuery{BusinessKey: "L-1"}); list.Total != 1 {
			t.Fatalf("instances by business key = %d", list.Total)
		}
	})

	t.Run("reject", func(t *testing.T) {
		in, _ := c.Start(ctx, &StartRequest{Key: "leave", Starter: "u2", Variables: map[string]any{"manager": "boss", "days": 1}})
		in, err := c.Complete(ctx, in.ActiveTasks()[0].ID, &CompleteRequest{Operator: "boss", Variables: map[string]any{"approved": false}})
		if err != nil || in.Status != InstanceCompleted {
			t.Fatalf("reject = %+v, %v", in, err)
		}
		if in.Variables["approved"] != false {
			t.Fatalf("variables = %v", in.Variables)
		}
	})

	t.Run("transfer and cancel", func(t *testing.T) {
		in, _ := c.Start(ctx, &StartRequest{Key: "leave", Starter: "u3", Variables: map[string]any{"manager": "boss"}})
		task := in.ActiveTasks()[0]
		moved, err := c.Transfer(ctx, task.ID, &TransferRequest{Operator: "boss", To: "deputy"})
		if err != nil || moved.Assignee != "deputy" {
			t.Fatalf("transfer = %+v, %v", moved, err)
		}
		in, err = c.Cancel(ctx, in.ID, &CancelRequest{Operator: "u3", Reason: "不请了"})
		if err != nil || in.Status != InstanceCanceled || in.Tasks[0].Status != TaskCanceled {
			t.Fatalf("cancel = %+v, %v", in, err)
		}
		if _, err := c.Cancel(ctx, in.ID, nil); !IsCode(err, CodeInstanceEnded) {
			t.Fatalf("cancel twice: %v", err)
		}
	})

	t.Run("countersign concurrently", func(t *testing.T) {
		in, err := c.Start(ctx, &StartRequest{Key: "countersign"})
		if err != nil || len(in.ActiveTasks()) != 2 {
			t.Fatalf("start = %+v, %v", in, err)
		}
		// 两个分支同时完成：实例行锁保证汇聚计数一致，最终恰好结束一次
		var wg sync.WaitGroup
		errs := make(chan error, 2)
		for _, task := range in.ActiveTasks() {
			wg.Add(1)
			go func(task Task) {
				defer wg.Done()
				_, err := c.Complete(ctx, task.ID, &CompleteRequest{Operator: task.Assignee})
				errs <- err
			}(task)
		}
		wg.Wait()
		close(errs)
		for err := range errs {
			if err != nil {
				t.Fatal(err)
			}
		}
		final, _ := c.Instance(ctx, in.ID)
		if final.Status != InstanceCompleted {
			t.Fatalf("status = %s", final.Status)
		}
		ends := 0
		for _, e := range final.Events {
			if e.Type == "instance_completed" {
				ends++
			}
		}
		if ends != 1 {
			t.Fatalf("instance_completed events = %d", ends)
		}
	})

	t.Run("errors", func(t *testing.T) {
		if _, err := c.Start(ctx, &StartRequest{Key: "missing"}); !IsNotFound(err) {
			t.Fatalf("missing key: %v", err)
		}
		if _, err := c.Instance(ctx, 1<<40); !IsNotFound(err) {
			t.Fatalf("missing instance: %v", err)
		}
		bad, _ := New(baseURL(), c.clientID, "wrong-secret")
		if _, err := bad.Tasks(ctx, nil); !IsCode(err, CodeSignature) {
			t.Fatalf("wrong secret: %v", err)
		}
	})
}

// 服务端推送的事件经 WebhookHandler 校验签名后到达（需要服务端能访问本机的测试接收端）
func TestIntegrationWebhook(t *testing.T) {
	c, clientPK := setupClient(t)
	ctx := context.Background()

	got := make(chan *WebhookEvent, 16)
	var secret string
	var ready sync.WaitGroup
	ready.Add(1)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ready.Wait()
		WebhookHandler(secret, func(_ *http.Request, e *WebhookEvent) error {
			got <- e
			return nil
		}).ServeHTTP(w, r)
	}))
	defer srv.Close()

	var hook struct {
		ID     int64  `json:"id"`
		Secret string `json:"secret"`
	}
	_ = json.Unmarshal(admin(t, http.MethodPost, "/admin/v1/webhooks", map[string]any{
		"client_id": clientPK, "url": srv.URL, "events": []string{EventTaskCreated, EventInstanceCompleted},
	}), &hook)
	secret = hook.Secret
	ready.Done()

	in, err := c.Start(ctx, &StartRequest{Key: "leave", BusinessKey: "W-1", Variables: map[string]any{"manager": "boss"}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := c.Complete(ctx, in.ActiveTasks()[0].ID, &CompleteRequest{Operator: "boss", Variables: map[string]any{"approved": false}}); err != nil {
		t.Fatal(err)
	}

	var types []string
	timeout := time.After(10 * time.Second)
	for len(types) < 2 {
		select {
		case e := <-got:
			d, err := e.Flow()
			if err != nil || d.Instance.ID != in.ID || d.Instance.BusinessKey != "W-1" {
				t.Fatalf("event %s data = %+v, %v", e.Type, d, err)
			}
			if e.Type == EventTaskCreated && (d.Task == nil || d.Task.Assignee != "boss") {
				t.Fatalf("task.created task = %+v", d.Task)
			}
			types = append(types, e.Type)
		case <-timeout:
			t.Fatalf("timed out waiting for webhook events, got %v", types)
		}
	}
	// 投递并发进行，不保证顺序
	if !(types[0] == EventTaskCreated && types[1] == EventInstanceCompleted) && !(types[1] == EventTaskCreated && types[0] == EventInstanceCompleted) {
		t.Fatalf("events = %v", types)
	}
}

const reviewModel = `{
  "nodes": [
    {"id": "start", "type": "start"},
    {"id": "submit", "type": "user_task", "name": "提交", "assignee": "${starter}"},
    {"id": "sign", "type": "user_task", "name": "会签", "multi": {"users": ["${signers}"], "mode": "all", "reject_on": true}},
    {"id": "gw", "type": "exclusive_gateway"},
    {"id": "ok", "type": "end"},
    {"id": "no", "type": "end"}
  ],
  "edges": [
    {"id": "1", "source": "start", "target": "submit"},
    {"id": "2", "source": "submit", "target": "sign"},
    {"id": "3", "source": "sign", "target": "gw"},
    {"id": "4", "source": "gw", "target": "ok", "condition": "approved"},
    {"id": "5", "source": "gw", "target": "no", "default": true}
  ]
}`

func TestIntegrationM2(t *testing.T) {
	c, clientPK := setupClient(t)
	ctx := context.Background()
	var m struct{ ID int64 }
	_ = json.Unmarshal(admin(t, http.MethodPost, "/admin/v1/models", map[string]any{
		"client_id": clientPK, "key": "review", "name": "评审", "content": json.RawMessage(reviewModel),
	}), &m)
	admin(t, http.MethodPost, fmt.Sprintf("/admin/v1/models/%d/publish", m.ID), nil)
	vars := map[string]any{"starter": "u1", "signers": []any{"s1", "s2"}}

	// 撤回：还没有人处理
	in, err := c.Start(ctx, &StartRequest{Key: "review", Starter: "u1", Variables: vars})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := c.Recall(ctx, in.ID, "u2"); !IsCode(err, CodeNotAssignee) {
		t.Fatalf("recall by other: %v", err)
	}
	in, err = c.Recall(ctx, in.ID, "u1")
	if err != nil || in.Status != InstanceCanceled || in.EndReason != "发起人撤回" {
		t.Fatalf("recall = %+v, %v", in, err)
	}

	// 会签中 s1 退回到提交，重新提交后会签通过
	in, _ = c.Start(ctx, &StartRequest{Key: "review", Starter: "u1", Variables: vars})
	in, _ = c.Complete(ctx, in.ActiveTasks()[0].ID, &CompleteRequest{Operator: "u1"})
	if _, err := c.Recall(ctx, in.ID, "u1"); !IsCode(err, CodeRecallNotAllowed) {
		t.Fatalf("recall after submit: %v", err)
	}
	sign := in.ActiveTasks()
	if len(sign) != 2 || sign[0].MultiKey == "" || sign[0].MultiKey != sign[1].MultiKey {
		t.Fatalf("countersign tasks = %+v", sign)
	}
	targets, err := c.ReturnTargets(ctx, sign[0].ID)
	if err != nil || len(targets) != 1 || targets[0].NodeID != "submit" || targets[0].Operator != "u1" {
		t.Fatalf("return targets = %+v, %v", targets, err)
	}
	if _, err := c.Return(ctx, sign[0].ID, &ReturnRequest{Operator: sign[0].Assignee, TargetNode: "sign"}); !IsCode(err, CodeReturnTarget) {
		t.Fatalf("return to unvisited node: %v", err)
	}
	in, err = c.Return(ctx, sign[0].ID, &ReturnRequest{Operator: sign[0].Assignee, TargetNode: "submit", Comment: "请补充预算"})
	if err != nil || len(in.ActiveTasks()) != 1 || in.ActiveTasks()[0].NodeID != "submit" {
		t.Fatalf("return = %+v, %v", in.ActiveTasks(), err)
	}
	in, _ = c.Complete(ctx, in.ActiveTasks()[0].ID, &CompleteRequest{Operator: "u1"})
	for _, task := range in.ActiveTasks() {
		in, err = c.Complete(ctx, task.ID, &CompleteRequest{Operator: task.Assignee, Variables: map[string]any{"approved": true}})
		if err != nil {
			t.Fatal(err)
		}
	}
	result, _ := in.Variables["sign_result"].(map[string]any)
	if in.Status != InstanceCompleted || in.Variables["approved"] != true || result["approved"] != float64(2) {
		t.Fatalf("final = %s %v", in.Status, in.Variables)
	}
}
