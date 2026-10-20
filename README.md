# HiFlow Go SDK

HiFlow 流程引擎 Open API 的 Go 客户端：请求签名、发起流程、待办查询与处理。只依赖标准库，Go 1.23+。

```bash
go get github.com/apersonw/hiflow-go
```

```go
import "github.com/apersonw/hiflow-go/hiflow"

c, err := hiflow.New("https://flow.example.com/api", os.Getenv("HIFLOW_CLIENT_ID"), os.Getenv("HIFLOW_CLIENT_SECRET"))

// 发起：key 为管理后台中流程模型的标识，按最新发布的版本执行
in, err := c.Start(ctx, &hiflow.StartRequest{
	Key: "leave", BusinessKey: "L-1001", Starter: "u42",
	Variables: map[string]any{"days": 3, "manager": "u7"},
})

// 待办：处理人是 u7，或未指定处理人且 u7 在候选人中
todo, err := c.Todo(ctx, "u7", nil)

// 处理：审批结果写入流程变量，由排他网关的条件决定走向
in, err = c.Complete(ctx, todo.List[0].ID, &hiflow.CompleteRequest{
	Operator: "u7", Comment: "同意", Variables: map[string]any{"approved": true},
})
```

| 方法 | 说明 |
|---|---|
| `Definition(ctx, key)` | 流程定义的最新版本（含流程图） |
| `Start(ctx, req)` | 发起流程，返回实例详情（含生成的待办） |
| `Instance(ctx, id)` / `Instances(ctx, q)` | 实例详情（含任务与轨迹）/ 分页查询 |
| `Cancel(ctx, id, req)` | 撤回 / 终止实例，进行中的任务一并取消 |
| `Tasks(ctx, q)` / `Todo(ctx, user, q)` / `Task(ctx, id)` | 任务查询 / 用户待办 / 任务详情 |
| `Complete(ctx, taskID, req)` | 完成任务，流程继续推进 |
| `Claim(ctx, taskID, user)` | 认领候选任务 |
| `ReturnTargets(ctx, taskID)` / `Return(ctx, taskID, req)` | 可退回的节点 / 退回到处理过的节点 |
| `Recall(ctx, id, operator)` | 发起人撤回（还没有任务被处理时） |
| `Transfer(ctx, taskID, req)` | 转办 |

## 接收事件（Webhook）

管理后台为客户端配置 Webhook 后，HiFlow 在任务创建 / 完成 / 取消 / 指派 / 退回 / 超时、实例发起 / 结束 / 取消时推送事件（签名、失败退避重试、至少投递一次）：

```go
http.Handle("/hiflow/events", hiflow.WebhookHandler(os.Getenv("HIFLOW_WEBHOOK_SECRET"), func(r *http.Request, e *hiflow.WebhookEvent) error {
	d, err := e.Flow()
	if err != nil {
		return nil // webhook.test 等非流程事件
	}
	switch e.Type {
	case hiflow.EventTaskCreated:
		notify(d.Task.Assignee, d.Instance.Title) // 待办提醒
	case hiflow.EventInstanceCompleted:
		markApproved(d.Instance.BusinessKey)
	}
	return nil // 返回错误时 HiFlow 稍后重试；按 e.ID 去重
}))
```

自行处理请求时用 `hiflow.VerifyWebhook(secret, r.Header.Get(hiflow.SignatureHeader), rawBody, 0)`。

错误为 `*hiflow.Error`（HTTP 状态码 + 业务码），用 `hiflow.IsCode(err, hiflow.CodeNotAssignee)`、`hiflow.IsNotFound(err)` 判断。

- 查询（GET）在网络错误、429、502、503、504 时自动重试 2 次（`WithRetries` 调整），每次重新签名；发起、完成等写操作不自动重试。
- 本地时钟与服务端偏差超过 5 分钟时，按服务端时间自动校准后重试一次。
- `Client` 可并发使用，建议全局复用。

## 测试

```bash
go test ./...                                                     # 单元测试
HIFLOW_ADMIN_API_KEY=xxx go test -tags=integration -v ./...       # 端到端（需服务已启动）
```
