package hiflow

import (
	"context"
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

// Start 发起流程实例，返回实例详情（含发起后生成的待办）
func (c *Client) Start(ctx context.Context, req *StartRequest) (*InstanceDetail, error) {
	var out InstanceDetail
	if err := c.post(ctx, "/open/v1/instances", req, &out); err != nil {
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
		set(v, "keyword", q.Keyword)
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

// Recall 发起人撤回：还没有任何任务被处理时，operator 须为实例的发起人（否则 CodeNotAssignee；已有任务处理过为 CodeRecallNotAllowed）
func (c *Client) Recall(ctx context.Context, id int64, operator string) (*InstanceDetail, error) {
	var out InstanceDetail
	if err := c.post(ctx, "/open/v1/instances/"+strconv.FormatInt(id, 10)+"/recall", map[string]string{"operator": operator}, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// Tasks 查询一页任务
func (c *Client) Tasks(ctx context.Context, q *TaskQuery) (*Page[Task], error) {
	v := url.Values{}
	if q != nil {
		setInt(v, "instance_id", q.InstanceID)
		set(v, "status", q.Status)
		set(v, "user", q.User)
		set(v, "assignee", q.Assignee)
		set(v, "keyword", q.Keyword)
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
