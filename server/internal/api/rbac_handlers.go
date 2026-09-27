package api

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"github.com/ai-employee-platform/server/internal/audit"
	"github.com/ai-employee-platform/server/internal/auth"
	"github.com/ai-employee-platform/server/internal/authz"
	"github.com/ai-employee-platform/server/internal/employee"
	"github.com/ai-employee-platform/server/internal/job"
	"github.com/ai-employee-platform/server/internal/quota"
	"github.com/ai-employee-platform/server/internal/workspace"
	"github.com/ai-employee-platform/server/internal/wsmember"
)

func scopeLabelCN(scope string) string {
	switch strings.ToUpper(strings.TrimSpace(scope)) {
	case "ALL":
		return "全部资源"
	case "OWN":
		return "仅本人"
	case "ASSIGNED":
		return "已分配"
	case "NONE":
		return "无权限"
	default:
		if scope == "" {
			return "（空）"
		}
		return scope
	}
}

func isSuperAdmin(sess *auth.Session) bool {
	if sess == nil {
		return false
	}
	return containsRole(sess.Roles, "SUPER_ADMIN")
}

func containsRole(roles []string, target string) bool {
	for _, r := range roles {
		if strings.EqualFold(strings.TrimSpace(r), target) {
			return true
		}
	}
	return false
}

func toAuthzGrants(gs []auth.PermissionGrant) []authz.Grant {
	out := make([]authz.Grant, 0, len(gs))
	for _, g := range gs {
		out = append(out, authz.Grant{Name: g.Code, Scope: authz.Scope(strings.ToUpper(g.Scope))})
	}
	return out
}

func (d Deps) resolveScope(ctx context.Context, sess *auth.Session, perm string) authz.Scope {
	// 仅当查询的就是本次路由声明的权限码时，才复用中间件注入的 Scope。
	// 仪表盘路由是 employee.read，不能把它的 OWN 误当成 workstation.read。
	if authz.PermFrom(ctx) == perm {
		if scope := authz.ScopeFrom(ctx); scope != "" && scope != authz.ScopeNONE {
			return scope
		}
	}
	grants := authz.GrantsFrom(ctx)
	if grants == nil && d.Auth != nil && sess != nil {
		gs, _ := d.Auth.PermissionGrants(ctx, sess)
		grants = toAuthzGrants(gs)
	}
	scope := authz.Resolve(grants, perm)
	if scope == authz.ScopeNONE && authz.Allowed(grants, perm) {
		for _, g := range grants {
			if g.Name == "*" {
				return authz.ScopeALL
			}
		}
		return authz.ScopeOWN
	}
	return scope
}

func (d Deps) filterEmployees(r *http.Request, sess *auth.Session, list []*employee.Employee) []*employee.Employee {
	scope := d.resolveScope(r.Context(), sess, "employee.read")
	if scope == authz.ScopeALL {
		return list
	}
	out := make([]*employee.Employee, 0)
	for _, e := range list {
		if e.OwnerUserID == sess.UserID {
			out = append(out, e)
		}
	}
	return out
}

func (d Deps) canSeeEmployee(r *http.Request, sess *auth.Session, e *employee.Employee) bool {
	scope := d.resolveScope(r.Context(), sess, "employee.read")
	if scope == authz.ScopeALL {
		return true
	}
	return e != nil && e.OwnerUserID == sess.UserID
}

func (d Deps) canManageEmployee(r *http.Request, sess *auth.Session, e *employee.Employee, permCode string) bool {
	if e == nil {
		return false
	}
	scope := d.resolveScope(r.Context(), sess, permCode)
	if scope == authz.ScopeALL {
		return true
	}
	return e.OwnerUserID == sess.UserID
}

