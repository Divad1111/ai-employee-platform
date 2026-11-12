package api

import (
	"encoding/json"
	"github.com/ai-employee-platform/gen/workspacedir"
	"github.com/ai-employee-platform/server/internal/auth"
	"net/http"
	"slices"
)

func (d Deps) handleWorkspaceRoots(w http.ResponseWriter, r *http.Request, s *auth.Session) {
	id := r.PathValue("id")
	if !d.canManageWorkstationShare(r.Context(), s, id) && !d.scopeAll(r, s, "workstation.write") {
		writeErr(w, 403, "只有创建者或工作站管理员可以设置根目录")
		return
	}
	var body struct {
		Roots []string `json:"roots"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.Roots == nil {
		writeErr(w, 400, "请提供根目录列表")
		return
	}
	if err := workspacedir.ValidateRoots(body.Roots); err != nil {
		writeErr(w, 400, err.Error())
		return
	}
	if err := d.Workstations.Meta.SetWorkspaceRoots(r.Context(), id, body.Roots); err != nil {
		writeErr(w, 400, err.Error())
		return
	}
	d.auditUser(r, s, "workstation.workspace_roots", map[string]string{"id": id})
	writeJSON(w, 200, body)
}
func (d Deps) checkWorkspaceLocation(w http.ResponseWriter, r *http.Request, node, root, name string) bool {
	if !workspacedir.ValidName(name) {
		writeErr(w, 400, "工作区必须填写单个合法目录名，例如 test")
		return false
	}
	if d.Workstations == nil || root == "" {
		writeErr(w, 400, "请先为工作站配置根目录，然后选择其中一个")
		return false
	}
	roots, err := d.Workstations.Meta.WorkspaceRoots(r.Context(), node)
	if err != nil || !slices.Contains(roots, root) {
		writeErr(w, 400, "所选根目录不属于该工作站")
		return false
	}
	return true
}
