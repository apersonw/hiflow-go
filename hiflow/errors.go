package hiflow

import (
	"errors"
	"fmt"
)

// Error HiFlow 返回的错误（HTTP 状态码 + 业务码），可用 errors.As 取出
type Error struct {
	// StatusCode HTTP 状态码
	StatusCode int
	// Code 业务码，见服务端 README 的错误码表（如 40401 资源不存在）
	Code      int
	Message   string
	RequestID string
}

func (e *Error) Error() string {
	if e.RequestID != "" {
		return fmt.Sprintf("hiflow: %d %s (http %d, request_id %s)", e.Code, e.Message, e.StatusCode, e.RequestID)
	}
	return fmt.Sprintf("hiflow: %d %s (http %d)", e.Code, e.Message, e.StatusCode)
}

// 常用业务码（与服务端 pkg/errs 一致）
const (
	CodeBadRequest = 40001
	// CodeReturnTarget 不能退回到该节点
	CodeReturnTarget = 40003
	// CodeInvalidUser 转办、加签、委托的目标用户在组织架构中不存在或已停用（目录校验为 strict）
	CodeInvalidUser      = 40004
	CodeMissingAuth      = 40101
	CodeInvalidClient    = 40102
	CodeTimestampExpired = 40103
	CodeNonceReplay      = 40104
	CodeSignature        = 40105
	CodeIPForbidden      = 40301
	// CodeNotAssignee 当前用户不是任务的处理人 / 不在候选人中
	CodeNotAssignee = 40302
	CodeNotFound    = 40401
	CodeConflict    = 40901
	// CodeInstanceEnded 流程实例已结束（完成或取消）
	CodeInstanceEnded = 40902
	// CodeTaskNotActive 任务已处理或已取消（如被其他候选人抢先处理）
	CodeTaskNotActive = 40903
	// CodeDefinitionSuspended 流程定义已挂起，不能发起新实例
	CodeDefinitionSuspended = 40904
	// CodeRecallNotAllowed 已有任务被处理，不能撤回
	CodeRecallNotAllowed = 40905
	CodePayloadTooLarge  = 41301
	// CodeEvaluate 流程推进失败（排他网关没有满足条件的出口等），通常是流程变量缺失
	CodeEvaluate        = 42201
	CodeTooManyRequests = 42901
	CodeInternal        = 50000
	CodeDependency      = 50003
	// CodeCallback HiFlow 回调业务系统失败（如处理人规则解析）
	CodeCallback = 50201
)

// IsCode 错误是否为指定业务码
func IsCode(err error, code int) bool {
	var e *Error
	return errors.As(err, &e) && e.Code == code
}

// IsNotFound 资源不存在（含访问其它客户端的数据）
func IsNotFound(err error) bool { return IsCode(err, CodeNotFound) }