func (d Deps) canAccessWorkspace(r *http.Request, sess *auth.Session, ws *workspace.Workspace, permCode string) bool {
	if ws == nil {
		return false
	}
	scope := d.resolveScope(r.Context(), sess, permCode)
	if scope == authz.ScopeALL {
		return true
	}
	if d.WSMembers != nil && ws.WorkstationID != "" {
		if ok, _ := d.WSMembers.HasAccess(r.Context(), ws.WorkstationID, sess.UserID); ok {
			return true
		}
	}
	if d.Employees != nil && ws.EmployeeID != "" {
		if emp, err := d.Employees.Get(r.Context(), ws.EmployeeID); err == nil && emp != nil {
			if emp.OwnerUserID == sess.UserID {
				return true
			}
		}
	}
	return false
}

func (d Deps) canAccessJob(r *http.Request, sess *auth.Session, j *job.Job, permCode string) bool {
	if j == nil {
		return false
	}
	scope := d.resolveScope(r.Context(), sess, permCode)
	if scope == authz.ScopeALL {
		return true
	}
	if j.CreatedBy == sess.UserID {
		return true
	}
	if d.Employees != nil && j.EmployeeID != "" {
		if emp, err := d.Employees.Get(r.Context(), j.EmployeeID); err == nil && emp != nil {
			return d.canSeeEmployee(r, sess, emp)
		}
	}
	return false
}

func (d Deps) filterWorkstationIDs(r *http.Request, sess *auth.Session, ids []string) []string {
	scope := d.resolveScope(r.Context(), sess, "workstation.read")
	if scope == authz.ScopeALL {
		return ids
	}
	if d.WSMembers == nil {
		return nil
	}
	allowed := map[string]struct{}{}
	mems, _ := d.WSMembers.ListByUser(r.Context(), sess.UserID)
	for _, m := range mems {
		allowed[m.WorkstationID] = struct{}{}
	}
	var out []string
	for _, id := range ids {
		if _, ok := allowed[id]; ok {
			out = append(out, id)
		}
	}
	return out
}

func (d Deps) canAccessWorkstation(r *http.Request, sess *auth.Session, wsID string) bool {
	scope := d.resolveScope(r.Context(), sess, "workstation.read")
	if scope == authz.ScopeALL {
		return true
	}
	if d.WSMembers == nil || wsID == "" {
		return false
	}
	ok, _ := d.WSMembers.HasAccess(r.Context(), wsID, sess.UserID)
	return ok
}

func (d Deps) ensureWSAccessForEmployee(ctx context.Context, sess *auth.Session, wsID, ownerID string) error {
	if wsID == "" {
		return nil
	}
	if d.resolveScope(ctx, sess, "employee.write") == authz.ScopeALL {
		return nil
	}
	uid := ownerID
	if uid == "" {
		uid = sess.UserID
	}
	if d.WSMembers == nil {
		return employee.ErrWSAccessDenied
	}
	ok, err := d.WSMembers.HasAccess(ctx, wsID, uid)
	if err != nil {
		return err
	}
	if !ok {
		return employee.ErrWSAccessDenied
	}
	return nil
}

func (d Deps) handleMeEnhanced(w http.ResponseWriter, r *http.Request, sess *auth.Session) {
	grants, err := d.Auth.PermissionGrants(r.Context(), sess)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	u, _ := d.Auth.Users().FindByID(r.Context(), sess.UserID)
	display, email, status := "", "", auth.StatusActive
	if u != nil {
		display, email, status = u.DisplayName, u.Email, u.Status
	}
	perms := make([]map[string]string, 0, len(grants))
	for _, g := range grants {
		perms = append(perms, map[string]string{"name": g.Code, "scope": g.Scope})
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"id": sess.UserID, "username": sess.Username, "display_name": display,
		"email": email, "status": status, "roles": sess.Roles, "permissions": perms,
		"user": map[string]any{
			"id": sess.UserID, "username": sess.Username, "display_name": display,
			"email": email, "status": status, "roles": sess.Roles,
		},
	})
}

