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

// 任务种类
const (
	TaskKindUser    = "user"
	TaskKindService = "service"
	TaskKindReceive = "receive"
	// TaskKindTimer 定时器：NextRunAt 到期后自动推进
	TaskKindTimer = "timer"
	// TaskKindSubprocess 子流程：ChildInstanceID 结束后自动推进
	TaskKindSubprocess = "subprocess"
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
	// Result 审批结果：最后到达的、声明了 result 的结束节点的结果（如 approved、rejected），模型未声明时为空
	Result string `json:"result"`
	// 作为子流程发起时：父实例、父实例中的子流程任务与嵌套层数（顶层为 0）
	ParentInstanceID *int64 `json:"parent_instance_id"`
	ParentTaskID     *int64 `json:"parent_task_id"`
	Depth            int    `json:"depth"`
}

// Running 实例是否仍在进行
func (in *Instance) Running() bool { return in.Status == InstanceRunning }

// InstanceDetail 实例详情：含全部任务与流程轨迹
type InstanceDetail struct {
	Instance
	Tasks  []Task  `json:"tasks"`
	Events []Event `json:"events"`
	// CC 本实例的抄送记录
	CC []CC `json:"cc"`
	// Archived 实例已归档（结束超过服务端 archive.after 后移入归档表，只读）
	Archived bool `json:"archived"`
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
	// Kind 任务种类：TaskKindUser 人工任务、TaskKindService 服务任务（引擎回调业务系统执行）、TaskKindReceive 接收任务（等待 Signal）、
	// TaskKindTimer 定时器、TaskKindSubprocess 子流程
	Kind string `json:"kind"`
	// Assignee 处理人；为空时 Candidates 中任一人可认领或直接处理
	Assignee   string   `json:"assignee"`
	Candidates []string `json:"candidates"`
	Status     string   `json:"status"`
	Operator   string   `json:"operator"`
	Comment    string   `json:"comment"`
	// Scope 所在并行分支作用域（根为空串）
	Scope string `json:"scope"`
	// MultiKey 多实例（会签）批次，同一节点同一次进入的待办相同；Approved 为本人的表态
	MultiKey string `json:"multi_key,omitempty"`
	Approved *bool  `json:"approved,omitempty"`
	// DelegatedFrom 按委托规则交给代理人（Assignee）时的原处理人；委托人本人也可处理
	DelegatedFrom string     `json:"delegated_from,omitempty"`
	DueAt         *time.Time `json:"due_at"`
	CreatedAt     time.Time  `json:"created_at"`
	CompletedAt   *time.Time `json:"completed_at"`
	// 服务任务：已调用次数、下次调用时间（进行中且为 nil 表示重试已用尽）、最近一次失败原因
	Attempts  int        `json:"attempts,omitempty"`
	NextRunAt *time.Time `json:"next_run_at,omitempty"`
	LastError string     `json:"last_error,omitempty"`
	// ChildInstanceID 子流程任务发起的子实例
	ChildInstanceID *int64 `json:"child_instance_id,omitempty"`
	// ResumeNode 退回时选择了“处理后直接回到退回人”：本待办处理后回到的节点
	ResumeNode string `json:"resume_node,omitempty"`
	// Overdue 进行中且已超时（任务列表与详情中返回）
	Overdue bool `json:"overdue,omitempty"`

	// 以下字段在任务列表与任务详情中返回（实例详情中的任务为空）
	InstanceTitle  string `json:"instance_title,omitempty"`
	DefinitionKey  string `json:"definition_key,omitempty"`
	DefinitionName string `json:"definition_name,omitempty"`
	BusinessKey    string `json:"business_key,omitempty"`
	Starter        string `json:"starter,omitempty"`
	// Form 本节点的表单视图（只在任务详情 Client.Task 中返回；模型没有定义表单时为 nil）
	Form *FormView `json:"form,omitempty"`
}

// 表单字段权限
const (
	PermEdit = "edit"
	PermRead = "read"
)

// FormView 某个节点上的表单：字段、本节点的权限与当前值（隐藏的字段不返回）。
// 提交时只传可编辑的字段；类型、必填或权限不符时返回 CodeInvalidForm
type FormView struct {
	NodeID string      `json:"node_id"`
	Fields []FormField `json:"fields"`
}

