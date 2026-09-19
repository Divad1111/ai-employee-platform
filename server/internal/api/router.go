// Package api 提供 Admin REST HTTP 路由与 SSE。
// 设计依据：设计文档 §77、§105、§106。
package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/ai-employee-platform/server/internal/approval"
	"github.com/ai-employee-platform/server/internal/artifact"
	"github.com/ai-employee-platform/server/internal/audit"
	"github.com/ai-employee-platform/server/internal/auth"
	"github.com/ai-employee-platform/server/internal/certca"
	"github.com/ai-employee-platform/server/internal/employee"
	"github.com/ai-employee-platform/server/internal/enrollment"
	"github.com/ai-employee-platform/server/internal/eventbus"
	"github.com/ai-employee-platform/server/internal/feishu"
	"github.com/ai-employee-platform/server/internal/job"
	"github.com/ai-employee-platform/server/internal/knowledge"
	"github.com/ai-employee-platform/server/internal/message"
	"github.com/ai-employee-platform/server/internal/metrics"
	"github.com/ai-employee-platform/server/internal/notification"
	"github.com/ai-employee-platform/server/internal/permission"
	"github.com/ai-employee-platform/server/internal/registry"
	"github.com/ai-employee-platform/server/internal/scheduler"
	"github.com/ai-employee-platform/server/internal/secret"
	"github.com/ai-employee-platform/server/internal/session"
	"github.com/ai-employee-platform/server/internal/skill"
	"github.com/ai-employee-platform/server/internal/workstation"
	"github.com/ai-employee-platform/server/internal/workspace"
)

// Deps HTTP API 依赖。
type Deps struct {
	Auth            *auth.Service
	Enrollment      *enrollment.Service
	CA              *certca.Authority
	Employees       *employee.Service
	Workspaces      *workspace.Service
	Workstations    *workstation.Service
	Sessions        *session.Service
	Jobs            *job.Service
	Messages        *message.Service
	Bus             *eventbus.Bus
	Audit           *audit.Memory
	Feishu          *feishu.Service
	Scheduler       *scheduler.Service
	Notify          *notification.Service
	Secrets         secret.Store
	SecretMgr       *secret.Manager
	Permission      *permission.Engine
	PermissionStore permission.Store
	Approvals       *approval.Service
	Artifacts       *artifact.Service
	Registry        *registry.Service
	Metrics         *metrics.Registry
	Skills          *skill.Service
	Knowledge       *knowledge.Service
}

