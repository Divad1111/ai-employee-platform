package workspace

import (
	"context"
	"errors"
	"fmt"
	"strings"
)

// 绑定解析错误。
var (
	ErrWorkspaceMissing     = errors.New("工作区不存在，请从列表选择已登记工作区，或使用「快速新建」登记本机路径")
	ErrPathNeedsWorkstation = errors.New("该值是本机路径，登记工作区前请先绑定工作站")
)

// ResolveBinding 把员工上的工作区引用解析成已登记的 workspace id。
// 空值保持空；已存在的 id 原样返回。
// 若值是本机绝对路径，则复用同一工作站上的相同路径，否则新建工作区。
func (s *Service) ResolveBinding(ctx context.Context, ref, workstationID, employeeID, actorID, ip string) (string, error) {
	ref = strings.TrimSpace(ref)
	if ref == "" {
		return "", nil
	}
	if w, err := s.store.Get(ctx, ref); err == nil && w != nil {
		if err := s.ensureOwner(ctx, w.ID, w.EmployeeID, employeeID); err != nil {
			return "", err
		}
		return w.ID, nil
	}
	if !looksLikeLocalPath(ref) {
		return "", fmt.Errorf("%w：%s", ErrWorkspaceMissing, ref)
	}
	if strings.TrimSpace(workstationID) == "" {
		return "", ErrPathNeedsWorkstation
	}
	list, err := s.store.List(ctx)
	if err != nil {
		return "", err
	}
	for _, w := range list {
		if w.WorkstationID != workstationID || !sameLocalPath(w.Path, ref) {
			continue
		}
		if err := s.ensureOwner(ctx, w.ID, w.EmployeeID, employeeID); err != nil {
			return "", err
		}
		return w.ID, nil
	}
	created, err := s.Create(ctx, CreateInput{
		WorkstationID: workstationID,
		Path:          ref,
		EmployeeID:    employeeID,
	}, actorID, ip)
	if err != nil {
		return "", err
	}
	return created.ID, nil
}

func (s *Service) ensureOwner(ctx context.Context, workspaceID, workspaceEmployeeID, employeeID string) error {
	if employeeID == "" {
		return nil
	}
	if workspaceEmployeeID != "" && workspaceEmployeeID != employeeID {
		return ErrLocked
	}
	if s.binder == nil {
		return nil
	}
	owner, err := s.binder.FindByWorkspace(ctx, workspaceID)
	if err != nil {
		return err
	}
	if owner != "" && owner != employeeID {
		return ErrLocked
	}
	return nil
}

func looksLikeLocalPath(s string) bool {
	s = strings.TrimSpace(s)
	if s == "" {
		return false
	}
	if strings.HasPrefix(s, "/") || strings.HasPrefix(s, `\\`) {
		return true
	}
	if len(s) >= 3 && s[1] == ':' && (s[2] == '\\' || s[2] == '/') {
		c := s[0]
		return (c >= 'A' && c <= 'Z') || (c >= 'a' && c <= 'z')
	}
	return false
}

func sameLocalPath(a, b string) bool {
	norm := func(s string) string {
		s = strings.TrimSpace(s)
		s = strings.ReplaceAll(s, "/", `\`)
		s = strings.TrimRight(s, `\`)
		return strings.ToLower(s)
	}
	return norm(a) == norm(b)
}
