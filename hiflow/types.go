package hiflow

import (
	"encoding/json"
	"time"
)

// 实例状态
const (
	InstanceRunning   = "running"
	InstanceCompleted = "completed"
	InstanceCanceled  = "canceled"
)

// 任务状态
const (
	TaskActive    = "active"
	TaskCompleted = "completed"
	TaskCanceled  = "canceled"
	// TaskWaiting 依次会签中尚未轮到（不出现在待办中）
	TaskWaiting = "waiting"
)

// Definition 流程定义（模型发布的不可变版本）
type Definition struct {
	ID        int64  `json:"id"`
	Key       string `json:"key"`
	Name      string `json:"name"`
	Version   int    `json:"version"`
	Suspended bool   `json:"suspended"`
	// Content 流程图（nodes + edges），业务系统可据此展示流程
	Content   json.RawMessage `json:"content"`
	CreatedAt time.Time       `json:"created_at"`
}

// Instance 流程实例
type Instance struct {
	ID                int64          `json:"id"`
	DefinitionID      int64          `json:"definition_id"`
	DefinitionKey     string         `json:"definition_key"`
	DefinitionVersion int            `json:"definition_version"`
	DefinitionName    string         `json:"definition_name"`
	BusinessKey       string         `json:"business_key"`
	Title             string         `json:"title"`
	Starter           string         `json:"starter"`
	Status            string         `json:"status"`
	Variables         map[string]any `json:"variables"`
	StartedAt         time.Time      `json:"started_at"`
	EndedAt           *time.Time     `json:"ended_at"`
	EndReason         string         `json:"end_reason"`
}

// Running 实例是否仍在进行
func (in *Instance) Running() bool { return in.Status == InstanceRunning }

// InstanceDetail 实例详情：含全部任务与流程轨迹
type InstanceDetail struct {
	Instance
	Tasks  []Task  `json:"tasks"`
	Events []Event `json:"events"`
}

// ActiveTasks 进行中的任务
func (d *InstanceDetail) ActiveTasks() []Task {
	var out []Task
	for _, t := range d.Tasks {
		if t.Status == TaskActive {
			out = append(out, t)
		}
	}
	return out
}

// Task 人工任务
type Task struct {
	ID         int64  `json:"id"`
	InstanceID int64  `json:"instance_id"`
	NodeID     string `json:"node_id"`
	Name       string `json:"name"`
	// Assignee 处理人；为空时 Candidates 中任一人可认领或直接处理
	Assignee   string   `json:"assignee"`
	Candidates []string `json:"candidates"`
	Status     string   `json:"status"`
	Operator   string   `json:"operator"`
	Comment    string   `json:"comment"`
	// Scope 所在并行分支作用域（根为空串）
	Scope string `json:"scope"`
	// MultiKey 多实例（会签）批次，同一节点同一次进入的待办相同；Approved 为本人的表态
	MultiKey    string     `json:"multi_key,omitempty"`
	Approved    *bool      `json:"approved,omitempty"`
	DueAt       *time.Time `json:"due_at"`
	CreatedAt   time.Time  `json:"created_at"`
	CompletedAt *time.Time `json:"completed_at"`
	// Overdue 进行中且已超时（任务列表与详情中返回）
	Overdue bool `json:"overdue,omitempty"`

	// 以下字段在任务列表与任务详情中返回（实例详情中的任务为空）
	InstanceTitle  string `json:"instance_title,omitempty"`
	DefinitionKey  string `json:"definition_key,omitempty"`
	DefinitionName string `json:"definition_name,omitempty"`
	BusinessKey    string `json:"business_key,omitempty"`
	Starter        string `json:"starter,omitempty"`
}

// Event 流程轨迹
type Event struct {
	ID int64 `json:"id"`
	// Type instance_started | node_entered | task_created | task_completed | task_canceled | task_transferred | task_returned
	// | task_overdue | instance_completed | instance_canceled | instance_recalled
	Type      string    `json:"type"`
	TaskID    *int64    `json:"task_id"`
	NodeID    string    `json:"node_id"`
	Operator  string    `json:"operator"`
	Comment   string    `json:"comment"`
	CreatedAt time.Time `json:"created_at"`
}

// Page 分页结果
type Page[T any] struct {
	List     []T   `json:"list"`
	Total    int64 `json:"total"`
	Page     int   `json:"page"`
	PageSize int   `json:"page_size"`
}

// StartRequest 发起流程
type StartRequest struct {
	// Key 流程标识（管理后台中模型的 key），按最新发布的版本发起
	Key string `json:"key"`
	// Version 指定版本发起，0 表示最新版本
	Version int `json:"version,omitempty"`
	// BusinessKey 业务主键（如请假单号），便于反查实例
	BusinessKey string `json:"business_key,omitempty"`
	// Title 实例标题，为空时使用流程名称
	Title   string `json:"title,omitempty"`
	Starter string `json:"starter,omitempty"`
	// Variables 流程变量：处理人表达式 ${var}、网关条件中使用
	Variables map[string]any `json:"variables,omitempty"`
}

// ReturnRequest 退回到本实例中处理过的人工任务节点（可选节点见 ReturnTargets）
type ReturnRequest struct {
	Operator   string `json:"operator"`
	TargetNode string `json:"target_node"`
	Comment    string `json:"comment,omitempty"`
}

// ReturnTarget 可退回的节点
type ReturnTarget struct {
	NodeID      string    `json:"node_id"`
	Name        string    `json:"name"`
	Operator    string    `json:"operator"`
	CompletedAt time.Time `json:"completed_at"`
}

// CompleteRequest 完成任务：审批结果等放在 Variables 中，由排他网关的条件决定流程走向。
// 多实例（会签）任务提交的 approved 记为本人表态，节点结束时流程变量 approved 为会签结论，<节点 ID>_result 为统计
type CompleteRequest struct {
	// Operator 处理人，须为任务的处理人或候选人之一
	Operator  string         `json:"operator"`
	Comment   string         `json:"comment,omitempty"`
	Variables map[string]any `json:"variables,omitempty"`
}

// CancelRequest 取消流程实例
type CancelRequest struct {
	Operator string `json:"operator,omitempty"`
	Reason   string `json:"reason,omitempty"`
}

// TransferRequest 转办
type TransferRequest struct {
	Operator string `json:"operator"`
	To       string `json:"to"`
	Comment  string `json:"comment,omitempty"`
}

// InstanceQuery 实例列表查询条件
type InstanceQuery struct {
	Key         string
	BusinessKey string
	Starter     string
	// Status running | completed | canceled
	Status   string
	Keyword  string
	Page     int
	PageSize int
}

// TaskQuery 任务列表查询条件
type TaskQuery struct {
	InstanceID int64
	// Status active | completed | canceled
	Status string
	// User 该用户的待办（处理人是他，或未指定处理人且他在候选人中）
	User     string
	Assignee string
	Keyword  string
	Page     int
	PageSize int
}