// NewRouter 构造 HTTP 路由。
func NewRouter(d Deps) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
	})

	var setupMu sync.Mutex
	mux.HandleFunc("GET /api/setup/status", d.handleSetupStatus)
	mux.HandleFunc("POST /api/setup/init", func(w http.ResponseWriter, r *http.Request) {
		setupMu.Lock()
		defer setupMu.Unlock()
		d.handleSetupInit(w, r)
	})

	mux.HandleFunc("POST /api/auth/login", d.handleLogin)
	mux.HandleFunc("POST /api/auth/logout", d.requireAuth(d.handleLogout))
	mux.HandleFunc("GET /api/auth/me", d.requireAuth(d.handleMe))

	mux.HandleFunc("POST /api/enrollment/tokens", d.requirePerm("enrollment.write", d.handleCreateToken))
	mux.HandleFunc("POST /api/enrollment/enroll", d.handleEnroll)
	mux.HandleFunc("POST /api/workstations/certificates/revoke", d.requirePermStepUp("workstation.write", d.handleRevoke))
	mux.HandleFunc("GET /api/system/ca", d.requirePerm("system.read", d.handleGetCA))

	// Dashboard
	mux.HandleFunc("GET /api/dashboard", d.requirePerm("employee.read", d.handleDashboard))

	// Employees
	mux.HandleFunc("GET /api/employees", d.requirePerm("employee.read", d.handleListEmployees))
	mux.HandleFunc("POST /api/employees", d.requirePerm("employee.write", d.handleCreateEmployee))
	mux.HandleFunc("GET /api/employees/{id}", d.requirePerm("employee.read", d.handleGetEmployee))
	mux.HandleFunc("GET /api/employees/{id}/overview", d.requirePerm("employee.read", d.handleEmployeeOverview))
	mux.HandleFunc("PATCH /api/employees/{id}", d.requirePerm("employee.write", d.handleUpdateEmployee))
	mux.HandleFunc("POST /api/employees/{id}/disable", d.requirePerm("employee.write", d.handleDisableEmployee))
	mux.HandleFunc("DELETE /api/employees/{id}", d.requirePermStepUp("employee.delete", d.handleDeleteEmployee))

	// Workspaces
	mux.HandleFunc("GET /api/workspaces", d.requirePerm("workspace.read", d.handleListWorkspaces))
	mux.HandleFunc("POST /api/workspaces", d.requirePerm("workspace.write", d.handleCreateWorkspace))
	mux.HandleFunc("GET /api/workspaces/{id}", d.requirePerm("workspace.read", d.handleGetWorkspace))
	mux.HandleFunc("PATCH /api/workspaces/{id}", d.requirePerm("workspace.write", d.handleUpdateWorkspace))
	mux.HandleFunc("POST /api/workspaces/{id}/bind", d.requirePerm("workspace.write", d.handleBindWorkspace))
	mux.HandleFunc("DELETE /api/workspaces/{id}", d.requirePermStepUp("workspace.write", d.handleDeleteWorkspace))

	// Workstations
	mux.HandleFunc("GET /api/workstations", d.requirePerm("workstation.read", d.handleListWorkstations))
	mux.HandleFunc("GET /api/workstations/{id}", d.requirePerm("workstation.read", d.handleGetWorkstation))
	mux.HandleFunc("PATCH /api/workstations/{id}", d.requirePerm("workstation.write", d.handleUpdateWorkstation))

	// Sessions
	mux.HandleFunc("GET /api/sessions", d.requirePerm("session.read", d.handleListSessions))
	mux.HandleFunc("POST /api/sessions", d.requirePerm("session.write", d.handleCreateSession))
	mux.HandleFunc("GET /api/sessions/{id}", d.requirePerm("session.read", d.handleGetSession))
	mux.HandleFunc("POST /api/sessions/{id}/transition", d.requirePerm("session.write", d.handleSessionTransition))

	// Jobs
	mux.HandleFunc("GET /api/jobs", d.requirePerm("job.read", d.handleListJobs))
	mux.HandleFunc("POST /api/jobs", d.requirePerm("job.write", d.handleCreateJob))
	mux.HandleFunc("GET /api/jobs/{id}", d.requirePerm("job.read", d.handleGetJob))
	mux.HandleFunc("GET /api/jobs/{id}/events", d.requirePerm("job.read", d.handleJobTimeline))
	mux.HandleFunc("POST /api/jobs/{id}/transition", d.requirePerm("job.write", d.handleJobTransition))
	mux.HandleFunc("POST /api/jobs/{id}/cancel", d.requirePerm("job.cancel", d.handleCancelJob))

	// Messages
	mux.HandleFunc("GET /api/messages", d.requirePerm("message.read", d.handleListMessages))
	mux.HandleFunc("POST /api/messages", d.requirePerm("message.write", d.handleSendMessage))

	// Audit / Settings / SSE
	mux.HandleFunc("GET /api/audit", d.requirePerm("audit.read", d.handleAuditEnhanced))
	mux.HandleFunc("GET /api/audit/export", d.requirePerm("audit.read", d.handleAuditExport))
	mux.HandleFunc("POST /api/audit/archive", d.requirePerm("system.write", d.handleAuditArchive))
	mux.HandleFunc("GET /api/settings", d.requirePerm("system.read", d.handleSettings))
	mux.HandleFunc("GET /api/events", d.requirePerm("employee.read", d.handleSSE))

	// Feishu / Secrets / Scheduler
	mux.HandleFunc("GET /api/integrations/feishu/config", d.requirePerm("system.read", d.handleFeishuGetConfig))
	mux.HandleFunc("PUT /api/integrations/feishu/config", d.requirePermStepUp("system.write", d.handleFeishuPutConfig))
	mux.HandleFunc("GET /api/integrations/feishu/bindings", d.requirePerm("employee.read", d.handleListFeishuBindings))
	mux.HandleFunc("POST /api/integrations/feishu/bindings", d.requirePerm("employee.write", d.handleFeishuBinding))
	mux.HandleFunc("POST /api/integrations/feishu/events", d.handleFeishuEvents) // 公开 Webhook

	// Skills / Knowledge / Permissions 写入
	mux.HandleFunc("GET /api/skills", d.requirePerm("employee.read", d.handleListSkills))
	mux.HandleFunc("POST /api/skills", d.requirePerm("employee.write", d.handleCreateSkill))
	mux.HandleFunc("PATCH /api/skills/{id}", d.requirePerm("employee.write", d.handleUpdateSkill))
	mux.HandleFunc("DELETE /api/skills/{id}", d.requirePerm("employee.write", d.handleDeleteSkill))
	mux.HandleFunc("POST /api/skills/bindings", d.requirePerm("employee.write", d.handleBindSkill))
	mux.HandleFunc("GET /api/knowledge", d.requirePerm("employee.read", d.handleListKnowledge))
	mux.HandleFunc("POST /api/knowledge", d.requirePerm("employee.write", d.handleCreateKnowledge))
	mux.HandleFunc("PATCH /api/knowledge/{id}", d.requirePerm("employee.write", d.handleUpdateKnowledge))
	mux.HandleFunc("DELETE /api/knowledge/{id}", d.requirePerm("employee.write", d.handleDeleteKnowledge))
	mux.HandleFunc("POST /api/knowledge/bindings", d.requirePerm("employee.write", d.handleBindKnowledge))
	mux.HandleFunc("PUT /api/permission/profiles", d.requirePerm("system.write", d.handleUpsertPermissionProfile))
	mux.HandleFunc("PUT /api/permission/rules", d.requirePerm("system.write", d.handleUpsertPermissionRule))

	mux.HandleFunc("POST /api/secrets", d.requirePerm("secret.write", d.handlePutSecret))
	mux.HandleFunc("GET /api/secrets", d.requirePerm("secret.read", d.handleListSecrets))
	mux.HandleFunc("GET /api/secrets/{id}", d.requirePerm("secret.read", d.handleGetSecret))
	mux.HandleFunc("DELETE /api/secrets/{id}", d.requirePermStepUp("secret.write", d.handleDeleteSecret))
	mux.HandleFunc("POST /api/secrets/{id}/rotate", d.requirePermStepUp("secret.write", d.handleRotateSecret))
	mux.HandleFunc("POST /api/secrets/bindings", d.requirePerm("secret.write", d.handleBindSecret))
	mux.HandleFunc("POST /api/employees/{employee_id}/secrets/resolve", d.requirePerm("secret.read", d.handleResolveSecrets))
	mux.HandleFunc("POST /api/scheduler/tick", d.requirePerm("job.write", d.handleSchedulerTick))

	// Permission / Approval / TOTP / Step-up（M7）
	mux.HandleFunc("POST /api/auth/step-up", d.requireAuth(d.handleStepUp))
	mux.HandleFunc("GET /api/auth/totp", d.requireAuth(d.handleTOTPStatus))
	mux.HandleFunc("POST /api/auth/totp/enroll", d.requireAuth(d.handleEnrollTOTP))
	mux.HandleFunc("GET /api/permission/profiles", d.requirePerm("system.read", d.handleListPermissionProfiles))
	mux.HandleFunc("GET /api/permission/rules", d.requirePerm("system.read", d.handleListPermissionRules))
	mux.HandleFunc("POST /api/permission/evaluate", d.requirePerm("approval.read", d.handleEvaluatePermission))
	mux.HandleFunc("GET /api/approvals", d.requirePerm("approval.read", d.handleListApprovals))
	mux.HandleFunc("GET /api/approvals/{id}", d.requirePerm("approval.read", d.handleGetApproval))
	mux.HandleFunc("POST /api/approvals/{id}/approve", d.requirePerm("approval.approve", d.handleApprove))
	mux.HandleFunc("POST /api/approvals/{id}/reject", d.requirePerm("approval.approve", d.handleRejectApproval))

	// Artifacts / Provider Registry / Metrics / Scheduler status（M9）
	mux.HandleFunc("GET /api/artifacts", d.requirePerm("job.read", d.handleListArtifacts))
	mux.HandleFunc("POST /api/artifacts", d.requirePerm("job.write", d.handleUploadArtifact))
	mux.HandleFunc("GET /api/artifacts/{id}/download", d.requirePerm("job.read", d.handleDownloadArtifact))
	mux.HandleFunc("GET /api/providers", d.requirePerm("system.read", d.handleListProviders))
	mux.HandleFunc("PUT /api/providers/{id}", d.requirePerm("system.write", d.handlePutProvider))
	mux.HandleFunc("POST /api/providers/versions", d.requirePerm("system.write", d.handleAddProviderVersion))
	mux.HandleFunc("GET /api/providers/versions", d.requirePerm("system.read", d.handleQueryProviderVersions))
	mux.HandleFunc("GET /api/providers/signing-key", d.handleSigningKey)
	mux.HandleFunc("GET /api/scheduler/status", d.requirePerm("job.read", d.handleSchedulerStatus))
	if d.Metrics != nil {
		mux.HandleFunc("GET /metrics", d.Metrics.Handler())
	}

	return mux
}

