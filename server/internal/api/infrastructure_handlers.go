package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/ai-employee-platform/server/internal/auth"
	"github.com/ai-employee-platform/server/internal/infrastructure"
	"github.com/gorilla/websocket"
	"golang.org/x/crypto/ssh"
)

// infrastructureRoutes 统一要求 ALL 范围，高危部署、终端与明文查看还需短时二次认证。
func (d Deps) infrastructureRoutes(mux *http.ServeMux) {
	register := func(pattern, perm string, step bool, h authed) {
		handler := d.onlyAll(perm, func(w http.ResponseWriter, r *http.Request, sess *auth.Session) {
			if d.Infrastructure == nil {
				writeErr(w, 503, "基础设施服务未就绪")
				return
			}
			h(w, r, sess)
		})
		if step {
			mux.HandleFunc(pattern, d.requirePermStepUp(perm, handler))
		} else {
			mux.HandleFunc(pattern, d.requirePerm(perm, handler))
		}
	}
	register("GET /api/infrastructure", "infra.read", false, d.handleInfrastructure)
	register("POST /api/infrastructure/clusters", "infra.manage", false, d.handleInfraCluster)
	register("PUT /api/infrastructure/clusters/{id}", "infra.manage", false, d.handleInfraCluster)
	register("POST /api/infrastructure/clusters/{id}/sync", "infra.manage", false, d.handleInfraSync)
	register("DELETE /api/infrastructure/clusters/{id}", "infra.manage", true, d.handleInfraDeleteCluster)
	register("POST /api/infrastructure/hosts", "infra.manage", false, d.handleInfraHost)
	register("PUT /api/infrastructure/hosts/{id}", "infra.manage", false, d.handleInfraHost)
	register("POST /api/infrastructure/hosts/{id}/discover-address", "infra.manage", false, d.handleInfraDiscover)
	register("POST /api/infrastructure/hosts/{id}/probe-key", "infra.manage", false, d.handleInfraProbe)
	register("POST /api/infrastructure/hosts/{id}/terminal-ticket", "infra.console", true, d.handleInfraTerminalTicket)
	register("POST /api/infrastructure/software", "infra.manage", false, d.handleInfraSoftware)
	register("PUT /api/infrastructure/software/{id}", "infra.manage", false, d.handleInfraSoftware)
	register("POST /api/infrastructure/software/{id}/check", "infra.manage", false, d.handleInfraCheck)
	register("POST /api/infrastructure/releases", "infra.manage", false, d.handleInfraRelease)
	register("GET /api/infrastructure/releases/{id}/download", "infra.read", false, d.handleInfraDownload)
	register("PUT /api/infrastructure/policies", "infra.manage", true, d.handleInfraPolicy)
	register("POST /api/infrastructure/operations", "infra.deploy", true, d.handleInfraOperation)
	register("POST /api/infrastructure/operations/{id}/retry", "infra.deploy", true, d.handleInfraRetry)
	register("POST /api/infrastructure/operations/{id}/cancel", "infra.deploy", true, d.handleInfraCancel)
	register("GET /api/infrastructure/credentials", "infra.credential", false, d.handleInfraCredentials)
	register("POST /api/infrastructure/credentials", "infra.credential", true, d.handleInfraCredential)
	register("POST /api/infrastructure/credentials/{id}/reveal", "infra.credential", true, d.handleInfraReveal)
	mux.HandleFunc("GET /api/infrastructure/terminal", d.handleInfraTerminal)
}

func infraError(w http.ResponseWriter, err error) {
	code := http.StatusBadRequest
	if errors.Is(err, infrastructure.ErrNotFound) {
		code = 404
	}
	if errors.Is(err, infrastructure.ErrConflict) {
		code = 409
	}
	writeErr(w, code, err.Error())
}
func infraDecode(w http.ResponseWriter, r *http.Request, v any) bool {
	r.Body = http.MaxBytesReader(w, r.Body, 2<<20)
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(v); err != nil {
		writeErr(w, 400, "请求内容无效或包含不支持的字段")
		return false
	}
	return true
}

