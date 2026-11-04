package hiflow

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
)

// Definition 查询流程定义的最新版本（含流程图）
func (c *Client) Definition(ctx context.Context, key string) (*Definition, error) {
	var out Definition
	if err := c.get(ctx, "/open/v1/definitions/"+url.PathEscape(key), nil, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// StartForm 流程最新版本的发起表单（字段、权限与默认值），业务前端据此渲染发起页；模型没有定义表单时 Form 为 nil
func (c *Client) StartForm(ctx context.Context, key string) (*StartForm, error) {
	var out StartForm
	if err := c.get(ctx, "/open/v1/definitions/"+url.PathEscape(key)+"/start-form", nil, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// Start 发起流程实例，返回实例详情（含发起后生成的待办）。设置了 IdempotencyKey 时失败可安全重试（SDK 自动重试），
// 重复请求返回已发起的实例；同一个键已用于其他流程时返回 CodeConflict
func (c *Client) Start(ctx context.Context, req *StartRequest) (*InstanceDetail, error) {
	b, err := json.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("hiflow: encode request: %w", err)
	}
	var out InstanceDetail
	r := &request{method: http.MethodPost, path: "/open/v1/instances", body: b, idempotent: req != nil && req.IdempotencyKey != ""}
	if err := c.call(ctx, r, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// Instance 实例详情（含全部任务与流程轨迹）
func (c *Client) Instance(ctx context.Context, id int64) (*InstanceDetail, error) {
	var out InstanceDetail
	if err := c.get(ctx, "/open/v1/instances/"+strconv.FormatInt(id, 10), nil, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// Instances 查询一页流程实例
func (c *Client) Instances(ctx context.Context, q *InstanceQuery) (*Page[Instance], error) {
	v := url.Values{}
	if q != nil {
		set(v, "key", q.Key)
		set(v, "business_key", q.BusinessKey)
		set(v, "starter", q.Starter)
		set(v, "status", q.Status)
		set(v, "result", q.Result)
		set(v, "keyword", q.Keyword)
		if q.Archived {
			v.Set("archived", "true")
		}
		if q.Suspended {
			v.Set("suspended", "true")
		}
		setInt(v, "page", int64(q.Page))
		setInt(v, "page_size", int64(q.PageSize))
	}
	var out Page[Instance]
	if err := c.get(ctx, "/open/v1/instances", v, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// Cancel 取消流程实例（撤回 / 终止），进行中的任务一并取消
func (c *Client) Cancel(ctx context.Context, id int64, req *CancelRequest) (*InstanceDetail, error) {
	if req == nil {
		req = &CancelRequest{}
	}
	var out InstanceDetail
	if err := c.post(ctx, "/open/v1/instances/"+strconv.FormatInt(id, 10)+"/cancel", req, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// Recall 发起人撤回：还没有其他人处理过任务（发起人自己完成的不算）或已被退回到发起人时可撤回，operator 须为实例的发起人
// （否则 CodeNotAssignee；不满足撤回条件为 CodeRecallNotAllowed）
func (c *Client) Recall(ctx context.Context, id int64, operator string) (*InstanceDetail, error) {
	var out InstanceDetail
	if err := c.post(ctx, "/open/v1/instances/"+strconv.FormatInt(id, 10)+"/recall", map[string]string{"operator": operator}, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// RecallToStarter 发起人撤回到自己重新提交：撤回条件同 Recall，实例不结束——进行中的待办取消、并行分支作废，
// 发起人得到“重新提交”待办（在开始节点上），修改表单后完成它即从头重新推进
func (c *Client) RecallToStarter(ctx context.Context, id int64, operator string) (*InstanceDetail, error) {
	var out InstanceDetail
	if err := c.post(ctx, "/open/v1/instances/"+strconv.FormatInt(id, 10)+"/recall", map[string]string{"operator": operator, "mode": "resubmit"}, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// Urge 发起人催办实例中进行中的人工待办：每个待办推送 Webhook task.urged，由业务系统通知处理人。
// 距上次催办不足服务端最小间隔的待办跳过（UrgeResult.Skipped）；全部跳过时返回 CodeTooManyRequests，没有可催办的待办时返回 CodeNothingToUrge
func (c *Client) Urge(ctx context.Context, id int64, operator, comment string) (*UrgeResult, error) {
	var out UrgeResult
	if err := c.post(ctx, "/open/v1/instances/"+strconv.FormatInt(id, 10)+"/urge", map[string]string{"operator": operator, "comment": comment}, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// UpdateBusinessKey 修改进行中实例的业务主键（如先用临时单号发起、之后换成正式单号），businessKey 为空表示清除；
// 子流程一并更新，内置变量 ${business_key} 同步更新。子流程实例不能单独修改；
// 流程设置了业务主键唯一且新值已有进行中的实例时返回 CodeBusinessKeyRunning
func (c *Client) UpdateBusinessKey(ctx context.Context, id int64, businessKey, operator string) (*InstanceDetail, error) {
	b, err := json.Marshal(map[string]string{"business_key": businessKey, "operator": operator})
	if err != nil {
		return nil, fmt.Errorf("hiflow: encode request: %w", err)
	}
	var out InstanceDetail
	// 设为同一个值是幂等的：网络失败可以安全重试
	r := &request{method: http.MethodPut, path: "/open/v1/instances/" + strconv.FormatInt(id, 10) + "/business-key", body: b, idempotent: true}
	if err := c.call(ctx, r, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// Suspend 挂起实例（如业务争议冻结单据）：挂起期间不能处理任务、撤回、催办（返回 CodeInstanceSuspended），
// 定时器、超时、服务任务暂停，子流程一并挂起；reason 必填。只能挂起顶层实例
func (c *Client) Suspend(ctx context.Context, id int64, reason, operator string) (*InstanceDetail, error) {
	var out InstanceDetail
	if err := c.post(ctx, "/open/v1/instances/"+strconv.FormatInt(id, 10)+"/suspend", map[string]string{"reason": reason, "operator": operator}, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// Resume 恢复挂起的实例：到期的定时器、超时与服务任务随后执行
func (c *Client) Resume(ctx context.Context, id int64, reason, operator string) (*InstanceDetail, error) {
	var out InstanceDetail
	if err := c.post(ctx, "/open/v1/instances/"+strconv.FormatInt(id, 10)+"/resume", map[string]string{"reason": reason, "operator": operator}, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// Signal 向实例发送消息：完成等待该消息（节点 message）的接收任务，变量合并后继续推进；
// 没有等待该消息的接收任务时返回 409（CodeConflict）
func (c *Client) Signal(ctx context.Context, instanceID int64, req *SignalRequest) (*InstanceDetail, error) {
	var out InstanceDetail
	if err := c.post(ctx, "/open/v1/instances/"+strconv.FormatInt(instanceID, 10)+"/signal", req, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// SendMessage 发送消息（快递状态推送等）：按实例 ID 或业务主键找到等待该消息的实例并投递；
// 带 MessageID 时去重（重复返回 MessageDuplicate，失败可安全重试），带 TTL 时暂时无人等待的消息会缓存（MessageBuffered）。
// 同一业务主键有多个实例在等待时返回 CodeConflict，请指定 Key 或 InstanceID。
// 没有实例在等待、而有流程的开始节点以该消息启动时发起新实例（MessageStarted），此时 InstanceID 与 BusinessKey 都可以为空
func (c *Client) SendMessage(ctx context.Context, req *MessageRequest) (*MessageResult, error) {
	b, err := json.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("hiflow: encode request: %w", err)
	}
	var out MessageResult
	r := &request{method: http.MethodPost, path: "/open/v1/messages", body: b, idempotent: req != nil && req.MessageID != ""}
	if err := c.call(ctx, r, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// BroadcastSignal 广播信号（如法规更新）：投递给本客户端内此刻等待它的全部任务，后台分批执行；
// 之后才开始等待的不会收到，挂起的实例不投递。返回投递进度，可用 SignalProgress 查询
func (c *Client) BroadcastSignal(ctx context.Context, req *BroadcastRequest) (*Signal, error) {
	b, err := json.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("hiflow: encode request: %w", err)
	}
	var out Signal
	if err := c.call(ctx, &request{method: http.MethodPost, path: "/open/v1/signals", body: b}, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// SignalProgress 信号的投递进度
func (c *Client) SignalProgress(ctx context.Context, id int64) (*Signal, error) {
	var out Signal
	if err := c.get(ctx, "/open/v1/signals/"+strconv.FormatInt(id, 10), nil, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// Tasks 查询一页任务
func (c *Client) Tasks(ctx context.Context, q *TaskQuery) (*Page[Task], error) {
	v := url.Values{}
	if q != nil {
		set(v, "sort", q.Sort)
		setInt(v, "instance_id", q.InstanceID)
		set(v, "status", q.Status)
		set(v, "user", q.User)
		set(v, "assignee", q.Assignee)
		set(v, "keyword", q.Keyword)
		set(v, "business_key", q.BusinessKey)
		setInt(v, "page", int64(q.Page))
		setInt(v, "page_size", int64(q.PageSize))
	}
	var out Page[Task]
	if err := c.get(ctx, "/open/v1/tasks", v, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// Todo 用户的待办（处理人是他，或未指定处理人且他在候选人中）；q 可补充分页与关键字，其 User、Status 被覆盖
func (c *Client) Todo(ctx context.Context, user string, q *TaskQuery) (*Page[Task], error) {
	query := TaskQuery{}
	if q != nil {
		query = *q
	}
	query.User, query.Status = user, TaskActive
	return c.Tasks(ctx, &query)
}

// Task 任务详情
func (c *Client) Task(ctx context.Context, id int64) (*Task, error) {
	var out Task
	if err := c.get(ctx, "/open/v1/tasks/"+strconv.FormatInt(id, 10), nil, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// Complete 完成任务，流程按模型继续推进；返回推进后的实例详情
func (c *Client) Complete(ctx context.Context, taskID int64, req *CompleteRequest) (*InstanceDetail, error) {
	var out InstanceDetail
	if err := c.post(ctx, "/open/v1/tasks/"+strconv.FormatInt(taskID, 10)+"/complete", req, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// AddSigners 加签：会签处理中临时加人，新待办加入同一批次（依次处理时排在最后），完成条件按新的总人数计算
func (c *Client) AddSigners(ctx context.Context, taskID int64, req *SignersRequest) (*InstanceDetail, error) {
	var out InstanceDetail
	if err := c.post(ctx, "/open/v1/tasks/"+strconv.FormatInt(taskID, 10)+"/add-signers", req, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// RemoveSigners 减签：取消会签中尚未处理的人（不能减掉自己），剩余的人已满足完成条件时节点随即结束并推进
func (c *Client) RemoveSigners(ctx context.Context, taskID int64, req *SignersRequest) (*InstanceDetail, error) {
	var out InstanceDetail
	if err := c.post(ctx, "/open/v1/tasks/"+strconv.FormatInt(taskID, 10)+"/remove-signers", req, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// Delegations 查询一页委托代理规则（最新的在前）
func (c *Client) Delegations(ctx context.Context, q *DelegationQuery) (*Page[Delegation], error) {
	v := url.Values{}
	if q != nil {
		set(v, "user", q.User)
		set(v, "delegate", q.Delegate)
		if q.Active {
			v.Set("active", "true")
		}
		setInt(v, "page", int64(q.Page))
		setInt(v, "page_size", int64(q.PageSize))
	}
	var out Page[Delegation]
	if err := c.get(ctx, "/open/v1/delegations", v, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// CreateDelegation 设置委托代理：生效期内 User 的新待办（指定处理人与会签处理人）改由 Delegate 处理；
// 已生成的待办不变，候选人不受影响
func (c *Client) CreateDelegation(ctx context.Context, req *DelegationRequest) (*Delegation, error) {
	var out Delegation
	if err := c.post(ctx, "/open/v1/delegations", req, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// DeleteDelegation 删除委托代理规则（立即失效）
func (c *Client) DeleteDelegation(ctx context.Context, id int64) error {
	return c.del(ctx, "/open/v1/delegations/"+strconv.FormatInt(id, 10))
}

// CCs 查询一页抄送记录（最新的在前）
func (c *Client) CCs(ctx context.Context, q *CCQuery) (*Page[CC], error) {
	v := url.Values{}
	if q != nil {
		set(v, "user", q.User)
		setInt(v, "instance_id", q.InstanceID)
		set(v, "business_key", q.BusinessKey)
		if q.Unread {
			v.Set("unread", "true")
		}
		setInt(v, "page", int64(q.Page))
		setInt(v, "page_size", int64(q.PageSize))
	}
	var out Page[CC]
	if err := c.get(ctx, "/open/v1/cc", v, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// ReadCC 标记抄送已读；user 须为被抄送人
func (c *Client) ReadCC(ctx context.Context, id int64, user string) (*CC, error) {
	var out CC
	if err := c.post(ctx, "/open/v1/cc/"+strconv.FormatInt(id, 10)+"/read", map[string]string{"user": user}, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// Claim 认领候选任务：认领后 user 成为处理人
func (c *Client) Claim(ctx context.Context, taskID int64, user string) (*Task, error) {
	var out Task
	if err := c.post(ctx, "/open/v1/tasks/"+strconv.FormatInt(taskID, 10)+"/claim", map[string]string{"user": user}, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// ReturnTargets 任务可退回的节点：本实例中处理过、且在当前分支或其外层的人工任务（最近处理的在前）
func (c *Client) ReturnTargets(ctx context.Context, taskID int64) ([]ReturnTarget, error) {
	var out []ReturnTarget
	if err := c.get(ctx, "/open/v1/tasks/"+strconv.FormatInt(taskID, 10)+"/return-targets", nil, &out); err != nil {
		return nil, err
	}
	return out, nil
}

// Return 退回：本待办结束，目标节点重新生成待办；目标在当前并行分支之外时整次并行分支作废。返回推进后的实例详情
func (c *Client) Return(ctx context.Context, taskID int64, req *ReturnRequest) (*InstanceDetail, error) {
	var out InstanceDetail
	if err := c.post(ctx, "/open/v1/tasks/"+strconv.FormatInt(taskID, 10)+"/return", req, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// Transfer 转办：把任务交给其他人处理
func (c *Client) Transfer(ctx context.Context, taskID int64, req *TransferRequest) (*Task, error) {
	var out Task
	if err := c.post(ctx, "/open/v1/tasks/"+strconv.FormatInt(taskID, 10)+"/transfer", req, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// TransferInstance 转办，返回实例详情（与 Complete、Return 等一致）；Transfer 返回任务本身
func (c *Client) TransferInstance(ctx context.Context, taskID int64, req *TransferRequest) (*InstanceDetail, error) {
	body := struct {
		*TransferRequest
		ReturnInstance bool `json:"return_instance"`
	}{req, true}
	var out InstanceDetail
	if err := c.post(ctx, "/open/v1/tasks/"+strconv.FormatInt(taskID, 10)+"/transfer", body, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

func set(v url.Values, k, s string) {
	if s != "" {
		v.Set(k, s)
	}
}

func setInt(v url.Values, k string, n int64) {
	if n > 0 {
		v.Set(k, strconv.FormatInt(n, 10))
	}
}