func (d Deps) handleListUsers(w http.ResponseWriter, r *http.Request, _ *auth.Session) {
	list, err := d.Auth.Users().List(r.Context())
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	items := make([]map[string]any, 0, len(list))
	for _, u := range list {
		wsCount, empCount := 0, 0
		if d.WSMembers != nil {
			ms, _ := d.WSMembers.ListByUser(r.Context(), u.ID)
			wsCount = len(ms)
		}
		if d.Employees != nil {
			emps, _ := d.Employees.List(r.Context())
			for _, e := range emps {
				if e.OwnerUserID == u.ID {
					empCount++
				}
			}
		}
		items = append(items, map[string]any{
			"id": u.ID, "username": u.Username, "display_name": u.DisplayName,
			"email": u.Email, "status": u.Status, "roles": u.Roles,
			"workstation_count": wsCount, "employee_count": empCount,
			"last_login_at": u.LastLoginAt, "created_at": u.CreatedAt,
		})
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items})
}

func (d Deps) handleCreateUser(w http.ResponseWriter, r *http.Request, sess *auth.Session) {
	var req struct {
		Username    string   `json:"username"`
		Password    string   `json:"password"`
		DisplayName string   `json:"display_name"`
		Email       string   `json:"email"`
		Roles       []string `json:"roles"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeErr(w, http.StatusBadRequest, "无效请求体")
		return
	}
	req.Username = strings.TrimSpace(req.Username)
	if len(req.Username) < 3 || len(req.Password) < 8 {
		writeErr(w, http.StatusBadRequest, "用户名至少 3 位，密码至少 8 位")
		return
	}
	hash, err := auth.HashPassword(req.Password)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	roles := req.Roles
	if len(roles) == 0 {
		roles = []string{"VIEWER"}
	}
	// 防垂直提权：非超级管理员严禁创建超级管理员账号
	if containsRole(roles, "SUPER_ADMIN") && !isSuperAdmin(sess) {
		writeErr(w, http.StatusForbidden, "只有超级管理员可以创建超级管理员账号")
		return
	}
	u := &auth.User{
		Username: req.Username, PasswordHash: hash, DisplayName: req.DisplayName,
		Email: req.Email, Status: auth.StatusActive, Roles: roles,
	}
	if err := d.Auth.Users().Create(r.Context(), u); err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	if d.Audit != nil {
		d.Audit.Log(r.Context(), "USER", sess.UserID, "user.create", "success", clientIP(r), map[string]string{"id": u.ID, "username": u.Username})
	}
	writeJSON(w, http.StatusCreated, map[string]any{
		"id": u.ID, "username": u.Username, "display_name": u.DisplayName, "roles": u.Roles, "status": u.Status,
	})
}

func (d Deps) handleGetUser(w http.ResponseWriter, r *http.Request, _ *auth.Session) {
	u, err := d.Auth.Users().FindByID(r.Context(), r.PathValue("id"))
	if err != nil || u == nil {
		writeErr(w, http.StatusNotFound, "用户不存在")
		return
	}
	var members []*wsmember.Membership
	if d.WSMembers != nil {
		members, _ = d.WSMembers.ListByUser(r.Context(), u.ID)
	}
	var emps []*employee.Employee
	if d.Employees != nil {
		all, _ := d.Employees.List(r.Context())
		for _, e := range all {
			if e.OwnerUserID == u.ID {
				emps = append(emps, e)
			}
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"id": u.ID, "username": u.Username, "display_name": u.DisplayName,
		"email": u.Email, "status": u.Status, "roles": u.Roles,
		"last_login_at": u.LastLoginAt, "created_at": u.CreatedAt,
		"workstations": members, "employees": emps,
	})
}

func (d Deps) handlePatchUser(w http.ResponseWriter, r *http.Request, sess *auth.Session) {
	u, err := d.Auth.Users().FindByID(r.Context(), r.PathValue("id"))
	if err != nil || u == nil {
		writeErr(w, http.StatusNotFound, "用户不存在")
		return
	}
	// 防垂直提权与账号夺取：
	// 1. 若目标账号是超级管理员，操作者必须也是超级管理员
	if containsRole(u.Roles, "SUPER_ADMIN") && !isSuperAdmin(sess) {
		writeErr(w, http.StatusForbidden, "只有超级管理员可以修改超级管理员账号")
		return
	}
	oldName, oldEmail := u.DisplayName, u.Email
	oldRoles := append([]string(nil), u.Roles...)
	var req struct {
		DisplayName *string  `json:"display_name"`
		Email       *string  `json:"email"`
		Roles       []string `json:"roles"`
		Password    *string  `json:"password"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeErr(w, http.StatusBadRequest, "无效请求体")
		return
	}
	// 2. 若要赋予超级管理员角色，操作者自身必须也是超级管理员
	if req.Roles != nil && containsRole(req.Roles, "SUPER_ADMIN") && !isSuperAdmin(sess) {
		writeErr(w, http.StatusForbidden, "只有超级管理员可以授予超级管理员角色")
		return
	}
	if req.DisplayName != nil {
		u.DisplayName = *req.DisplayName
	}
	if req.Email != nil {
		u.Email = *req.Email
	}
	if req.Password != nil && *req.Password != "" {
		if len(*req.Password) < 8 {
			writeErr(w, http.StatusBadRequest, "密码至少 8 位")
			return
		}
		hash, err := auth.HashPassword(*req.Password)
		if err != nil {
			writeErr(w, http.StatusInternalServerError, err.Error())
			return
		}
		u.PasswordHash = hash
	}
	if err := d.Auth.Users().Update(r.Context(), u); err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	if req.Roles != nil {
		_ = d.Auth.Users().SetRoles(r.Context(), u.ID, req.Roles)
		u.Roles = req.Roles
	}
	if d.Audit != nil {
		parts := []string{
			audit.FormatChange("显示名", oldName, u.DisplayName),
			audit.FormatChange("邮箱", oldEmail, u.Email),
		}
		if req.Roles != nil {
			parts = append(parts, audit.FormatChange("角色", strings.Join(oldRoles, ","), strings.Join(u.Roles, ",")))
		}
		if req.Password != nil && *req.Password != "" {
			parts = append(parts, "密码：已重置（不记录明文）")
		}
		d.Audit.Log(r.Context(), "USER", sess.UserID, "user.update", "success", clientIP(r), map[string]string{
			"id": u.ID, "username": u.Username, "summary": audit.JoinSummary(parts...),
		})
	}
	writeJSON(w, http.StatusOK, map[string]any{"id": u.ID, "username": u.Username, "roles": u.Roles, "status": u.Status})
}