func (d Deps) handleSetupStatus(w http.ResponseWriter, r *http.Request) {
	if d.Auth == nil {
		writeJSON(w, http.StatusOK, map[string]any{
			"initialized": false,
			"needs_setup": true,
			"version":     "v1.0.0",
		})
		return
	}
	init, err := d.Auth.IsInitialized(r.Context())
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"initialized": init,
		"needs_setup": !init,
		"version":     "v1.0.0",
	})
}

type setupInitReq struct {
	Username    string `json:"username"`
	Password    string `json:"password"`
	DisplayName string `json:"display_name"`
	SystemName  string `json:"system_name"`
}

func (d Deps) handleSetupInit(w http.ResponseWriter, r *http.Request) {
	if d.Auth == nil {
		writeErr(w, http.StatusServiceUnavailable, "认证服务未就绪")
		return
	}
	init, err := d.Auth.IsInitialized(r.Context())
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	if init {
		writeErr(w, http.StatusConflict, "系统已完成首次部署设置，禁止重复初始化")
		return
	}

	var req setupInitReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeErr(w, http.StatusBadRequest, "无效请求体数据")
		return
	}
	if strings.TrimSpace(req.Username) == "" || strings.TrimSpace(req.Password) == "" {
		writeErr(w, http.StatusBadRequest, "管理员账号和密码不能为空")
		return
	}

	sess, err := d.Auth.InitAdmin(r.Context(), req.Username, req.Password, req.DisplayName, clientIP(r))
	if err != nil {
		if err == auth.ErrAlreadyInitialized {
			writeErr(w, http.StatusConflict, err.Error())
			return
		}
		if err == auth.ErrWeakPassword || err == auth.ErrInvalidUsername {
			writeErr(w, http.StatusBadRequest, err.Error())
			return
		}
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}

	writeJSON(w, http.StatusCreated, map[string]any{
		"status":      "ok",
		"token":       sess.Token,
		"expires_at":  sess.ExpiresAt,
		"system_name": req.SystemName,
		"user": map[string]any{
			"id":           sess.UserID,
			"username":     sess.Username,
			"display_name": req.DisplayName,
			"roles":        sess.Roles,
		},
	})
}