// FormField 表单字段（Key 即流程变量名）
type FormField struct {
	Key   string `json:"key"`
	Label string `json:"label"`
	// Type text / textarea / number / boolean / date（2006-01-02）/ datetime（RFC 3339）/ select / multiselect / user / users /
	// file / files（附件，值为 FileValue / []FileValue）
	Type string `json:"type"`
	// Required 本节点必须有值
	Required    bool         `json:"required,omitempty"`
	Options     []FormOption `json:"options,omitempty"`
	Default     any          `json:"default,omitempty"`
	Placeholder string       `json:"placeholder,omitempty"`
	Help        string       `json:"help,omitempty"`
	// Min / Max number 为取值范围，text / textarea 为字数，multiselect / users / files 为个数
	Min     *float64 `json:"min,omitempty"`
	Max     *float64 `json:"max,omitempty"`
	Pattern string   `json:"pattern,omitempty"`
	// Accept / MaxSize 附件允许的文件类型（扩展名或 MIME 类型，逗号分隔）与单个文件大小上限（字节）
	Accept  string `json:"accept,omitempty"`
	MaxSize int64  `json:"max_size,omitempty"`
	// Permission 本节点的权限：PermEdit 或 PermRead
	Permission string `json:"permission"`
	// Value 当前值（发起表单为默认值）
	Value any `json:"value,omitempty"`
}

// FileValue 附件字段的值：文件由业务系统自己存储（HiFlow 不接入任何文件服务），ID 为业务系统的文件标识；带 Size / Type 时服务端按字段的 MaxSize / Accept 校验
type FileValue struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	Size int64  `json:"size,omitempty"`
	Type string `json:"type,omitempty"`
	URL  string `json:"url,omitempty"`
}

// FormOption 单选 / 多选的选项
type FormOption struct {
	Value string `json:"value"`
	Label string `json:"label,omitempty"`
}

// StartForm 发起表单
type StartForm struct {
	DefinitionID int64     `json:"definition_id"`
	Key          string    `json:"key"`
	Version      int       `json:"version"`
	Name         string    `json:"name"`
	Form         *FormView `json:"form"`
}

// Event 流程轨迹
type Event struct {
	ID int64 `json:"id"`
	// Type instance_started | node_entered | task_created | task_completed | task_canceled | task_transferred | task_returned
	// | task_overdue | task_auto_passed（按模型的自动通过规则直接通过，Operator 为处理人）| cc | signers_added | signers_removed
	// | service_failed | node_skipped | instance_completed | instance_canceled | instance_recalled
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
	// IdempotencyKey 幂等键（可选，最长 128 位，如 "leave:" + 请假单号）：同一客户端下相同的键只发起一次，重复请求返回已发起的实例。
	// 设置后 Start 在网络错误、429 / 5xx 时按客户端的重试配置自动重试
	IdempotencyKey string `json:"idempotency_key,omitempty"`
	// Title 实例标题，为空时使用流程名称
	Title   string `json:"title,omitempty"`
	Starter string `json:"starter,omitempty"`
	// Variables 流程变量：处理人表达式 ${var}、网关条件中使用
	Variables map[string]any `json:"variables,omitempty"`
}

// UrgeResult 催办结果
type UrgeResult struct {
	InstanceID int64 `json:"instance_id"`
	// Urged 本次催办的待办；Skipped 距上次催办不足最小间隔而跳过的待办
	Urged   []int64 `json:"urged"`
	Skipped []int64 `json:"skipped"`
}

// ReturnRequest 退回到本实例中处理过的人工任务节点，或开始节点（退回发起人重新提交）；可选节点见 ReturnTargets
type ReturnRequest struct {
	Operator   string `json:"operator"`
	TargetNode string `json:"target_node"`
	Comment    string `json:"comment,omitempty"`
	// DirectBack 处理后直接回到退回人所在的节点，不再按模型重走中间的节点（ReturnTarget.DirectBack 为 true 的目标才可以）
	DirectBack bool `json:"direct_back,omitempty"`
}

// ReturnTarget 可退回的节点
type ReturnTarget struct {
	NodeID string `json:"node_id"`
	Name   string `json:"name"`
	// Starter 目标为开始节点：退回发起人，生成发起人的“重新提交”待办
	Starter bool `json:"starter"`
	// DirectBack 可以选择处理后直接回到退回人（与退回人在同一分支内）
	DirectBack  bool      `json:"direct_back"`
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
	// CC 处理时抄送给这些人（另有节点配置的抄送在节点处理完成时产生）
	CC []string `json:"cc,omitempty"`
}

// SignalRequest 向实例发送消息：完成等待该消息的接收任务
type SignalRequest struct {
	Message string `json:"message"`
	// Operator 记为接收任务的处理人，默认 system
	Operator  string         `json:"operator,omitempty"`
	Variables map[string]any `json:"variables,omitempty"`
	// MessageID 消息 ID（如快递公司推送 ID）：同一客户端内相同 ID 只处理一次
	MessageID string `json:"message_id,omitempty"`
	// TTL 暂时没有等待该消息的节点时缓存的秒数（最长 30 天），实例走到该节点后自动投递；0 不缓存（返回 CodeConflict）
	TTL int `json:"ttl,omitempty"`
}

