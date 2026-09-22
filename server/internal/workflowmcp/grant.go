package workflowmcp

import (
	"context"
	"sort"
)

// ResolveScope 从员工已授权工作流算出可见范围（技能/知识闭包）。
// knowledge_refs 中的 "*" 表示全库可读；其它项作为路径前缀匹配。
func (s *Service) ResolveScope(ctx context.Context, employeeID string) (*Scope, error) {
	grants, err := s.store.ListGrantsByEmployee(ctx, employeeID)
	if err != nil {
		return nil, err
	}
	scope := &Scope{
		WorkflowIDs:       []string{},
		SkillIDs:          []string{},
		KnowledgePrefixes: []string{},
	}
	skillSet := map[string]struct{}{}
	prefixSet := map[string]struct{}{}
	for _, g := range grants {
		w, err := s.store.GetWorkflow(ctx, g.WorkflowID)
		if err != nil {
			continue
		}
		scope.WorkflowIDs = append(scope.WorkflowIDs, w.ID)
		for _, sid := range w.SkillRefs {
			skillSet[sid] = struct{}{}
		}
		for _, kr := range w.KnowledgeRefs {
			if kr == "*" {
				scope.AllKnowledge = true
				continue
			}
			prefixSet[kr] = struct{}{}
		}
	}
	for sid := range skillSet {
		scope.SkillIDs = append(scope.SkillIDs, sid)
	}
	for p := range prefixSet {
		scope.KnowledgePrefixes = append(scope.KnowledgePrefixes, p)
	}
	sort.Strings(scope.WorkflowIDs)
	sort.Strings(scope.SkillIDs)
	sort.Strings(scope.KnowledgePrefixes)
	return scope, nil
}

// InScopeWorkflow 判断工作流是否在 scope 内。
func (sc *Scope) InScopeWorkflow(id string) bool {
	if sc == nil {
		return false
	}
	for _, w := range sc.WorkflowIDs {
		if w == id {
			return true
		}
	}
	return false
}

// InScopeSkill 判断技能是否在 scope 内。
func (sc *Scope) InScopeSkill(idOrCursor string) bool {
	if sc == nil {
		return false
	}
	for _, s := range sc.SkillIDs {
		if s == idOrCursor {
			return true
		}
	}
	return false
}

// InScopeKnowledge 判断知识路径/id 是否在 scope 内。
func (sc *Scope) InScopeKnowledge(pathOrID string) bool {
	if sc == nil {
		return false
	}
	if sc.AllKnowledge {
		return true
	}
	for _, p := range sc.KnowledgePrefixes {
		if pathOrID == p || hasPrefixPath(pathOrID, p) {
			return true
		}
	}
	return false
}

func hasPrefixPath(path, prefix string) bool {
	if len(path) < len(prefix) {
		return false
	}
	if path[:len(prefix)] != prefix {
		return false
	}
	if len(path) == len(prefix) {
		return true
	}
	return path[len(prefix)] == '/'
}

// GrantWorkflow 向员工授权工作流。
func (s *Service) GrantWorkflow(ctx context.Context, employeeID, workflowID, grantedBy string) error {
	if _, err := s.store.GetWorkflow(ctx, workflowID); err != nil {
		return err
	}
	return s.store.GrantWorkflow(ctx, EmployeeGrant{
		EmployeeID: employeeID,
		WorkflowID: workflowID,
		GrantedBy:  grantedBy,
	})
}

// RevokeWorkflow 撤销员工工作流授权。
func (s *Service) RevokeWorkflow(ctx context.Context, employeeID, workflowID string) error {
	return s.store.RevokeWorkflow(ctx, employeeID, workflowID)
}

// ListEmployeeWorkflows 列出员工已授权工作流。
func (s *Service) ListEmployeeWorkflows(ctx context.Context, employeeID string) ([]*Workflow, error) {
	grants, err := s.store.ListGrantsByEmployee(ctx, employeeID)
	if err != nil {
		return nil, err
	}
	out := make([]*Workflow, 0, len(grants))
	for _, g := range grants {
		w, err := s.store.GetWorkflow(ctx, g.WorkflowID)
		if err != nil {
			continue
		}
		out = append(out, w)
	}
	return out, nil
}

// EffectiveSkills 返回员工因工作流授权可访问的技能包（闭包）。
func (s *Service) EffectiveSkills(ctx context.Context, employeeID string) ([]*SkillPackage, error) {
	scope, err := s.ResolveScope(ctx, employeeID)
	if err != nil {
		return nil, err
	}
	out := make([]*SkillPackage, 0, len(scope.SkillIDs))
	for _, sid := range scope.SkillIDs {
		sp, err := s.store.GetSkill(ctx, sid)
		if err != nil {
			continue
		}
		out = append(out, sp)
	}
	return out, nil
}
