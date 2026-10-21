package iamserver

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"os"
	"sort"
	"strconv"
	"strings"
)

// Paginate 按偏移量分页一个已排序的完整列表（数据量小、可一次读出时使用；token 为不透明的偏移量编码）
func Paginate[T any](all []T, p PageRequest) (*Page[T], error) {
	offset := 0
	if p.Token != "" {
		raw, err := base64.RawURLEncoding.DecodeString(p.Token)
		n, err2 := strconv.Atoi(strings.TrimPrefix(string(raw), "o:"))
		if err != nil || err2 != nil || n < 0 || !strings.HasPrefix(string(raw), "o:") {
			return nil, BadRequest("page_token 无效")
		}
		offset = n
	}
	size := p.Size
	if size <= 0 {
		size = DefaultPageSize
	}
	out := &Page[T]{Items: []T{}}
	if offset < len(all) {
		end := min(offset+size, len(all))
		out.Items = all[offset:end]
		if end < len(all) {
			out.NextPageToken = base64.RawURLEncoding.EncodeToString([]byte("o:" + strconv.Itoa(end)))
		}
	}
	return out, nil
}

// StaticData 内存目录的数据：用户的 ManagerID 为直属上级，角色的 Members 为用户 ID（与 HiFlow tools/iammock 的数据文件格式相同）
type StaticData struct {
	Users []struct {
		User
		ManagerID string `json:"manager_id,omitempty"`
	} `json:"users"`
	Departments []Department `json:"departments"`
	Roles       []struct {
		Role
		Members []string `json:"members"`
	} `json:"roles"`
}

// LoadStaticFile 读取 JSON 数据文件
func LoadStaticFile(path string) (*StaticData, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var d StaticData
	if err := json.Unmarshal(b, &d); err != nil {
		return nil, err
	}
	return &d, nil
}

// Static 内存中的完整目录实现（全部能力）：用于联调、演示与测试，也是实现各方法语义的参考
type Static struct {
	users    map[string]User
	managers map[string]string
	order    []string
	depts    map[string]Department
	roles    map[string]Role
	members  map[string][]string
}

// NewStatic 由数据构造内存目录；Status 为空的用户视为在职
func NewStatic(d *StaticData) *Static {
	s := &Static{users: map[string]User{}, managers: map[string]string{}, depts: map[string]Department{}, roles: map[string]Role{}, members: map[string][]string{}}
	for _, u := range d.Users {
		if u.Status == "" {
			u.Status = StatusActive
		}
		s.users[u.ID] = u.User
		s.managers[u.ID] = u.ManagerID
		s.order = append(s.order, u.ID)
	}
	sort.Strings(s.order)
	for _, dep := range d.Departments {
		if dep.HeadUserIDs == nil {
			dep.HeadUserIDs = []string{}
		}
		s.depts[dep.ID] = dep
	}
	for _, r := range d.Roles {
		s.roles[r.Code] = r.Role
		s.members[r.Code] = r.Members
	}
	return s
}

func (s *Static) User(_ context.Context, id string) (*User, error) {
	u, ok := s.users[id]
	if !ok {
		return nil, NotFound("用户 %s 不存在", id)
	}
	return &u, nil
}

func (s *Static) BatchUsers(_ context.Context, ids []string) ([]User, error) {
	var out []User
	for _, id := range ids {
		if u, ok := s.users[id]; ok {
			out = append(out, u)
		}
	}
	return out, nil
}

// Managers 沿汇报链向上；停用的上级视为空缺，跳过但计入级数
func (s *Static) Managers(_ context.Context, id string, levels int) ([]Manager, error) {
	if _, ok := s.users[id]; !ok {
		return nil, NotFound("用户 %s 不存在", id)
	}
	var out []Manager
	seen := map[string]bool{id: true}
	for level, cur := 1, s.managers[id]; cur != "" && level <= levels && !seen[cur]; level++ {
		seen[cur] = true
		m, ok := s.users[cur]
		if !ok {
			break
		}
		if m.Status == StatusActive {
			out = append(out, Manager{Level: level, User: m})
		}
		cur = s.managers[cur]
	}
	return out, nil
}

