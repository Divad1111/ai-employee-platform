package api

import (
	"io"
	"net/http"
	"runtime"

	"github.com/ai-employee-platform/server/internal/artifact"
	"github.com/ai-employee-platform/server/internal/auth"
	"github.com/ai-employee-platform/server/internal/registry"
)

func (d Deps) handleListArtifacts(w http.ResponseWriter, r *http.Request, _ *auth.Session) {
	if d.Artifacts == nil {
		writeErr(w, http.StatusServiceUnavailable, "artifacts 未启用")
		return
	}
	jobID := r.URL.Query().Get("job_id")
	var list []*artifact.Artifact
	var err error
	if jobID != "" {
		list, err = d.Artifacts.ListByJob(r.Context(), jobID)
	} else {
		list, err = d.Artifacts.List(r.Context())
	}
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": list})
}

func (d Deps) handleUploadArtifact(w http.ResponseWriter, r *http.Request, sess *auth.Session) {
	if d.Artifacts == nil {
		writeErr(w, http.StatusServiceUnavailable, "artifacts 未启用")
		return
	}
	jobID := r.FormValue("job_id")
	name := r.FormValue("name")
	typ := r.FormValue("type")
	if jobID == "" {
		jobID = r.URL.Query().Get("job_id")
	}
	if name == "" {
		name = r.URL.Query().Get("name")
	}
	if typ == "" {
		typ = r.URL.Query().Get("type")
	}
	var body io.Reader = r.Body
	if r.MultipartForm == nil {
		_ = r.ParseMultipartForm(32 << 20)
	}
	if f, hdr, err := r.FormFile("file"); err == nil {
		defer f.Close()
		if name == "" {
			name = hdr.Filename
		}
		body = f
	}
	a, dedup, err := d.Artifacts.Put(r.Context(), jobID, name, typ, body)
	if err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	if d.Audit != nil {
		d.Audit.Log(r.Context(), "USER", sess.UserID, "artifact.upload", "success", clientIP(r), map[string]string{
			"artifact_id": a.ID, "job_id": jobID, "sha256": a.SHA256, "dedup": boolStr(dedup),
		})
	}
	writeJSON(w, http.StatusCreated, map[string]any{"artifact": a, "deduplicated": dedup})
}

func (d Deps) handleDownloadArtifact(w http.ResponseWriter, r *http.Request, _ *auth.Session) {
	rc, a, err := d.Artifacts.Open(r.Context(), r.PathValue("id"))
	if err != nil {
		writeErr(w, http.StatusNotFound, err.Error())
		return
	}
	defer rc.Close()
	w.Header().Set("Content-Type", "application/octet-stream")
	w.Header().Set("Content-Disposition", "attachment; filename="+a.Name)
	w.Header().Set("X-SHA256", a.SHA256)
	_, _ = io.Copy(w, rc)
}

func (d Deps) handleListProviders(w http.ResponseWriter, r *http.Request, _ *auth.Session) {
	list, err := d.Registry.ListProviders(r.Context())
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": list})
}

func (d Deps) handlePutProvider(w http.ResponseWriter, r *http.Request, _ *auth.Session) {
	var p registry.Provider
	if err := decodeJSON(r, &p); err != nil {
		writeErr(w, http.StatusBadRequest, "无效请求体")
		return
	}
	if id := r.PathValue("id"); id != "" {
		p.ID = id
	}
	if p.ID == "" {
		writeErr(w, http.StatusBadRequest, "需要 id")
		return
	}
	if err := d.Registry.UpsertProvider(r.Context(), &p); err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, p)
}

func (d Deps) handleAddProviderVersion(w http.ResponseWriter, r *http.Request, _ *auth.Session) {
	var v registry.Version
	if err := decodeJSON(r, &v); err != nil {
		writeErr(w, http.StatusBadRequest, "无效请求体")
		return
	}
	out, err := d.Registry.AddVersion(r.Context(), &v)
	if err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusCreated, out)
}

func (d Deps) handleQueryProviderVersions(w http.ResponseWriter, r *http.Request, _ *auth.Session) {
	list, err := d.Registry.QueryVersions(r.Context(),
		r.URL.Query().Get("provider_id"),
		r.URL.Query().Get("os"),
		r.URL.Query().Get("arch"),
	)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": list})
}

func (d Deps) handleSigningKey(w http.ResponseWriter, _ *http.Request) {
	// Workstation 可匿名拉取公钥（Q-06 B）；生产可再加 mTLS
	if d.Registry == nil {
		writeErr(w, http.StatusServiceUnavailable, "registry 未启用")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"alg": "ed25519", "public_key_b64": d.Registry.SigningPublicKey(),
		"goos": runtime.GOOS, "goarch": runtime.GOARCH,
	})
}

func (d Deps) handleSchedulerStatus(w http.ResponseWriter, _ *http.Request, _ *auth.Session) {
	reasons := map[string]string{}
	if d.Scheduler != nil {
		reasons = d.Scheduler.RejectReasons()
	}
	writeJSON(w, http.StatusOK, map[string]any{"reject_reasons": reasons})
}

func boolStr(v bool) string {
	if v {
		return "true"
	}
	return "false"
}
