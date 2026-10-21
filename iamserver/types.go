// Package iamserver 是 HiFlow 组织架构目录接口（IAM Directory API v1，规范见 HiFlow 仓库 docs/iam-api.yaml）的服务端骨架：
// IAM 团队实现 Directory（以及按需实现 Managers、Departments、Roles、Searcher）几个查询方法，
// 由 Handler 负责签名校验、路由、参数校验、分页参数、能力声明与统一错误格式。
//
//	h := iamserver.New(myDirectory{}, iamserver.Options{DirectoryID: "hfdir_xxx", Secret: os.Getenv("HIFLOW_DIRECTORY_SECRET")})
//	http.Handle("/hiflow/v1/", http.StripPrefix("/hiflow/v1", h))
//
// 挂载时须去掉基础地址的路径前缀：签名中的 Path 是相对基础地址的路径（如 /users/u1）。
package iamserver

import (
	"errors"
	"fmt"
	"net/http"
)

// SpecVersion 实现的规范版本
const SpecVersion = "1.0"

// 默认值与上限（与规范一致）
const (
	DefaultPageSize     = 100
	MaxPageSize         = 500
	DefaultMaxBatchSize = 200
	MaxManagerLevels    = 10
	StatusActive        = "active"
	StatusDisabled      = "disabled"
)

// User 用户。停用（离职、冻结）的用户仍须能按 ID 查到（Status 为 disabled）；搜索、部门成员、角色成员只返回在职用户
type User struct {
	ID                  string   `json:"id"`
	Name                string   `json:"name"`
	Status              string   `json:"status"`
	Title               string   `json:"title,omitempty"`
	Email               string   `json:"email,omitempty"`
	Mobile              string   `json:"mobile,omitempty"`
	DepartmentIDs       []string `json:"department_ids,omitempty"`
	PrimaryDepartmentID string   `json:"primary_department_id,omitempty"`
}

// Department 部门；HeadUserIDs 可多人、可为空，Path 为从根部门到本部门的 ID（可选）
type Department struct {
	ID          string   `json:"id"`
	Name        string   `json:"name"`
	ParentID    string   `json:"parent_id"`
	HeadUserIDs []string `json:"head_user_ids"`
	Path        []string `json:"path,omitempty"`
}

// Role 角色（岗位 / 职能分组），流程模型按 Code 引用
type Role struct {
	Code        string `json:"code"`
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
}

// Manager 汇报链中的一级上级：Level 从 1（直属上级）开始严格递增，空缺的级可以跳过
type Manager struct {
	Level int  `json:"level"`
	User  User `json:"user"`
}

// PageRequest 分页参数：Size 已校验（1 ~ 500，默认 100），Token 为上一页返回的 NextPageToken（首页为空）
type PageRequest struct {
	Size  int
	Token string
}

// Page 一页结果；NextPageToken 为空表示最后一页
type Page[T any] struct {
	Items         []T    `json:"items"`
	NextPageToken string `json:"next_page_token"`
}

// Error 返回给 HiFlow 的错误：HTTP 状态码 + {code, message}
type Error struct {
	Status  int
	Code    string
	Message string
}

func (e *Error) Error() string { return e.Code + ": " + e.Message }

// 规范中的错误码
const (
	CodeBadRequest     = "bad_request"
	CodeUnauthorized   = "unauthorized"
	CodeNotFound       = "not_found"
	CodeNotImplemented = "not_implemented"
	CodeInternal       = "internal"
	CodeUnavailable    = "unavailable"
)

// ErrNotFound 用户、部门、角色不存在（返回 404 not_found）
var ErrNotFound = &Error{Status: http.StatusNotFound, Code: CodeNotFound, Message: "资源不存在"}

// NotFound 带说明的 404
func NotFound(format string, args ...any) *Error {
	return &Error{Status: http.StatusNotFound, Code: CodeNotFound, Message: fmt.Sprintf(format, args...)}
}

// BadRequest 参数错误（如无效的 page_token）
func BadRequest(format string, args ...any) *Error {
	return &Error{Status: http.StatusBadRequest, Code: CodeBadRequest, Message: fmt.Sprintf(format, args...)}
}

// Unavailable IAM 暂时不可用（HiFlow 按目录配置使用过期缓存）
func Unavailable(format string, args ...any) *Error {
	return &Error{Status: http.StatusServiceUnavailable, Code: CodeUnavailable, Message: fmt.Sprintf(format, args...)}
}

// asError 方法返回的错误转为响应；其它错误记为 500 internal
func asError(err error) *Error {
	var e *Error
	if errors.As(err, &e) {
		return e
	}
	return &Error{Status: http.StatusInternalServerError, Code: CodeInternal, Message: "内部错误"}
}
