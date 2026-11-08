package api

import (
	"errors"
	"net/http"

	"github.com/ai-employee-platform/server/internal/auth"
	"github.com/ai-employee-platform/server/internal/workspace"
)

// authorizeWorkspaceRef 在解析路径或写入关联之前同时检查节点、工作区和已有绑定。
func (d Deps) authorizeWorkspaceRef(w http.ResponseWriter, r *http.Request, s *auth.Session, ref, node, perm string) bool {
	if ref == "" {
		return true
	}
	if d.Workspaces == nil {
		writeErr(w, 503, "工作区服务不可用")
		return false
	}
	ws, err := d.Workspaces.LookupReference(r.Context(), ref, node)
	if err != nil {
		// 新本机路径由解析器登记并由工作站再次校验；查询失败必须拒绝。
		if !errors.Is(err, workspace.ErrNotFound) {
			writeErr(w, 503, "无法校验工作区")
			return false
		}
		return true
	}
	if (node != "" && ws.WorkstationID != node) || (!d.scopeAll(r, s, perm) && !d.canWriteWorkspace(r, s, ws)) {
		writeErr(w, 403, "工作区不属于目标工作站或无权使用")
		return false
	}
	if !d.scopeAll(r, s, perm) && ws.EmployeeID != "" && !d.employeeOwnedBy(r, s, ws.EmployeeID, perm) {
		writeErr(w, 403, "工作区已绑定其他用户的员工")
		return false
	}
	return true
}

func (d Deps) authorizeWorkspaceEmployee(w http.ResponseWriter, r *http.Request, s *auth.Session, id, node string) bool {
	if id == "" {
		return true
	}
	if d.Employees == nil {
		writeErr(w, 403, "员工服务不可用")
		return false
	}
	e, err := d.Employees.Get(r.Context(), id)
	if err != nil || (e.WorkstationID != "" && e.WorkstationID != node) || (!d.scopeAll(r, s, "workspace.write") && e.OwnerUserID != s.UserID) {
		writeErr(w, 403, "无权绑定该员工或工作站不一致")
		return false
	}
	// 改绑会解除旧工作区，也必须取得该对象的写权限。
	if e.WorkspaceID != "" && !d.scopeAll(r, s, "workspace.write") {
		old, err := d.Workspaces.Get(r.Context(), e.WorkspaceID)
		if err != nil || !d.canWriteWorkspace(r, s, old) {
			writeErr(w, 403, "无权解除原工作区")
			return false
		}
	}
	return true
}