func (d Deps) handleDisableUser(w http.ResponseWriter, r *http.Request, sess *auth.Session) {
	d.setUserStatus(w, r, sess, auth.StatusDisabled)
}

func (d Deps) handleEnableUser(w http.ResponseWriter, r *http.Request, sess *auth.Session) {
	d.setUserStatus(w, r, sess, auth.StatusActive)
}

func (d Deps) handleDeleteUser(w http.ResponseWriter, r *http.Request, sess *auth.Session) {
	id := r.PathValue("id")
	if id == sess.UserID {
		writeErr(w, http.StatusBadRequest, "不能删除当前登录用户")
		return
	}
	u, err := d.Auth.Users().FindByID(r.Context(), id)
	if err != nil || u == nil {
		writeErr(w, http.StatusNotFound, "用户不存在")
		return
	}
	// 防垂直越权：只有超级管理员可以删除超级管理员账号
	if containsRole(u.Roles, "SUPER_ADMIN") && !isSuperAdmin(sess) {
		writeErr(w, http.StatusForbidden, "只有超级管理员可以删除超级管理员账号")
		return
	}
	if err := d.Auth.Users().SoftDelete(r.Context(), id); err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	if d.Audit != nil {
		d.Audit.Log(r.Context(), "USER", sess.UserID, "user.delete", "success", clientIP(r),
			map[string]string{
				"id": id, "username": u.Username,
				"summary": fmt.Sprintf("删除用户 %s", u.Username),
			})
	}
	writeJSON(w, http.StatusOK, map[string]string{"ok": "true", "id": id})
}

