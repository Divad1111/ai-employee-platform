package api

import (
	"encoding/json"
	"net/http"
	"strings"

	"github.com/ai-employee-platform/server/internal/auth"
	"github.com/ai-employee-platform/server/internal/mcp"
)

// ---------- MCP Server API ----------

func (d Deps) handleListMCPServers(w http.ResponseWriter, r *http.Request, _ *auth.Session) {
	if d.MCP == nil {
		writeJSON(w, http.StatusOK, map[string]any{"items": []any{}})
		return
	}
	items, err := d.MCP.ListServers(r.Context())
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items})
}

func (d Deps) handleGetMCPServer(w http.ResponseWriter, r *http.Request, _ *auth.Session) {
	if d.MCP == nil {
		writeErr(w, http.StatusNotFound, "MCP 服务未就绪")
		return
	}
	srv, err := d.MCP.GetServer(r.Context(), r.PathValue("id"))
	if err != nil {
		writeErr(w, http.StatusNotFound, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"server": srv})
}

func (d Deps) handleCreateMCPServer(w http.ResponseWriter, r *http.Request, sess *auth.Session) {
	if d.MCP == nil {
		writeErr(w, http.StatusInternalServerError, "MCP 服务未就绪")
		return
	}
	var in mcp.CreateServerInput
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		writeErr(w, http.StatusBadRequest, "无效 JSON 数据")
		return
	}
	srv, err := d.MCP.CreateServer(r.Context(), in)
	if err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	if d.Audit != nil {
		d.Audit.Log(r.Context(), "USER", sess.UserID, "mcp.server.create", "success", clientIP(r), map[string]string{
			"mcp_server_id": srv.ID, "name": srv.Name, "transport": srv.Transport,
		})
	}
	writeJSON(w, http.StatusCreated, map[string]any{"server": srv})
}

func (d Deps) handleUpdateMCPServer(w http.ResponseWriter, r *http.Request, sess *auth.Session) {
	if d.MCP == nil {
		writeErr(w, http.StatusInternalServerError, "MCP 服务未就绪")
		return
	}
	id := r.PathValue("id")
	var in mcp.CreateServerInput
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		writeErr(w, http.StatusBadRequest, "无效 JSON 数据")
		return
	}
	srv, err := d.MCP.UpdateServer(r.Context(), id, in)
	if err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	if d.Audit != nil {
		d.Audit.Log(r.Context(), "USER", sess.UserID, "mcp.server.update", "success", clientIP(r), map[string]string{
			"mcp_server_id": srv.ID, "name": srv.Name,
		})
	}
	writeJSON(w, http.StatusOK, map[string]any{"server": srv})
}

func (d Deps) handleDeleteMCPServer(w http.ResponseWriter, r *http.Request, sess *auth.Session) {
	if d.MCP == nil {
		writeErr(w, http.StatusInternalServerError, "MCP 服务未就绪")
		return
	}
	id := r.PathValue("id")
	if err := d.MCP.DeleteServer(r.Context(), id); err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	if d.Audit != nil {
		d.Audit.Log(r.Context(), "USER", sess.UserID, "mcp.server.delete", "success", clientIP(r), map[string]string{
			"mcp_server_id": id,
		})
	}
	writeJSON(w, http.StatusOK, map[string]string{"deleted": id})
}

// ---------- Credentials API ----------

func (d Deps) handleListCredentials(w http.ResponseWriter, r *http.Request, _ *auth.Session) {
	if d.MCP == nil {
		writeJSON(w, http.StatusOK, map[string]any{"items": []any{}})
		return
	}
	ownerType := r.URL.Query().Get("owner_type")
	ownerID := r.URL.Query().Get("owner_id")
	items, err := d.MCP.ListCredentials(r.Context(), ownerType, ownerID)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items})
}

func (d Deps) handleGetCredential(w http.ResponseWriter, r *http.Request, _ *auth.Session) {
	if d.MCP == nil {
		writeErr(w, http.StatusNotFound, "MCP 服务未就绪")
		return
	}
	c, err := d.MCP.GetCredential(r.Context(), r.PathValue("id"))
	if err != nil {
		writeErr(w, http.StatusNotFound, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"credential": c})
}

func (d Deps) handleCreateCredential(w http.ResponseWriter, r *http.Request, sess *auth.Session) {
	if d.MCP == nil {
		writeErr(w, http.StatusInternalServerError, "MCP 服务未就绪")
		return
	}
	var in mcp.CreateCredentialInput
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		writeErr(w, http.StatusBadRequest, "无效 JSON 数据")
		return
	}
	in.CreatedBy = sess.UserID
	if in.OwnerID == "" && strings.EqualFold(in.OwnerType, mcp.OwnerTypeUser) {
		in.OwnerID = sess.UserID
	}
	c, err := d.MCP.CreateCredential(r.Context(), in)
	if err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	if d.Audit != nil {
		d.Audit.Log(r.Context(), "USER", sess.UserID, "mcp.credential.create", "success", clientIP(r), map[string]string{
			"credential_id": c.ID, "name": c.CredentialName, "provider": c.Provider,
		})
	}
	writeJSON(w, http.StatusCreated, map[string]any{"credential": c})
}

