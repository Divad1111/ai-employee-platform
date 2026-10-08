package api

import (
	"net/http"
	"strings"

	"github.com/ai-employee-platform/server/internal/approval"
	"github.com/ai-employee-platform/server/internal/artifact"
	"github.com/ai-employee-platform/server/internal/auth"
	"github.com/ai-employee-platform/server/internal/authz"
	"github.com/ai-employee-platform/server/internal/mcp"
	"github.com/ai-employee-platform/server/internal/message"
	"github.com/ai-employee-platform/server/internal/quota"
	"github.com/ai-employee-platform/server/internal/workspace"
)

// onlyAll 全局资源没有“本人”归属。范围为 OWN 时拒绝修改。
func (d Deps) onlyAll(perm string, next authed) authed {
	return func(w http.ResponseWriter, r *http.Request, sess *auth.Session) {
		if d.resolveScope(r.Context(), sess, perm) != authz.ScopeALL {
			writeErr(w, http.StatusForbidden, "当前权限范围为仅本人，不能访问全局资源")
			return
		}
		next(w, r, sess)
	}
}

func (d Deps) scopeAll(r *http.Request, sess *auth.Session, perm string) bool {
	return d.resolveScope(r.Context(), sess, perm) == authz.ScopeALL
}

// employeeOwnedBy 范围为 ALL，或该数字员工属于当前用户。
func (d Deps) employeeOwnedBy(r *http.Request, sess *auth.Session, employeeID, perm string) bool {
	if d.scopeAll(r, sess, perm) {
		return true
	}
	if employeeID == "" || sess == nil || d.Employees == nil {
		return false
	}
	e, err := d.Employees.Get(r.Context(), employeeID)
	if err != nil || e == nil {
		return false
	}
	return e.OwnerUserID == sess.UserID
}

func (d Deps) denyUnlessEmployee(w http.ResponseWriter, r *http.Request, sess *auth.Session, perm, employeeID string) bool {
	if d.employeeOwnedBy(r, sess, employeeID, perm) {
		return false
	}
	writeErr(w, http.StatusForbidden, "无权操作该数字员工")
	return true
}

// canWriteWorkspace 工作区写权限：ALL，或工作站创建者，或绑定员工的所有者。
func (d Deps) canWriteWorkspace(r *http.Request, sess *auth.Session, ws *workspace.Workspace) bool {
	if ws == nil || sess == nil {
		return false
	}
	if d.scopeAll(r, sess, "workspace.write") {
		return true
	}
	if ws.WorkstationID != "" && d.workstationCreatedBy(r.Context(), ws.WorkstationID) == sess.UserID {
		return true
	}
	if d.Employees != nil && ws.EmployeeID != "" {
		if emp, err := d.Employees.Get(r.Context(), ws.EmployeeID); err == nil && emp != nil && emp.OwnerUserID == sess.UserID {
			return true
		}
	}
	return false
}

// quotaVisible 配额范围：ALL 看全部；仅本人只看自己的用户限额、名下员工、自己创建的工作站。
func (d Deps) quotaVisible(r *http.Request, sess *auth.Session, perm, resourceType, resourceID string) bool {
	if sess == nil {
		return false
	}
	if d.scopeAll(r, sess, perm) {
		return true
	}
	switch resourceType {
	case quota.TypeUser, quota.TypeUserBonus:
		return resourceID == sess.UserID
	case quota.TypeEmployee:
		return d.employeeOwnedBy(r, sess, resourceID, perm)
	case quota.TypeWorkstation:
		createdBy := d.workstationCreatedBy(r.Context(), resourceID)
		return createdBy != "" && createdBy == sess.UserID
	default:
		return false
	}
}

func (d Deps) canSeeApproval(r *http.Request, sess *auth.Session, ar *approval.Request, perm string) bool {
	if ar == nil || sess == nil {
		return false
	}
	if d.scopeAll(r, sess, perm) {
		return true
	}
	if ar.RequesterID == sess.UserID {
		return true
	}
	return d.employeeOwnedBy(r, sess, ar.EmployeeID, perm)
}

func (d Deps) canSeeMessage(r *http.Request, sess *auth.Session, msg *message.Message) bool {
	if msg == nil || sess == nil {
		return false
	}
	if d.scopeAll(r, sess, "message.read") {
		return true
	}
	if msg.SenderType == message.TypeUser && msg.SenderID == sess.UserID {
		return true
	}
	if msg.ReceiverType == message.TypeUser && msg.ReceiverID == sess.UserID {
		return true
	}
	if msg.SenderType == message.TypeEmployee && d.employeeOwnedBy(r, sess, msg.SenderID, "message.read") {
		return true
	}
	if msg.ReceiverType == message.TypeEmployee && d.employeeOwnedBy(r, sess, msg.ReceiverID, "message.read") {
		return true
	}
	return false
}

func (d Deps) artifactVisible(r *http.Request, sess *auth.Session, a *artifact.Artifact, perm string) bool {
	if a == nil {
		return false
	}
	if d.scopeAll(r, sess, perm) {
		return true
	}
	if a.JobID == "" || d.Jobs == nil {
		return false
	}
	j, err := d.Jobs.Get(r.Context(), a.JobID)
	if err != nil || j == nil {
		return false
	}
	return d.canAccessJob(r, sess, j, perm)
}

func credentialOwnedBy(c *mcp.Credential, userID string) bool {
	if c == nil || userID == "" {
		return false
	}
	if c.CreatedBy == userID {
		return true
	}
	return strings.EqualFold(c.OwnerType, mcp.OwnerTypeUser) && c.OwnerID == userID
}