func (d Deps) setUserStatus(w http.ResponseWriter, r *http.Request, sess *auth.Session, status string) {
	u, err := d.Auth.Users().FindByID(r.Context(), r.PathValue("id"))
	if err != nil || u == nil {
		writeErr(w, http.StatusNotFound, "用户不存在")
		return
	}
	oldStatus := u.Status
	if u.ID == sess.UserID && status == auth.StatusDisabled {
		writeErr(w, http.StatusBadRequest, "不能禁用当前登录用户")
		return
	}
	// 防垂直越权：只有超级管理员可以修改超级管理员账号状态
	if containsRole(u.Roles, "SUPER_ADMIN") && !isSuperAdmin(sess) {
		writeErr(w, http.StatusForbidden, "只有超级管理员可以修改超级管理员账号状态")
		return
	}
	u.Status = status
	if err := d.Auth.Users().Update(r.Context(), u); err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	action := "user.enable"
	if status == auth.StatusDisabled {
		action = "user.disable"
	}
	if d.Audit != nil {
		d.Audit.Log(r.Context(), "USER", sess.UserID, action, "success", clientIP(r), map[string]string{
			"id": u.ID, "username": u.Username,
			"summary": audit.FormatChange("状态", oldStatus, status),
		})
	}
	writeJSON(w, http.StatusOK, map[string]any{"id": u.ID, "status": u.Status})
}

func (d Deps) handleListRoles(w http.ResponseWriter, r *http.Request, _ *auth.Session) {
	roles, err := d.Auth.Users().ListRoles(r.Context())
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": roles})
}