// MessageRequest 发送消息：按实例 ID 或业务主键（如快递单号，Key 可限定流程）找到等待该消息的实例
type MessageRequest struct {
	InstanceID  int64  `json:"instance_id,omitempty"`
	BusinessKey string `json:"business_key,omitempty"`
	// Key 限定流程（同一业务主键有多个流程的实例时）
	Key     string `json:"key,omitempty"`
	Message string `json:"message"`
	// MessageID 消息 ID：同一客户端内相同 ID 只处理一次（重复时 Status 为 MessageDuplicate）；设置后 SendMessage 失败时自动重试
	MessageID string `json:"message_id,omitempty"`
	// TTL 暂时没有等待该消息的节点时缓存的秒数（最长 30 天）；0 不缓存（返回 CodeConflict）
	TTL       int            `json:"ttl,omitempty"`
	Operator  string         `json:"operator,omitempty"`
	Variables map[string]any `json:"variables,omitempty"`
}

// 消息的处理结果
const (
	MessageDelivered = "delivered"
	MessageBuffered  = "buffered"
	MessageDuplicate = "duplicate"
)

// MessageResult 发送消息的结果
type MessageResult struct {
	// Status delivered 已投递、buffered 已缓存等待投递、duplicate 同一 message_id 已处理过
	Status     string `json:"status"`
	InstanceID int64  `json:"instance_id,omitempty"`
	TaskID     int64  `json:"task_id,omitempty"`
	MessageID  string `json:"message_id,omitempty"`
	// Instance 投递后的实例详情（只在 delivered 时返回）
	Instance *InstanceDetail `json:"instance,omitempty"`
}

// SignersRequest 加签 / 减签（只适用于会签等多实例任务）
type SignersRequest struct {
	// Operator 当前待办的处理人
	Operator string   `json:"operator"`
	Users    []string `json:"users"`
	Comment  string   `json:"comment,omitempty"`
}

// Delegation 委托代理规则：生效期内 User 的新待办改由 Delegate 处理
type Delegation struct {
	ID       int64  `json:"id"`
	User     string `json:"user"`
	Delegate string `json:"delegate"`
	// DefinitionKey 只委托该流程的待办，空串表示全部流程
	DefinitionKey string    `json:"definition_key"`
	StartsAt      time.Time `json:"starts_at"`
	// EndsAt 结束时间（不含），nil 表示长期有效
	EndsAt    *time.Time `json:"ends_at"`
	Reason    string     `json:"reason"`
	CreatedAt time.Time  `json:"created_at"`
}

// DelegationRequest 设置委托代理
type DelegationRequest struct {
	User          string `json:"user"`
	Delegate      string `json:"delegate"`
	DefinitionKey string `json:"definition_key,omitempty"`
	// StartsAt 默认现在
	StartsAt *time.Time `json:"starts_at,omitempty"`
	EndsAt   *time.Time `json:"ends_at,omitempty"`
	Reason   string     `json:"reason,omitempty"`
}

// DelegationQuery 委托代理查询条件
type DelegationQuery struct {
	User     string
	Delegate string
	// Active 只看当前生效的
	Active   bool
	Page     int
	PageSize int
}

// CC 抄送记录（只读，不影响流程推进）
type CC struct {
	ID         int64  `json:"id"`
	InstanceID int64  `json:"instance_id"`
	TaskID     *int64 `json:"task_id"`
	NodeID     string `json:"node_id"`
	// User 被抄送人；Sender 触发抄送的处理人，Comment 为其处理意见
	User      string     `json:"user"`
	Sender    string     `json:"sender"`
	Comment   string     `json:"comment"`
	ReadAt    *time.Time `json:"read_at"`
	CreatedAt time.Time  `json:"created_at"`

	// 以下字段在抄送列表中返回
	InstanceTitle  string `json:"instance_title,omitempty"`
	DefinitionKey  string `json:"definition_key,omitempty"`
	DefinitionName string `json:"definition_name,omitempty"`
	BusinessKey    string `json:"business_key,omitempty"`
	Starter        string `json:"starter,omitempty"`
	TaskName       string `json:"task_name,omitempty"`
}

// CCQuery 抄送查询条件
type CCQuery struct {
	// User 被抄送人（“抄送我的”）
	User       string
	InstanceID int64
	// Unread 只看未读
	Unread   bool
	Page     int
	PageSize int
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
	Status string
	// Result 审批结果（结束节点声明的 result，如 approved、rejected）
	Result  string
	Keyword string
	// Archived 查询已归档的实例（服务端开启归档后，结束较久的实例只在归档中）
	Archived bool
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