type loginReq struct {
	Username string `json:"username"`
	Password string `json:"password"`
	TOTP     string `json:"totp,omitempty"`
}

func (d Deps) handleLogin(w http.ResponseWriter, r *http.Request) {
	var req loginReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeErr(w, http.StatusBadRequest, "无效请求体")
		return
	}
	sess, err := d.Auth.Login(r.Context(), req.Username, req.Password, clientIP(r))
	if err != nil {
		if err == auth.ErrUserLocked {
			writeErr(w, http.StatusLocked, err.Error())
			return
		}
		writeErr(w, http.StatusUnauthorized, "登录失败: 用户名或密码错误")
		return
	}

	// 若该账号已绑定开启 TOTP 双因子认证
	if d.Approvals != nil && d.Approvals.TOTPEnabled(r.Context(), sess.UserID) {
		if req.TOTP == "" {
			_ = d.Auth.Logout(r.Context(), sess.Token, clientIP(r))
			writeJSON(w, http.StatusUnauthorized, map[string]any{
				"error":      "该账号已启用 TOTP 双因子认证，请输入 6 位动态验证码",
				"needs_totp": true,
			})
			return
		}
		if err := d.Approvals.VerifyUserTOTP(r.Context(), sess.UserID, req.TOTP, clientIP(r)); err != nil {
			_ = d.Auth.Logout(r.Context(), sess.Token, clientIP(r))
			writeJSON(w, http.StatusUnauthorized, map[string]any{
				"error":      "TOTP 动态验证码错误或已失效，请重新输入",
				"needs_totp": true,
			})
			return
		}
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"token":      sess.Token,
		"expires_at": sess.ExpiresAt,
		"user": map[string]any{
			"id":       sess.UserID,
			"username": sess.Username,
			"roles":    sess.Roles,
		},
	})
}