func (d Deps) handleCreateRole(w http.ResponseWriter, r *http.Request, sess *auth.Session) {
	var req struct {
		Name        string `json:"name"`
		Description string `json:"description"`
		Grants      []struct {
			Permission string `json:"permission"`
			Code       string `json:"code"`
			Scope      string `json:"scope"`
		} `json:"grants"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeErr(w, http.StatusBadRequest, "无效请求体")
		return
	}
	name := strings.ToUpper(strings.TrimSpace(req.Name))
	if len(name) < 2 || len(name) > 32 {
		writeErr(w, http.StatusBadRequest, "角色标识需 2–32 个字符")
		return
	}
	for _, c := range name {
		if !((c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9') || c == '_') {
			writeErr(w, http.StatusBadRequest, "角色标识仅允许大写字母、数字与下划线")
			return
		}
	}
	grants := make([]auth.PermissionGrant, 0, len(req.Grants))
	for _, g := range req.Grants {
		code := strings.TrimSpace(g.Permission)
		if code == "" {
			code = strings.TrimSpace(g.Code)
		}
		if code == "" {
			continue
		}
		scope := strings.ToUpper(strings.TrimSpace(g.Scope))
		if scope == "" {
			scope = "NONE"
		}
		grants = append(grants, auth.PermissionGrant{Code: code, Scope: scope})
	}
	if err := d.Auth.Users().CreateRole(r.Context(), name, strings.TrimSpace(req.Description), grants); err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	if d.Audit != nil {
		d.Audit.Log(r.Context(), "USER", sess.UserID, "role.create", "success", clientIP(r),
			map[string]string{
				"role":    name,
				"summary": fmt.Sprintf("新建角色 %s（%s），权限 %d 项", name, strings.TrimSpace(req.Description), len(grants)),
			})
	}
	writeJSON(w, http.StatusCreated, map[string]any{"name": name, "description": req.Description, "grants": grants})
}

func (d Deps) handleDeleteRole(w http.ResponseWriter, r *http.Request, sess *auth.Session) {
	name := strings.ToUpper(strings.TrimSpace(r.PathValue("name")))
	if name == "" {
		writeErr(w, http.StatusBadRequest, "需要角色名")
		return
	}
	if auth.IsBuiltInRole(name) {
		writeErr(w, http.StatusBadRequest, "内置角色不可删除")
		return
	}
	if err := d.Auth.Users().DeleteRole(r.Context(), name); err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	if d.Audit != nil {
		d.Audit.Log(r.Context(), "USER", sess.UserID, "role.delete", "success", clientIP(r),
			map[string]string{"role": name})
	}
	writeJSON(w, http.StatusOK, map[string]string{"ok": "true", "name": name})
}

func (d Deps) handleListPermissionsCatalog(w http.ResponseWriter, r *http.Request, _ *auth.Session) {
	perms, err := d.Auth.Users().ListAllPermissions(r.Context())
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": perms})
}

func (d Deps) handlePatchRolePerms(w http.ResponseWriter, r *http.Request, sess *auth.Session) {
	var req struct {
		Permission string `json:"permission"`
		Scope      string `json:"scope"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.Permission == "" {
		writeErr(w, http.StatusBadRequest, "需要 permission 与 scope")
		return
	}
	role := r.PathValue("name")
	oldScope := "（未授权）"
	if roles, err := d.Auth.Users().ListRoles(r.Context()); err == nil {
		for _, ri := range roles {
			if !strings.EqualFold(ri.Name, role) {
				continue
			}
			for _, g := range ri.Grants {
				if g.Code == req.Permission {
					if g.Scope != "" {
						oldScope = scopeLabelCN(g.Scope)
					}
				}
			}
		}
	}
	newScope := strings.ToUpper(strings.TrimSpace(req.Scope))
	if err := d.Auth.Users().SetRolePermissionScope(r.Context(), role, req.Permission, newScope); err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	if d.Audit != nil {
		d.Audit.Log(r.Context(), "USER", sess.UserID, "role.update", "success", clientIP(r), map[string]string{
			"role": role, "permission": req.Permission,
			"before": oldScope, "after": scopeLabelCN(newScope),
			"summary": fmt.Sprintf("角色 %s 的权限 %s：范围 %s → %s", role, req.Permission, oldScope, scopeLabelCN(newScope)),
		})
	}
	writeJSON(w, http.StatusOK, map[string]string{"ok": "true"})
}

func (d Deps) handleListWSMembers(w http.ResponseWriter, r *http.Request, sess *auth.Session) {
	wsID := r.PathValue("id")
	if !d.canAccessWorkstation(r, sess, wsID) {
		writeErr(w, http.StatusNotFound, "工作站不存在或无权限")
		return
	}
	if d.WSMembers == nil {
		writeJSON(w, http.StatusOK, map[string]any{"items": []any{}})
		return
	}
	items, err := d.WSMembers.ListByWorkstation(r.Context(), wsID)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items})
}

