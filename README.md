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
| `AddSigners(ctx, taskID, req)` / `RemoveSigners(ctx, taskID, req)` | 会签加签 / 减签 |
| `CreateDelegation(ctx, req)` / `Delegations(ctx, q)` / `DeleteDelegation(ctx, id)` | 委托代理：生效期内委托人的新待办交给代理人（`Task.DelegatedFrom` 记录委托人） |
| `CCs(ctx, q)` / `ReadCC(ctx, id, user)` | 抄送记录（“抄送我的”、未读）/ 标记已读；处理时抄送用 `CompleteRequest.CC` |
| `Signal(ctx, instanceID, req)` | 发送消息，推进等待该消息的接收任务 |
| `Recall(ctx, id, operator)` | 发起人撤回（还没有任务被处理时） |
| `Transfer(ctx, taskID, req)` | 转办 |

## 待办嵌入令牌

为当前登录用户签发嵌入令牌（本地计算，不请求 HiFlow），交给业务前端的[浏览器 SDK / 嵌入组件](../browser/README.md)：

```go
tok, err := client.NewEmbedToken(&hiflow.EmbedTokenOptions{User: userID, ExpiresIn: 2 * time.Hour})
```

## 处理人规则回调

模型中的处理人可以写 `@leader(${starter})` 这类规则，HiFlow 推进到该节点时回调客户端的回调地址（管理后台配置）解析成用户。
签名格式同 Webhook，密钥为客户端密钥：

```go
http.Handle("/hiflow/callback", hiflow.CallbackHandler(clientSecret, func(r *http.Request, e *hiflow.WebhookEvent) (any, error) {
	req, err := e.Resolve() // req.Rule == "leader"，req.Args == []string{"u1"}
	if err != nil {
		return nil, err
	}
	return hiflow.ResolveResponse{Users: []string{directory.LeaderOf(req.Args[0])}}, nil
}))
```

返回多人时作为候选人；返回零人、非 2xx 或超时时本次推进失败（`CodeCallback` / 42201）并整体回滚。

服务任务（`service_task`）用同一个回调地址，`e.Type == hiflow.CallbackServiceInvoke`，用 `e.Service()` 解析，返回 `hiflow.ResolveResponse` 换成
`hiflow.ServiceResponse{Variables: ...}`；非 2xx 时 HiFlow 退避重试，请按 `TaskID` 保证幂等。

## 接收事件（Webhook）

管理后台为客户端配置 Webhook 后，HiFlow 在任务创建 / 完成 / 取消 / 指派 / 退回 / 超时、抄送、实例发起 / 结束 / 取消时推送事件（签名、失败退避重试、至少投递一次）：

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

## 组织架构目录接口（IAM 侧服务端骨架）

IAM 团队实现 HiFlow 目录接口（规范 docs/iam-api.yaml）时，用 `iamserver` 包只需实现几个查询方法，签名校验、路由、参数校验、能力声明与错误格式由骨架处理：

```go
type myDirectory struct{ db *sql.DB }

func (d myDirectory) User(ctx context.Context, id string) (*iamserver.User, error)            { /* 不存在返回 iamserver.ErrNotFound */ }
func (d myDirectory) BatchUsers(ctx context.Context, ids []string) ([]iamserver.User, error)  { /* 不存在的 ID 不返回 */ }
// 按需实现 Managers、Departments、Roles、Searcher 接口，能力声明自动生成

h := iamserver.New(myDirectory{db}, iamserver.Options{DirectoryID: "hfdir_xxx", Secret: os.Getenv("HIFLOW_DIRECTORY_SECRET")})
http.Handle("/hiflow/v1/", http.StripPrefix("/hiflow/v1", h)) // 签名中的 Path 相对基础地址，须去掉前缀
```

`iamserver.Static` 是内存中的完整实现（也是各方法语义的参考），示例见 [examples/iamserver](examples/iamserver/main.go)。
多实例部署时把 `Options.Nonces` 换成共享存储。对接完成后用 HiFlow 的 `tools/iamcheck` 或管理后台“测试连接”做契约自检。

## 测试

```bash
go test ./...                                                     # 单元测试
HIFLOW_ADMIN_API_KEY=xxx go test -tags=integration -v ./...       # 端到端（需服务已启动）
```