func (d Deps) handleLogout(w http.ResponseWriter, r *http.Request, sess *auth.Session) {
	_ = d.Auth.Logout(r.Context(), sess.Token, clientIP(r))
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (d Deps) handleMe(w http.ResponseWriter, _ *http.Request, sess *auth.Session) {
	writeJSON(w, http.StatusOK, map[string]any{
		"id":       sess.UserID,
		"username": sess.Username,
		"roles":    sess.Roles,
	})
}

type createTokenReq struct {
	Label    string `json:"label"`
	TTLHours int    `json:"ttl_hours"`
}

func (d Deps) handleCreateToken(w http.ResponseWriter, r *http.Request, sess *auth.Session) {
	var req createTokenReq
	_ = json.NewDecoder(r.Body).Decode(&req)
	ttl := 0
	if req.TTLHours > 0 {
		ttl = req.TTLHours
	}
	plain, meta, err := d.Enrollment.CreateToken(r.Context(), req.Label, sess.UserID, clientIP(r), time.Duration(ttl)*time.Hour)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{
		"token": plain, "id": meta.ID, "expires_at": meta.ExpiresAt, "label": meta.Label,
	})
}

type enrollReq struct {
	Token         string `json:"token"`
	WorkstationID string `json:"workstation_id"`
	CSRPEM        string `json:"csr_pem"`
}

func (d Deps) handleEnroll(w http.ResponseWriter, r *http.Request) {
	var req enrollReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeErr(w, http.StatusBadRequest, "无效请求体")
		return
	}
	caPEM, certPEM, rec, tokenLabel, err := d.Enrollment.Enroll(r.Context(), req.Token, req.WorkstationID, []byte(req.CSRPEM), clientIP(r))
	if err != nil {
		writeErr(w, http.StatusForbidden, err.Error())
		return
	}
	if d.Workstations != nil {
		wsName := strings.TrimSpace(tokenLabel)
		if wsName == "" {
			suffix := req.WorkstationID
			if len(suffix) > 6 {
				suffix = suffix[len(suffix)-6:]
			}
			wsName = "工作站-" + suffix
		}
		d.Workstations.EnsureRegistered(r.Context(), req.WorkstationID, wsName)
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"ca_pem": caPEM, "certificate": certPEM, "fingerprint": rec.Fingerprint,
		"workstation_id": rec.WorkstationID, "expires_at": rec.ExpiresAt, "status": rec.Status,
	})
}

type updateWorkstationReq struct {
	Name string `json:"name"`
}

func (d Deps) handleUpdateWorkstation(w http.ResponseWriter, r *http.Request, sess *auth.Session) {
	id := r.PathValue("id")
	if id == "" {
		writeErr(w, http.StatusBadRequest, "缺少工作站 ID")
		return
	}
	var req updateWorkstationReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeErr(w, http.StatusBadRequest, "无效请求体")
		return
	}
	req.Name = strings.TrimSpace(req.Name)
	if req.Name == "" {
		writeErr(w, http.StatusBadRequest, "工作站名称不能为空")
		return
	}
	if d.Workstations != nil && d.Workstations.Meta != nil {
		if err := d.Workstations.Meta.Upsert(r.Context(), id, req.Name); err != nil {
			writeErr(w, http.StatusInternalServerError, err.Error())
			return
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "id": id, "name": req.Name})
}

type revokeReq struct {
	Fingerprint string `json:"fingerprint"`
}

func (d Deps) handleRevoke(w http.ResponseWriter, r *http.Request, _ *auth.Session) {
	var req revokeReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.Fingerprint == "" {
		writeErr(w, http.StatusBadRequest, "需要 fingerprint")
		return
	}
	if err := d.CA.Revoke(req.Fingerprint); err != nil {
		writeErr(w, http.StatusNotFound, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "revoked"})
}

func (d Deps) handleGetCA(w http.ResponseWriter, _ *http.Request, _ *auth.Session) {
	writeJSON(w, http.StatusOK, map[string]string{"ca_pem": string(d.CA.CAPEM())})
}

func (d Deps) handleListEmployees(w http.ResponseWriter, r *http.Request, _ *auth.Session) {
	list, err := d.Employees.List(r.Context())
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": list})
}

func (d Deps) handleCreateEmployee(w http.ResponseWriter, r *http.Request, sess *auth.Session) {
	var in employee.CreateInput
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		writeErr(w, http.StatusBadRequest, "无效请求体")
		return
	}
	e, err := d.Employees.Create(r.Context(), in, sess.UserID, clientIP(r))
	if err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusCreated, e)
}