func (d Deps) handleUpdateCredential(w http.ResponseWriter, r *http.Request, sess *auth.Session) {
	if d.MCP == nil {
		writeErr(w, http.StatusInternalServerError, "MCP 服务未就绪")
		return
	}
	id := r.PathValue("id")
	var in mcp.CreateCredentialInput
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		writeErr(w, http.StatusBadRequest, "无效 JSON 数据")
		return
	}
	in.CreatedBy = sess.UserID
	c, err := d.MCP.UpdateCredential(r.Context(), id, in)
	if err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	if d.Audit != nil {
		d.Audit.Log(r.Context(), "USER", sess.UserID, "mcp.credential.update", "success", clientIP(r), map[string]string{
			"credential_id": c.ID, "name": c.CredentialName,
		})
	}
	writeJSON(w, http.StatusOK, map[string]any{"credential": c})
}

func (d Deps) handleDeleteCredential(w http.ResponseWriter, r *http.Request, sess *auth.Session) {
	if d.MCP == nil {
		writeErr(w, http.StatusInternalServerError, "MCP 服务未就绪")
		return
	}
	id := r.PathValue("id")
	if err := d.MCP.DeleteCredential(r.Context(), id); err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	if d.Audit != nil {
		d.Audit.Log(r.Context(), "USER", sess.UserID, "mcp.credential.delete", "success", clientIP(r), map[string]string{
			"credential_id": id,
		})
	}
	writeJSON(w, http.StatusOK, map[string]string{"deleted": id})
}

// ---------- Employee MCP Bindings API ----------

func (d Deps) handleListEmployeeMCPBindings(w http.ResponseWriter, r *http.Request, _ *auth.Session) {
	if d.MCP == nil {
		writeJSON(w, http.StatusOK, map[string]any{"items": []any{}})
		return
	}
	empID := r.PathValue("id")
	items, err := d.MCP.ListBindingsByEmployee(r.Context(), empID)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items})
}

func (d Deps) handleCreateEmployeeMCPBinding(w http.ResponseWriter, r *http.Request, sess *auth.Session) {
	if d.MCP == nil {
		writeErr(w, http.StatusInternalServerError, "MCP 服务未就绪")
		return
	}
	empID := r.PathValue("id")
	var in mcp.BindEmployeeInput
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		writeErr(w, http.StatusBadRequest, "无效 JSON 数据")
		return
	}
	b, err := d.MCP.BindEmployee(r.Context(), empID, in)
	if err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	if d.Audit != nil {
		d.Audit.Log(r.Context(), "USER", sess.UserID, "mcp.binding.create", "success", clientIP(r), map[string]string{
			"binding_id": b.ID, "employee_id": empID, "mcp_server_id": b.MCPServerID,
		})
	}
	writeJSON(w, http.StatusCreated, map[string]any{"binding": b})
}

func (d Deps) handleUpdateEmployeeMCPBinding(w http.ResponseWriter, r *http.Request, sess *auth.Session) {
	if d.MCP == nil {
		writeErr(w, http.StatusInternalServerError, "MCP 服务未就绪")
		return
	}
	bindingID := r.PathValue("bindingId")
	var in mcp.BindEmployeeInput
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		writeErr(w, http.StatusBadRequest, "无效 JSON 数据")
		return
	}
	b, err := d.MCP.UpdateBinding(r.Context(), bindingID, in)
	if err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	if d.Audit != nil {
		d.Audit.Log(r.Context(), "USER", sess.UserID, "mcp.binding.update", "success", clientIP(r), map[string]string{
			"binding_id": b.ID, "employee_id": b.EmployeeID, "mcp_server_id": b.MCPServerID,
		})
	}
	writeJSON(w, http.StatusOK, map[string]any{"binding": b})
}

func (d Deps) handleDeleteEmployeeMCPBinding(w http.ResponseWriter, r *http.Request, sess *auth.Session) {
	if d.MCP == nil {
		writeErr(w, http.StatusInternalServerError, "MCP 服务未就绪")
		return
	}
	bindingID := r.PathValue("bindingId")
	if err := d.MCP.UnbindEmployee(r.Context(), bindingID); err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	if d.Audit != nil {
		d.Audit.Log(r.Context(), "USER", sess.UserID, "mcp.binding.delete", "success", clientIP(r), map[string]string{
			"binding_id": bindingID,
		})
	}
	writeJSON(w, http.StatusOK, map[string]string{"deleted": bindingID})
}
