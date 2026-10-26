package iamserver

import (
	"context"
	"time"
)

// Directory 必须实现：按 ID 查询用户（含停用用户）。不存在时返回 ErrNotFound
type Directory interface {
	User(ctx context.Context, id string) (*User, error)
	// BatchUsers 批量查询（ids 已去重，不超过 Options.MaxBatchSize）；不存在的 ID 不放在结果中
	BatchUsers(ctx context.Context, ids []string) ([]User, error)
}

// Managers 可选：汇报链（能力 managers）。返回最多 levels 级上级（levels 已限制在 1 ~ MaxManagerLevels），
// 从直属上级开始 Level 严格递增；上级空缺（如已离职）时可跳过该级。用户不存在时返回 ErrNotFound
type Managers interface {
	Managers(ctx context.Context, userID string, levels int) ([]Manager, error)
}

// Departments 可选：部门树与成员（能力 departments；Options.RecursiveMembers 声明支持含下级部门的成员）
type Departments interface {
	// Departments parentID 为空时返回根部门；parentID 不存在时返回 ErrNotFound
	Departments(ctx context.Context, parentID string, page PageRequest) (*Page[Department], error)
	Department(ctx context.Context, id string) (*Department, error)
	// Members 部门的在职成员；recursive 为 true 时含下级部门（只在 Options.RecursiveMembers 时调用）
	Members(ctx context.Context, departmentID string, recursive bool, page PageRequest) (*Page[User], error)
}

// Roles 可选：角色与成员（能力 roles；Options.RoleDepartmentScope 声明支持按部门限定）
type Roles interface {
	Roles(ctx context.Context, page PageRequest) (*Page[Role], error)
	// RoleMembers 担任角色的在职用户；departmentID 非空时只返回该部门（含下级）中的（只在 Options.RoleDepartmentScope 时传入）
	RoleMembers(ctx context.Context, code, departmentID string, page PageRequest) (*Page[User], error)
}

// Searcher 可选：按姓名或账号搜索在职用户（能力 search），departmentID 非空时限定部门（含下级）
type Searcher interface {
	SearchUsers(ctx context.Context, keyword, departmentID string, page PageRequest) (*Page[User], error)
}

// Syncer 可选：导出接口（能力 sync，规范 1.1），HiFlow 据此维护只读的组织快照，在 IAM 长时间不可用时仍能解析处理人规则。
// since 为零值时全量导出（不含墓碑）；否则只返回此后（含该时刻）有变化的记录，已删除的返回墓碑。
// 每一页的 AsOf 为导出开始时的时间（翻页时保持不变），HiFlow 下次以它为 since。
// SyncDepartments / SyncRoles 只在同时实现了 Departments / Roles 时被调用
type Syncer interface {
	SyncUsers(ctx context.Context, since time.Time, page PageRequest) (*SyncPage[SyncUser], error)
	SyncDepartments(ctx context.Context, since time.Time, page PageRequest) (*SyncPage[SyncDepartment], error)
	SyncRoles(ctx context.Context, since time.Time, page PageRequest) (*SyncPage[SyncRole], error)
}