func (s *Static) pathOf(id string) []string {
	var path []string
	for cur, n := id, 0; cur != "" && n < 64; n++ {
		path = append([]string{cur}, path...)
		cur = s.depts[cur].ParentID
	}
	return path
}

func (s *Static) Departments(_ context.Context, parent string, p PageRequest) (*Page[Department], error) {
	if _, ok := s.depts[parent]; parent != "" && !ok {
		return nil, NotFound("部门 %s 不存在", parent)
	}
	var list []Department
	for _, d := range s.depts {
		if d.ParentID == parent {
			d.Path = s.pathOf(d.ID)
			list = append(list, d)
		}
	}
	sort.Slice(list, func(i, j int) bool { return list[i].ID < list[j].ID })
	return Paginate(list, p)
}

func (s *Static) Department(_ context.Context, id string) (*Department, error) {
	d, ok := s.depts[id]
	if !ok {
		return nil, NotFound("部门 %s 不存在", id)
	}
	d.Path = s.pathOf(id)
	return &d, nil
}

// subtree 部门及其全部下级部门
func (s *Static) subtree(root string) map[string]bool {
	out := map[string]bool{root: true}
	for changed := true; changed; {
		changed = false
		for _, d := range s.depts {
			if out[d.ParentID] && !out[d.ID] {
				out[d.ID], changed = true, true
			}
		}
	}
	return out
}

// active 在职用户：scope 非空时限定所属部门，ids 非空时限定用户（按 ID 排序）
func (s *Static) active(scope map[string]bool, ids []string) []User {
	allow := map[string]bool{}
	for _, id := range ids {
		allow[id] = true
	}
	var out []User
	for _, id := range s.order {
		u := s.users[id]
		if u.Status != StatusActive || (ids != nil && !allow[id]) {
			continue
		}
		if scope != nil {
			in := false
			for _, d := range u.DepartmentIDs {
				in = in || scope[d]
			}
			if !in {
				continue
			}
		}
		out = append(out, u)
	}
	return out
}

func (s *Static) Members(_ context.Context, dept string, recursive bool, p PageRequest) (*Page[User], error) {
	if _, ok := s.depts[dept]; !ok {
		return nil, NotFound("部门 %s 不存在", dept)
	}
	scope := map[string]bool{dept: true}
	if recursive {
		scope = s.subtree(dept)
	}
	return Paginate(s.active(scope, nil), p)
}

func (s *Static) Roles(_ context.Context, p PageRequest) (*Page[Role], error) {
	list := make([]Role, 0, len(s.roles))
	for _, r := range s.roles {
		list = append(list, r)
	}
	sort.Slice(list, func(i, j int) bool { return list[i].Code < list[j].Code })
	return Paginate(list, p)
}

func (s *Static) RoleMembers(_ context.Context, code, dept string, p PageRequest) (*Page[User], error) {
	if _, ok := s.roles[code]; !ok {
		return nil, NotFound("角色 %s 不存在", code)
	}
	var scope map[string]bool
	if dept != "" {
		if _, ok := s.depts[dept]; !ok {
			return nil, NotFound("部门 %s 不存在", dept)
		}
		scope = s.subtree(dept)
	}
	members := s.members[code]
	if members == nil {
		members = []string{}
	}
	return Paginate(s.active(scope, members), p)
}

func (s *Static) SearchUsers(_ context.Context, keyword, dept string, p PageRequest) (*Page[User], error) {
	var scope map[string]bool
	if dept != "" {
		if _, ok := s.depts[dept]; !ok {
			return nil, NotFound("部门 %s 不存在", dept)
		}
		scope = s.subtree(dept)
	}
	list := s.active(scope, nil)
	if k := strings.ToLower(keyword); k != "" {
		kept := list[:0:0]
		for _, u := range list {
			if strings.Contains(strings.ToLower(u.Name), k) || strings.Contains(strings.ToLower(u.ID), k) {
				kept = append(kept, u)
			}
		}
		list = kept
	}
	return Paginate(list, p)
}