func (d Deps) handleGetEmployee(w http.ResponseWriter, r *http.Request, _ *auth.Session) {
	e, err := d.Employees.Get(r.Context(), r.PathValue("id"))
	if err != nil {
		writeErr(w, http.StatusNotFound, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, e)
}

func (d Deps) handleUpdateEmployee(w http.ResponseWriter, r *http.Request, sess *auth.Session) {
	var raw map[string]any
	if err := json.NewDecoder(r.Body).Decode(&raw); err != nil {
		writeErr(w, http.StatusBadRequest, "无效请求体")
		return
	}
	in := employee.UpdateInput{}
	if v, ok := raw["name"].(string); ok {
		in.Name = &v
	}
	if v, ok := raw["description"].(string); ok {
		in.Description = &v
	}
	if v, ok := raw["role_summary"].(string); ok {
		in.RoleSummary = &v
	}
	if v, ok := raw["default_provider"].(string); ok {
		in.DefaultProvider = &v
	}
	if v, ok := raw["workstation_id"].(string); ok {
		in.WorkstationID = &v
	}
	if v, ok := raw["workspace_id"].(string); ok {
		in.WorkspaceID = &v
	}
	if v, ok := raw["permission_profile"].(string); ok {
		in.PermissionProfile = &v
	}
	if v, ok := raw["status"].(string); ok {
		in.Status = &v
	}
	e, err := d.Employees.Update(r.Context(), r.PathValue("id"), in, sess.UserID, clientIP(r))
	if err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, e)
}

func (d Deps) handleDisableEmployee(w http.ResponseWriter, r *http.Request, sess *auth.Session) {
	e, err := d.Employees.Disable(r.Context(), r.PathValue("id"), sess.UserID, clientIP(r))
	if err != nil {
		writeErr(w, http.StatusNotFound, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, e)
}

func (d Deps) handleDeleteEmployee(w http.ResponseWriter, r *http.Request, sess *auth.Session) {
	if err := d.Employees.Delete(r.Context(), r.PathValue("id"), sess.UserID, clientIP(r)); err != nil {
		writeErr(w, http.StatusNotFound, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "deleted"})
}

func (d Deps) handleListWorkspaces(w http.ResponseWriter, r *http.Request, _ *auth.Session) {
	list, _ := d.Workspaces.List(r.Context())
	writeJSON(w, http.StatusOK, map[string]any{"items": list})
}

func (d Deps) handleCreateWorkspace(w http.ResponseWriter, r *http.Request, sess *auth.Session) {
	var in workspace.CreateInput
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		writeErr(w, http.StatusBadRequest, "无效请求体")
		return
	}
	ws, err := d.Workspaces.Create(r.Context(), in, sess.UserID, clientIP(r))
	if err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusCreated, ws)
}

func (d Deps) handleGetWorkspace(w http.ResponseWriter, r *http.Request, _ *auth.Session) {
	ws, err := d.Workspaces.Get(r.Context(), r.PathValue("id"))
	if err != nil {
		writeErr(w, http.StatusNotFound, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, ws)
}

func (d Deps) handleUpdateWorkspace(w http.ResponseWriter, r *http.Request, sess *auth.Session) {
	var body struct {
		WorkstationID string `json:"workstation_id"`
		Path          string `json:"path"`
		Repository    string `json:"repository"`
		Branch        string `json:"branch"`
	}
	_ = json.NewDecoder(r.Body).Decode(&body)
	ws, err := d.Workspaces.Update(r.Context(), r.PathValue("id"), body.WorkstationID, body.Path, body.Repository, body.Branch, sess.UserID, clientIP(r))
	if err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, ws)
}