func (d Deps) handleInfrastructure(w http.ResponseWriter, r *http.Request, _ *auth.Session) {
	s := d.Infrastructure
	clusters, err := s.Clusters(r.Context())
	if err != nil {
		infraError(w, err)
		return
	}
	hosts, err := s.Hosts(r.Context())
	if err != nil {
		infraError(w, err)
		return
	}
	software, err := s.Software(r.Context())
	if err != nil {
		infraError(w, err)
		return
	}
	releases, err := s.Releases(r.Context())
	if err != nil {
		infraError(w, err)
		return
	}
	installs, err := s.Installations(r.Context())
	if err != nil {
		infraError(w, err)
		return
	}
	policies, err := s.Policies(r.Context())
	if err != nil {
		infraError(w, err)
		return
	}
	ops, err := s.Store.Operations(r.Context())
	if err != nil {
		infraError(w, err)
		return
	}
	// 工作站在线状态是独立观测值，不能以虚拟机开机状态替代。
	states := map[string]string{}
	if d.Workstations != nil {
		for _, h := range hosts {
			if h.WorkstationID != "" {
				states[h.WorkstationID] = d.Workstations.Get(r.Context(), h.WorkstationID).Status
			}
		}
	}
	writeJSON(w, 200, map[string]any{"clusters": clusters, "hosts": hosts, "software": software, "releases": releases, "installations": installs, "policies": policies, "operations": ops, "workstation_states": states})
}
func (d Deps) handleInfraCluster(w http.ResponseWriter, r *http.Request, sess *auth.Session) {
	var v infrastructure.Cluster
	if !infraDecode(w, r, &v) {
		return
	}
	v.ID = r.PathValue("id")
	v, err := d.Infrastructure.SaveCluster(r.Context(), v)
	if err != nil {
		infraError(w, err)
		return
	}
	d.auditUser(r, sess, "infra.cluster.save", map[string]string{"id": v.ID})
	writeJSON(w, 200, v)
}
func (d Deps) handleInfraSync(w http.ResponseWriter, r *http.Request, sess *auth.Session) {
	ctx, cancel := context.WithTimeout(r.Context(), 60*time.Second)
	defer cancel()
	if err := d.Infrastructure.Sync(ctx, r.PathValue("id"), sess.UserID); err != nil {
		infraError(w, err)
		return
	}
	d.auditUser(r, sess, "infra.cluster.sync", map[string]string{"id": r.PathValue("id")})
	writeJSON(w, 200, map[string]bool{"ok": true})
}
func (d Deps) handleInfraDeleteCluster(w http.ResponseWriter, r *http.Request, sess *auth.Session) {
	if err := d.Infrastructure.DeleteCluster(r.Context(), r.PathValue("id")); err != nil {
		infraError(w, err)
		return
	}
	d.auditUser(r, sess, "infra.cluster.delete", map[string]string{"id": r.PathValue("id")})
	w.WriteHeader(204)
}
func (d Deps) handleInfraHost(w http.ResponseWriter, r *http.Request, sess *auth.Session) {
	var v infrastructure.Host
	if !infraDecode(w, r, &v) {
		return
	}
	v.ID = r.PathValue("id")
	v, err := d.Infrastructure.SaveHost(r.Context(), v)
	if err != nil {
		infraError(w, err)
		return
	}
	d.auditUser(r, sess, "infra.host.save", map[string]string{"id": v.ID})
	writeJSON(w, 200, v)
}
func (d Deps) handleInfraDiscover(w http.ResponseWriter, r *http.Request, sess *auth.Session) {
	v, err := d.Infrastructure.DiscoverAddress(r.Context(), r.PathValue("id"), sess.UserID)
	if err != nil {
		infraError(w, err)
		return
	}
	writeJSON(w, 200, v)
}
func (d Deps) handleInfraProbe(w http.ResponseWriter, r *http.Request, _ *auth.Session) {
	h, err := d.Infrastructure.Host(r.Context(), r.PathValue("id"))
	if err != nil {
		infraError(w, err)
		return
	}
	key, err := d.Infrastructure.Remote.Probe(r.Context(), h)
	if err != nil {
		infraError(w, err)
		return
	}
	writeJSON(w, 200, map[string]string{"fingerprint": key})
}
func (d Deps) handleInfraSoftware(w http.ResponseWriter, r *http.Request, sess *auth.Session) {
	var v infrastructure.Software
	if !infraDecode(w, r, &v) {
		return
	}
	v.ID = r.PathValue("id")
	v, err := d.Infrastructure.SaveSoftware(r.Context(), v)
	if err != nil {
		infraError(w, err)
		return
	}
	d.auditUser(r, sess, "infra.software.save", map[string]string{"id": v.ID})
	writeJSON(w, 200, v)
}
func (d Deps) handleInfraCheck(w http.ResponseWriter, r *http.Request, sess *auth.Session) {
	items, err := d.Infrastructure.CheckUpdates(r.Context(), r.PathValue("id"), sess.UserID)
	if err != nil {
		infraError(w, err)
		return
	}
	writeJSON(w, 200, map[string]any{"items": items})
}
func (d Deps) handleInfraRelease(w http.ResponseWriter, r *http.Request, sess *auth.Session) {
	var v infrastructure.Release
	var content io.Reader
	if strings.HasPrefix(r.Header.Get("Content-Type"), "multipart/form-data") {
		r.Body = http.MaxBytesReader(w, r.Body, infrastructure.MaxPackageSize+(2<<20))
		if err := r.ParseMultipartForm(8 << 20); err != nil {
			writeErr(w, 400, "安装包上传失败或超过 512 MiB")
			return
		}
		if r.MultipartForm != nil {
			defer r.MultipartForm.RemoveAll()
		}
		if err := json.Unmarshal([]byte(r.FormValue("metadata")), &v); err != nil {
			writeErr(w, 400, "版本信息格式无效")
			return
		}
		f, _, err := r.FormFile("file")
		if err != nil {
			writeErr(w, 400, "请选择安装包")
			return
		}
		defer f.Close()
		content = f
	} else if !infraDecode(w, r, &v) {
		return
	}
	v, err := d.Infrastructure.AddRelease(r.Context(), v, content)
	if err != nil {
		infraError(w, err)
		return
	}
	d.auditUser(r, sess, "infra.release.create", map[string]string{"id": v.ID, "software_id": v.SoftwareID, "sha256": v.SHA256})
	writeJSON(w, 201, v)
}
func (d Deps) handleInfraDownload(w http.ResponseWriter, r *http.Request, sess *auth.Session) {
	v, f, err := d.Infrastructure.CachedPackage(r.Context(), r.PathValue("id"), sess.UserID)
	if err != nil {
		infraError(w, err)
		return
	}
	defer f.Close()
	w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=%q", d.Infrastructure.PackageName(v)))
	w.Header().Set("Content-Type", "application/octet-stream")
	w.Header().Set("Cache-Control", "no-store")
	http.ServeContent(w, r, d.Infrastructure.PackageName(v), v.CreatedAt, f)
}
func (d Deps) handleInfraPolicy(w http.ResponseWriter, r *http.Request, sess *auth.Session) {
	var v infrastructure.Policy
	if !infraDecode(w, r, &v) {
		return
	}
	if v.AutoInstall && !d.scopeAll(r, sess, "infra.deploy") {
		writeErr(w, 403, "启用自动部署需要全部资源范围的部署权限")
		return
	}
	v, err := d.Infrastructure.SavePolicy(r.Context(), v)
	if err != nil {
		infraError(w, err)
		return
	}
	d.auditUser(r, sess, "infra.policy.save", map[string]string{"id": v.ID})
	writeJSON(w, 200, v)
}

