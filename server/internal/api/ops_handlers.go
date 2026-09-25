package api

import (
	"bytes"
	"crypto/ecdsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/hex"
	"encoding/pem"
	"errors"
	"io"
	"net/http"
	"runtime"
	"strings"
	"time"

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
	jobID, name, typ, body, cleanup, err := parseArtifactMultipart(r)
	if cleanup != nil {
		defer cleanup()
	}
	if err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	a, dedup, err := d.Artifacts.PutSecure(r.Context(), artifact.PutInput{
		JobID: jobID, Name: name, Type: typ, Body: body,
		UploadedBy: "admin:" + sess.UserID,
	})
	if err != nil {
		writeArtifactPutErr(w, err)
		return
	}
	if d.Audit != nil {
		d.Audit.Log(r.Context(), "USER", sess.UserID, "artifact.upload", "success", clientIP(r), map[string]string{
			"artifact_id": a.ID, "job_id": jobID, "sha256": a.SHA256, "dedup": boolStr(dedup), "via": "admin",
		})
	}
	writeJSON(w, http.StatusCreated, map[string]any{"artifact": a, "deduplicated": dedup})
}

// handleWorkstationUploadArtifact 工作站出站上传。
// 鉴权：本 CA 签发且未吊销的客户端证书 + ECDSA 签名 + Job.WorkstationID 绑定。
func (d Deps) handleWorkstationUploadArtifact(w http.ResponseWriter, r *http.Request) {
	if d.Artifacts == nil || d.CA == nil {
		writeErr(w, http.StatusServiceUnavailable, "artifacts 未启用")
		return
	}
	wsID := strings.TrimSpace(r.Header.Get("X-AIE-Workstation-Id"))
	ts := strings.TrimSpace(r.Header.Get("X-AIE-Timestamp"))
	sig := strings.TrimSpace(r.Header.Get("X-AIE-Signature"))
	certPEM := strings.TrimSpace(r.Header.Get("X-AIE-Client-Cert"))
	// Header 中 PEM 换行常用字面量 \n 传输
	certPEM = strings.ReplaceAll(certPEM, `\n`, "\n")
	if wsID == "" || ts == "" || sig == "" || certPEM == "" {
		writeErr(w, http.StatusUnauthorized, "缺少工作站鉴权头")
		return
	}
	if err := artifact.CheckTimestampSkew(ts, time.Now().UTC(), 5*time.Minute); err != nil {
		writeErr(w, http.StatusUnauthorized, err.Error())
		return
	}
	block, _ := pem.Decode([]byte(certPEM))
	if block == nil || block.Type != "CERTIFICATE" {
		writeErr(w, http.StatusUnauthorized, "无效客户端证书")
		return
	}
	cn, _, err := d.CA.VerifyClientRaw(block.Bytes)
	if err != nil {
		writeErr(w, http.StatusUnauthorized, "证书校验失败")
		return
	}
	if cn != wsID {
		writeErr(w, http.StatusUnauthorized, "证书身份与 Workstation-Id 不一致")
		return
	}
	cert, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		writeErr(w, http.StatusUnauthorized, "证书解析失败")
		return
	}
	pub, ok := cert.PublicKey.(*ecdsa.PublicKey)
	if !ok {
		writeErr(w, http.StatusUnauthorized, "证书公钥类型不支持")
		return
	}

	jobID, name, typ, body, cleanup, err := parseArtifactMultipart(r)
	if cleanup != nil {
		defer cleanup()
	}
	if err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	data, err := io.ReadAll(io.LimitReader(body, artifact.DefaultMaxBytes+1))
	if err != nil {
		writeErr(w, http.StatusBadRequest, "读取文件失败")
		return
	}
	if int64(len(data)) > artifact.DefaultMaxBytes {
		writeErr(w, http.StatusRequestEntityTooLarge, artifact.ErrTooLarge.Error())
		return
	}
	safeName, err := artifact.SanitizeName(name)
	if err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	sum := sha256Hex(data)
	payload := artifact.CanonicalSignPayload(wsID, ts, jobID, safeName, sum, int64(len(data)))
	if err := artifact.VerifyECDSASignatureASN1(pub, payload, sig); err != nil {
		writeErr(w, http.StatusUnauthorized, err.Error())
		return
	}

	a, dedup, err := d.Artifacts.PutSecure(r.Context(), artifact.PutInput{
		JobID: jobID, Name: safeName, Type: typ, Body: bytes.NewReader(data),
		RequireWSBind: true, WorkstationID: wsID, UploadedBy: "workstation",
	})
	if err != nil {
		writeArtifactPutErr(w, err)
		return
	}
	if d.Audit != nil {
		d.Audit.Log(r.Context(), "WORKSTATION", wsID, "artifact.upload", "success", clientIP(r), map[string]string{
			"artifact_id": a.ID, "job_id": jobID, "sha256": a.SHA256, "dedup": boolStr(dedup), "via": "workstation",
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

func (d Deps) handlePutProvider(w http.ResponseWriter, r *http.Request, sess *auth.Session) {
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
	d.auditUser(r, sess, "provider.upsert", map[string]string{"id": p.ID, "name": p.Name})
	writeJSON(w, http.StatusOK, p)
}

func (d Deps) handleAddProviderVersion(w http.ResponseWriter, r *http.Request, sess *auth.Session) {
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
	d.auditUser(r, sess, "provider.version.add", map[string]string{
		"provider_id": out.ProviderID, "version": out.Version,
	})
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

func parseArtifactMultipart(r *http.Request) (jobID, name, typ string, body io.Reader, cleanup func(), err error) {
	jobID = r.FormValue("job_id")
	name = r.FormValue("name")
	typ = r.FormValue("type")
	if jobID == "" {
		jobID = r.URL.Query().Get("job_id")
	}
	if name == "" {
		name = r.URL.Query().Get("name")
	}
	if typ == "" {
		typ = r.URL.Query().Get("type")
	}
	body = r.Body
	if r.MultipartForm == nil {
		_ = r.ParseMultipartForm(artifact.DefaultMaxBytes + (1 << 20))
	}
	if f, hdr, ferr := r.FormFile("file"); ferr == nil {
		cleanup = func() { _ = f.Close() }
		if name == "" {
			name = hdr.Filename
		}
		body = f
	}
	if jobID == "" || name == "" {
		err = errors.New("需要 job_id 与 name（或 file）")
		return
	}
	return
}

func writeArtifactPutErr(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, artifact.ErrTooLarge):
		writeErr(w, http.StatusRequestEntityTooLarge, err.Error())
	case errors.Is(err, artifact.ErrJobForbidden):
		writeErr(w, http.StatusForbidden, err.Error())
	case errors.Is(err, artifact.ErrJobRequired),
		errors.Is(err, artifact.ErrInvalidName),
		errors.Is(err, artifact.ErrInvalidType),
		errors.Is(err, artifact.ErrEmptyBody):
		writeErr(w, http.StatusBadRequest, err.Error())
	default:
		writeErr(w, http.StatusBadRequest, err.Error())
	}
}

func sha256Hex(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

func boolStr(v bool) string {
	if v {
		return "true"
	}
	return "false"
}
