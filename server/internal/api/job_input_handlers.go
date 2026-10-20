package api

import (
	"crypto/ecdsa"
	"crypto/x509"
	"encoding/pem"
	"io"
	"net/http"
	"strings"
	"time"

	input "github.com/ai-employee-platform/gen/jobinput"
	"github.com/ai-employee-platform/server/internal/artifact"
)

// A node may download only files in a Job assigned to that node. Hash knowledge
// alone grants no access; proof of possession binds the request to Job + hash.
func (d Deps) handleWorkstationJobInput(w http.ResponseWriter, r *http.Request) {
	if d.InputFiles == nil || d.CA == nil || d.Jobs == nil {
		writeErr(w, 503, "附件服务未配置")
		return
	}
	wsID, ts := r.Header.Get("X-AIE-Workstation-Id"), r.Header.Get("X-AIE-Timestamp")
	if wsID == "" || artifact.CheckTimestampSkew(ts, time.Now(), 5*time.Minute) != nil {
		writeErr(w, 401, "工作站鉴权失败")
		return
	}
	block, _ := pem.Decode([]byte(strings.ReplaceAll(r.Header.Get("X-AIE-Client-Cert"), `\n`, "\n")))
	if block == nil {
		writeErr(w, 401, "工作站鉴权失败")
		return
	}
	cn, _, err := d.CA.VerifyClientRaw(block.Bytes)
	if err != nil || cn != wsID {
		writeErr(w, 401, "工作站鉴权失败")
		return
	}
	cert, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		writeErr(w, 401, "工作站鉴权失败")
		return
	}
	pub, ok := cert.PublicKey.(*ecdsa.PublicKey)
	jobID, hash := r.PathValue("id"), r.PathValue("hash")
	if !ok || !input.ValidHash(hash) || artifact.VerifyECDSASignatureASN1(pub, input.DownloadPayload(wsID, ts, jobID, hash), r.Header.Get("X-AIE-Signature")) != nil {
		writeErr(w, 401, "工作站签名无效")
		return
	}
	j, err := d.Jobs.Get(r.Context(), jobID)
	if err != nil || j.WorkstationID != wsID {
		writeErr(w, 404, "附件不存在")
		return
	}
	var file *input.File
	for i := range j.InputAttachments {
		if j.InputAttachments[i].SHA256 == hash {
			file = &j.InputAttachments[i]
			break
		}
	}
	if file == nil {
		writeErr(w, 404, "附件不存在")
		return
	}
	reader, err := d.InputFiles.Open(hash)
	if err != nil {
		writeErr(w, 404, "附件内容不可用")
		return
	}
	defer reader.Close()
	w.Header().Set("Content-Type", "application/octet-stream")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("X-SHA256", hash)
	_, _ = io.Copy(w, reader)
}