func (d Deps) handleInfraCancel(w http.ResponseWriter, r *http.Request, sess *auth.Session) {
	if err := d.Infrastructure.Store.Cancel(r.Context(), r.PathValue("id")); err != nil {
		infraError(w, err)
		return
	}
	d.auditUser(r, sess, "infra.operation.cancel", map[string]string{"id": r.PathValue("id")})
	writeJSON(w, 200, map[string]bool{"ok": true})
}
func (d Deps) handleInfraOperation(w http.ResponseWriter, r *http.Request, sess *auth.Session) {
	var v infrastructure.Spec
	if !infraDecode(w, r, &v) {
		return
	}
	v.Automatic = false
	op, err := d.Infrastructure.Queue(r.Context(), v, sess.UserID, r.Header.Get("Idempotency-Key"))
	if err != nil {
		infraError(w, err)
		return
	}
	d.auditUser(r, sess, "infra.operation.create", map[string]string{"id": op.ID, "action": v.Action, "host_id": op.HostID})
	writeJSON(w, 202, op)
}
func (d Deps) handleInfraRetry(w http.ResponseWriter, r *http.Request, sess *auth.Session) {
	o, err := d.Infrastructure.Retry(r.Context(), r.PathValue("id"), sess.UserID)
	if err != nil {
		infraError(w, err)
		return
	}
	d.auditUser(r, sess, "infra.operation.retry", map[string]string{"id": o.ID})
	writeJSON(w, 202, o)
}
func (d Deps) handleInfraCredentials(w http.ResponseWriter, r *http.Request, _ *auth.Session) {
	writeJSON(w, 200, map[string]any{"items": d.Infrastructure.Credentials()})
}
func (d Deps) handleInfraCredential(w http.ResponseWriter, r *http.Request, sess *auth.Session) {
	var v struct {
		Kind  string `json:"kind"`
		Name  string `json:"name"`
		Value string `json:"value"`
	}
	if !infraDecode(w, r, &v) {
		return
	}
	m, err := d.Infrastructure.SaveCredential(r.Context(), v.Kind, v.Name, v.Value, sess.UserID, clientIP(r))
	if err != nil {
		infraError(w, err)
		return
	}
	writeJSON(w, 201, m)
}
func (d Deps) handleInfraReveal(w http.ResponseWriter, r *http.Request, sess *auth.Session) {
	value, err := d.Infrastructure.RevealCredential(r.Context(), r.PathValue("id"), sess.UserID)
	if err != nil {
		infraError(w, err)
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	writeJSON(w, 200, map[string]string{"value": value})
}
func (d Deps) handleInfraTerminalTicket(w http.ResponseWriter, r *http.Request, sess *auth.Session) {
	ticket, err := d.Infrastructure.TerminalTicket(r.Context(), r.PathValue("id"), sess.UserID)
	if err != nil {
		infraError(w, err)
		return
	}
	d.auditUser(r, sess, "infra.console.open", map[string]string{"host_id": r.PathValue("id")})
	w.Header().Set("Cache-Control", "no-store")
	writeJSON(w, 200, map[string]string{"ticket": ticket})
}

type terminalWriter struct {
	mu sync.Mutex
	ws *websocket.Conn
}

func (w *terminalWriter) Write(b []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	_ = w.ws.SetWriteDeadline(time.Now().Add(15 * time.Second))
	err := w.ws.WriteMessage(websocket.BinaryMessage, b)
	return len(b), err
}
func terminalOrigin(r *http.Request) bool {
	u, err := url.Parse(r.Header.Get("Origin"))
	return err == nil && (u.Scheme == "http" || u.Scheme == "https") && strings.EqualFold(u.Host, r.Host)
}

// handleInfraTerminal 只接受单次票据与同源 WebSocket，密钥始终留在后端。
func (d Deps) handleInfraTerminal(w http.ResponseWriter, r *http.Request) {
	if d.Infrastructure == nil {
		writeErr(w, 503, "基础设施服务未就绪")
		return
	}
	if !terminalOrigin(r) {
		writeErr(w, 403, "终端只接受同源连接")
		return
	}
	upgrader := websocket.Upgrader{CheckOrigin: terminalOrigin}
	ws, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		return
	}
	defer ws.Close()
	ws.SetReadLimit(65536)
	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Minute)
	defer cancel()
	client, _, err := d.Infrastructure.OpenTerminal(ctx, r.URL.Query().Get("ticket"))
	writer := &terminalWriter{ws: ws}
	if err != nil {
		_, _ = writer.Write([]byte("\r\n" + err.Error() + "\r\n"))
		return
	}
	defer client.Close()
	sess, err := client.NewSession()
	if err != nil {
		return
	}
	defer sess.Close()
	if err := sess.RequestPty("xterm-256color", 30, 120, ssh.TerminalModes{ssh.ECHO: 1, ssh.TTY_OP_ISPEED: 14400, ssh.TTY_OP_OSPEED: 14400}); err != nil {
		return
	}
	input, err := sess.StdinPipe()
	if err != nil {
		return
	}
	sess.Stdout = writer
	sess.Stderr = writer
	if err := sess.Shell(); err != nil {
		return
	}
	go func() { _ = sess.Wait(); _ = ws.Close() }()
	go func() { <-ctx.Done(); _ = client.Close(); _ = ws.Close() }()
	for {
		_ = ws.SetReadDeadline(time.Now().Add(2 * time.Minute))
		var message struct {
			Type string `json:"type"`
			Data string `json:"data"`
			Rows int    `json:"rows"`
			Cols int    `json:"cols"`
		}
		if err := ws.ReadJSON(&message); err != nil {
			return
		}
		switch message.Type {
		case "input":
			if _, err := io.WriteString(input, message.Data); err != nil {
				return
			}
		case "resize":
			if message.Rows >= 2 && message.Rows <= 500 && message.Cols >= 2 && message.Cols <= 500 {
				_ = sess.WindowChange(message.Rows, message.Cols)
			}
		}
	}
}