func (d Deps) handleBindWorkspace(w http.ResponseWriter, r *http.Request, sess *auth.Session) {
	var body struct {
		EmployeeID string `json:"employee_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.EmployeeID == "" {
		writeErr(w, http.StatusBadRequest, "需要 employee_id")
		return
	}
	ws, err := d.Workspaces.BindEmployee(r.Context(), r.PathValue("id"), body.EmployeeID, sess.UserID, clientIP(r))
	if err != nil {
		writeErr(w, http.StatusConflict, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, ws)
}

func (d Deps) handleDeleteWorkspace(w http.ResponseWriter, r *http.Request, sess *auth.Session) {
	if err := d.Workspaces.Delete(r.Context(), r.PathValue("id"), sess.UserID, clientIP(r)); err != nil {
		writeErr(w, http.StatusNotFound, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "deleted"})
}

func (d Deps) handleListWorkstations(w http.ResponseWriter, r *http.Request, _ *auth.Session) {
	writeJSON(w, http.StatusOK, map[string]any{"items": d.Workstations.List(r.Context())})
}

func (d Deps) handleGetWorkstation(w http.ResponseWriter, r *http.Request, _ *auth.Session) {
	writeJSON(w, http.StatusOK, d.Workstations.Get(r.Context(), r.PathValue("id")))
}

func (d Deps) handleListSessions(w http.ResponseWriter, r *http.Request, _ *auth.Session) {
	list, _ := d.Sessions.List(r.Context())
	writeJSON(w, http.StatusOK, map[string]any{"items": list})
}

func (d Deps) handleCreateSession(w http.ResponseWriter, r *http.Request, sess *auth.Session) {
	var in session.CreateInput
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		writeErr(w, http.StatusBadRequest, "无效请求体")
		return
	}
	s, err := d.Sessions.Create(r.Context(), in, sess.UserID, clientIP(r))
	if err != nil {
		writeErr(w, http.StatusConflict, err.Error())
		return
	}
	writeJSON(w, http.StatusCreated, s)
}

func (d Deps) handleGetSession(w http.ResponseWriter, r *http.Request, _ *auth.Session) {
	s, err := d.Sessions.Get(r.Context(), r.PathValue("id"))
	if err != nil {
		writeErr(w, http.StatusNotFound, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, s)
}

func (d Deps) handleSessionTransition(w http.ResponseWriter, r *http.Request, sess *auth.Session) {
	var body struct {
		Status string `json:"status"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.Status == "" {
		writeErr(w, http.StatusBadRequest, "需要 status")
		return
	}
	s, err := d.Sessions.Transition(r.Context(), r.PathValue("id"), body.Status, sess.UserID, clientIP(r))
	if err != nil {
		writeErr(w, http.StatusConflict, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, s)
}

func (d Deps) handleListJobs(w http.ResponseWriter, r *http.Request, _ *auth.Session) {
	list, _ := d.Jobs.List(r.Context())
	writeJSON(w, http.StatusOK, map[string]any{"items": list})
}

func (d Deps) handleCreateJob(w http.ResponseWriter, r *http.Request, sess *auth.Session) {
	var in job.CreateInput
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		writeErr(w, http.StatusBadRequest, "无效请求体")
		return
	}
	j, dup, err := d.Jobs.Create(r.Context(), in, sess.UserID, clientIP(r))
	if err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	code := http.StatusCreated
	if dup {
		code = http.StatusOK
	}
	writeJSON(w, code, map[string]any{"job": j, "idempotent": dup})
}

func (d Deps) handleGetJob(w http.ResponseWriter, r *http.Request, _ *auth.Session) {
	j, err := d.Jobs.Get(r.Context(), r.PathValue("id"))
	if err != nil {
		writeErr(w, http.StatusNotFound, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, j)
}

func (d Deps) handleJobTimeline(w http.ResponseWriter, r *http.Request, _ *auth.Session) {
	evs, err := d.Jobs.Timeline(r.Context(), r.PathValue("id"))
	if err != nil {
		writeErr(w, http.StatusNotFound, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": evs})
}

func (d Deps) handleJobTransition(w http.ResponseWriter, r *http.Request, sess *auth.Session) {
	var body struct {
		Status  string            `json:"status"`
		Payload map[string]string `json:"payload"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.Status == "" {
		writeErr(w, http.StatusBadRequest, "需要 status")
		return
	}
	j, err := d.Jobs.Transition(r.Context(), r.PathValue("id"), body.Status, sess.UserID, clientIP(r), body.Payload)
	if err != nil {
		writeErr(w, http.StatusConflict, err.Error())
		return
	}
	if d.Notify != nil {
		_ = d.Notify.OnJobTerminal(r.Context(), j)
	}
	if d.Scheduler != nil && j.WorkstationID != "" {
		d.Scheduler.Release(j.WorkstationID)
	}
	writeJSON(w, http.StatusOK, j)
}

func (d Deps) handleCancelJob(w http.ResponseWriter, r *http.Request, sess *auth.Session) {
	j, err := d.Jobs.Cancel(r.Context(), r.PathValue("id"), sess.UserID, clientIP(r))
	if err != nil {
		writeErr(w, http.StatusConflict, err.Error())
		return
	}
	if d.Notify != nil {
		_ = d.Notify.OnJobTerminal(r.Context(), j)
	}
	if d.Scheduler != nil && j.WorkstationID != "" {
		d.Scheduler.Release(j.WorkstationID)
	}
	writeJSON(w, http.StatusOK, j)
}

func (d Deps) handleListMessages(w http.ResponseWriter, r *http.Request, _ *auth.Session) {
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	list, _ := d.Messages.List(r.Context(), limit)
	writeJSON(w, http.StatusOK, map[string]any{"items": list})
}

func (d Deps) handleSendMessage(w http.ResponseWriter, r *http.Request, sess *auth.Session) {
	var in message.SendInput
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		writeErr(w, http.StatusBadRequest, "无效请求体")
		return
	}
	msg, err := d.Messages.Send(r.Context(), in, sess.UserID, clientIP(r))
	if err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusCreated, msg)
}

func (d Deps) handleAudit(w http.ResponseWriter, r *http.Request, sess *auth.Session) {
	d.handleAuditEnhanced(w, r, sess)
}

func (d Deps) handleSettings(w http.ResponseWriter, _ *http.Request, _ *auth.Session) {
	writeJSON(w, http.StatusOK, map[string]any{
		"heartbeat_interval_sec": 5,
		"offline_after_sec":      15,
		"max_sessions_per_employee": 1,
		"note": "Secret 类配置不在此回显",
	})
}

func (d Deps) handleSSE(w http.ResponseWriter, r *http.Request, _ *auth.Session) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		writeErr(w, http.StatusInternalServerError, "不支持 SSE")
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	ch := d.Bus.Subscribe(32)
	defer d.Bus.Unsubscribe(ch)
	// 先推送最近事件
	for _, ev := range d.Bus.Recent(5) {
		writeSSE(w, flusher, ev)
	}
	ctx := r.Context()
	for {
		select {
		case <-ctx.Done():
			return
		case ev, ok := <-ch:
			if !ok {
				return
			}
			writeSSE(w, flusher, ev)
		}
	}
}

func writeSSE(w http.ResponseWriter, flusher http.Flusher, ev eventbus.Event) {
	payload, _ := json.Marshal(ev)
	fmt.Fprintf(w, "event: %s\ndata: %s\n\n", ev.Type, payload)
	flusher.Flush()
}

type authed func(http.ResponseWriter, *http.Request, *auth.Session)

func (d Deps) requireAuth(next authed) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		sess, err := d.Auth.Authenticate(r.Context(), bearer(r))
		if err != nil {
			writeErr(w, http.StatusUnauthorized, "未登录")
			return
		}
		next(w, r, sess)
	}
}

func (d Deps) requirePerm(code string, next authed) http.HandlerFunc {
	return d.requireAuth(func(w http.ResponseWriter, r *http.Request, sess *auth.Session) {
		if err := d.Auth.Authorize(r.Context(), sess, code); err != nil {
			writeErr(w, http.StatusForbidden, "权限不足")
			return
		}
		next(w, r, sess)
	})
}

// requirePermStepUp 权限 + 短时二次认证（吊销证书/删 Employee 等）。
func (d Deps) requirePermStepUp(code string, next authed) http.HandlerFunc {
	return d.requirePerm(code, func(w http.ResponseWriter, r *http.Request, sess *auth.Session) {
		if err := d.Auth.RequireStepUp(sess); err != nil {
			writeErr(w, http.StatusForbidden, err.Error())
			return
		}
		next(w, r, sess)
	})
}

func bearer(r *http.Request) string {
	h := r.Header.Get("Authorization")
	if strings.HasPrefix(strings.ToLower(h), "bearer ") {
		return strings.TrimSpace(h[7:])
	}
	return r.Header.Get("X-Session-Token")
}

func clientIP(r *http.Request) string {
	if x := r.Header.Get("X-Forwarded-For"); x != "" {
		return strings.TrimSpace(strings.Split(x, ",")[0])
	}
	host := r.RemoteAddr
	if i := strings.LastIndex(host, ":"); i >= 0 {
		return host[:i]
	}
	return host
}

func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(v)
}

func writeErr(w http.ResponseWriter, code int, msg string) {
	writeJSON(w, code, map[string]string{"error": msg})
}