func (d Deps) handleAddWSMember(w http.ResponseWriter, r *http.Request, sess *auth.Session) {
	wsID := r.PathValue("id")
	if d.resolveScope(r.Context(), sess, "workstation.write") != authz.ScopeALL && !d.canAccessWorkstation(r, sess, wsID) {
		writeErr(w, http.StatusForbidden, "权限不足")
		return
	}
	var req struct {
		UserID string `json:"user_id"`
		Role   string `json:"role"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.UserID == "" {
		writeErr(w, http.StatusBadRequest, "需要 user_id")
		return
	}
	if req.Role == "" {
		req.Role = wsmember.RoleMember
	}
	if d.WSMembers == nil {
		writeErr(w, http.StatusServiceUnavailable, "成员存储未就绪")
		return
	}
	m := &wsmember.Membership{WorkstationID: wsID, UserID: req.UserID, Role: req.Role, Status: wsmember.StatusActive}
	if err := d.WSMembers.Upsert(r.Context(), m); err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	d.auditUser(r, sess, "workstation.member.add", map[string]string{
		"workstation_id": wsID, "user_id": req.UserID, "role": req.Role,
	})
	writeJSON(w, http.StatusCreated, m)
}

func (d Deps) handleRemoveWSMember(w http.ResponseWriter, r *http.Request, sess *auth.Session) {
	if d.WSMembers == nil {
		writeErr(w, http.StatusServiceUnavailable, "成员存储未就绪")
		return
	}
	wsID := r.PathValue("id")
	userID := r.PathValue("userId")
	if err := d.WSMembers.Remove(r.Context(), wsID, userID); err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	d.auditUser(r, sess, "workstation.member.remove", map[string]string{
		"workstation_id": wsID, "user_id": userID,
	})
	writeJSON(w, http.StatusOK, map[string]string{"ok": "true"})
}

func (d Deps) handleListQuotas(w http.ResponseWriter, r *http.Request, _ *auth.Session) {
	if d.Quota == nil {
		writeJSON(w, http.StatusOK, map[string]any{"items": []any{}})
		return
	}
	items, err := d.Quota.Store().ListPolicies(r.Context())
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items})
}

// handleMyQuota 当前登录用户的月度 Token 用量与有效限额。
func (d Deps) handleMyQuota(w http.ResponseWriter, r *http.Request, sess *auth.Session) {
	writeJSON(w, http.StatusOK, d.buildMyQuota(r, sess))
}

// buildMyQuota 组装当前用户配额快照（仪表盘 /me 共用）。
func (d Deps) buildMyQuota(r *http.Request, sess *auth.Session) map[string]any {
	out := map[string]any{
		"user_id":               sess.UserID,
		"period_type":           quota.PeriodMonthly,
		"tokens_used":           int64(0),
		"requests_used":         int64(0),
		"token_limit":           int64(0),
		"request_limit":         int64(0),
		"role_token_limit":      int64(0),
		"exception_token_limit": int64(0),
		"extra_token_limit":     int64(0),
		"has_exception":         false,
		"has_extra":             false,
		"unlimited":             true,
		"source":                "none",
		"source_role":           "",
		"usage_percent":         float64(0),
		"remaining":             int64(0),
	}
	if d.Quota == nil || sess == nil || sess.UserID == "" {
		return out
	}
	ctx := r.Context()
	roles := sess.Roles
	if u, err := d.Auth.Users().FindByID(ctx, sess.UserID); err == nil && u != nil && len(u.Roles) > 0 {
		roles = u.Roles
	}
	usage, err := d.Quota.Store().GetUsage(ctx, quota.TypeUser, sess.UserID, quota.PeriodMonthly)
	if err == nil && usage != nil {
		out["tokens_used"] = usage.TokensUsed
		out["requests_used"] = usage.RequestsUsed
		out["period_key"] = usage.PeriodKey
	}
	eff, err := d.Quota.ResolveEffective(ctx, sess.UserID, roles)
	if err != nil || eff == nil {
		return out
	}
	out["unlimited"] = eff.Unlimited
	out["token_limit"] = eff.TokenLimit
	out["request_limit"] = eff.RequestLimit
	out["role_token_limit"] = eff.RoleTokenLimit
	out["exception_token_limit"] = eff.ExceptionTokenLimit
	out["extra_token_limit"] = eff.ExtraTokenLimit
	out["has_exception"] = eff.HasException
	out["has_extra"] = eff.HasExtra
	out["source"] = eff.Source
	out["source_role"] = eff.SourceRole
	used, _ := out["tokens_used"].(int64)
	if eff.TokenLimit > 0 {
		pct := float64(used) / float64(eff.TokenLimit) * 100
		if pct > 100 {
			pct = 100
		}
		out["usage_percent"] = pct
		rem := eff.TokenLimit - used
		if rem < 0 {
			rem = 0
		}
		out["remaining"] = rem
		out["unlimited"] = false
	}
	return out
}

func (d Deps) handleUpsertQuota(w http.ResponseWriter, r *http.Request, sess *auth.Session) {
	if d.Quota == nil {
		writeErr(w, http.StatusServiceUnavailable, "配额服务未就绪")
		return
	}
	var p quota.Policy
	if err := json.NewDecoder(r.Body).Decode(&p); err != nil {
		writeErr(w, http.StatusBadRequest, "无效请求体")
		return
	}
	if p.ResourceType == "" || p.ResourceID == "" {
		writeErr(w, http.StatusBadRequest, "需要 resource_type 与 resource_id")
		return
	}
	switch p.ResourceType {
	case quota.TypeUser, quota.TypeUserBonus, quota.TypeWorkstation, quota.TypeEmployee, quota.TypeRole:
	default:
		writeErr(w, http.StatusBadRequest, "resource_type 须为 USER / USER_BONUS / WORKSTATION / DIGITAL_EMPLOYEE / ROLE")
		return
	}
	if p.ResourceType == quota.TypeRole {
		p.ResourceID = strings.ToUpper(strings.TrimSpace(p.ResourceID))
	}
	p.Enabled = true
	beforeLimit := "（未设置）"
	if old, err := d.Quota.Store().GetPolicy(r.Context(), p.ResourceType, p.ResourceID, quota.PeriodMonthly); err == nil && old != nil {
		beforeLimit = strconv.FormatInt(old.TokenLimit, 10)
	}
	if err := d.Quota.Store().UpsertPolicy(r.Context(), &p); err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	afterLimit := strconv.FormatInt(p.TokenLimit, 10)
	d.auditUser(r, sess, "quota.upsert", map[string]string{
		"resource_type": p.ResourceType,
		"resource_id":   p.ResourceID,
		"token_limit":   afterLimit,
		"before":        beforeLimit,
		"after":         afterLimit,
		"summary":       fmt.Sprintf("%s %s 月度 Token 限额：%s → %s", p.ResourceType, p.ResourceID, beforeLimit, afterLimit),
	})
	writeJSON(w, http.StatusOK, p)
}

func (d Deps) handleDeleteQuota(w http.ResponseWriter, r *http.Request, sess *auth.Session) {
	if d.Quota == nil {
		writeErr(w, http.StatusServiceUnavailable, "配额服务未就绪")
		return
	}
	resourceType := strings.TrimSpace(r.URL.Query().Get("resource_type"))
	resourceID := strings.TrimSpace(r.URL.Query().Get("resource_id"))
	period := strings.TrimSpace(r.URL.Query().Get("period_type"))
	if period == "" {
		period = quota.PeriodMonthly
	}
	if resourceType == "" || resourceID == "" {
		writeErr(w, http.StatusBadRequest, "需要 resource_type 与 resource_id")
		return
	}
	if resourceType == quota.TypeRole {
		writeErr(w, http.StatusBadRequest, "角色预设不可删除，请直接改限额")
		return
	}
	switch resourceType {
	case quota.TypeUser, quota.TypeUserBonus, quota.TypeWorkstation, quota.TypeEmployee:
	default:
		writeErr(w, http.StatusBadRequest, "resource_type 无效")
		return
	}
	detail := fmt.Sprintf("删除 %s %s 的 %s 配额策略", resourceType, resourceID, period)
	if old, err := d.Quota.Store().GetPolicy(r.Context(), resourceType, resourceID, period); err == nil && old != nil {
		detail = fmt.Sprintf("删除 %s %s 的 %s 配额（原 Token 限额 %d）", resourceType, resourceID, period, old.TokenLimit)
	}
	if err := d.Quota.Store().DeletePolicy(r.Context(), resourceType, resourceID, period); err != nil {
		if err == quota.ErrNotFound {
			writeErr(w, http.StatusNotFound, "策略不存在")
			return
		}
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	d.auditUser(r, sess, "quota.delete", map[string]string{
		"resource_type": resourceType,
		"resource_id":   resourceID,
		"period_type":   period,
		"summary":       detail,
	})
	writeJSON(w, http.StatusOK, map[string]string{"status": "deleted"})
}
