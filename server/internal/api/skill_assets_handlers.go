package api

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime"
	"net/http"
	"strconv"

	"github.com/ai-employee-platform/server/internal/auth"
	"github.com/ai-employee-platform/server/internal/workflowmcp"
)

func writeAssetError(w http.ResponseWriter, err error) {
	code := http.StatusInternalServerError
	switch {
	case errors.Is(err, workflowmcp.ErrConflict):
		code = http.StatusConflict
	case errors.Is(err, workflowmcp.ErrNotFound):
		code = http.StatusNotFound
	case errors.Is(err, workflowmcp.ErrInvalidInput):
		code = http.StatusBadRequest
	}
	writeErr(w, code, err.Error())
}

func (d Deps) handleListSkillAssets(w http.ResponseWriter, r *http.Request, _ *auth.Session) {
	sp, files, err := d.WorkflowMCP.ListAssets(r.Context(), r.PathValue("id"))
	if err != nil {
		writeAssetError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"skill": sp, "files": files})
}

func (d Deps) handleReadSkillAsset(w http.ResponseWriter, r *http.Request, _ *auth.Session) {
	f, revision, err := d.WorkflowMCP.ReadAsset(r.Context(), r.PathValue("id"), r.URL.Query().Get("path"))
	if err != nil {
		writeAssetError(w, err)
		return
	}
	w.Header().Set("Content-Type", http.DetectContentType(f.Content))
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("X-Skill-Revision", strconv.FormatInt(revision, 10))
	w.Header().Set("ETag", `"`+f.SHA256+`"`)
	w.Header().Set("Content-Disposition", mime.FormatMediaType("attachment", map[string]string{"filename": f.Path}))
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(f.Content)
}

func (d Deps) handlePatchSkillAssets(w http.ResponseWriter, r *http.Request, sess *auth.Session) {
	r.Body = http.MaxBytesReader(w, r.Body, 66<<20)
	if err := r.ParseMultipartForm(8 << 20); err != nil {
		writeErr(w, http.StatusRequestEntityTooLarge, "无效上传或请求超过 66 MiB")
		return
	}
	defer r.MultipartForm.RemoveAll()
	var manifest struct {
		Revision int64 `json:"revision"`
		Writes   []struct {
			Path string `json:"path"`
			Part string `json:"part"`
		} `json:"writes"`
		Deletes []string `json:"deletes"`
	}
	values := r.MultipartForm.Value["manifest"]
	if len(values) != 1 || len(values[0]) > 512<<10 || json.Unmarshal([]byte(values[0]), &manifest) != nil {
		writeErr(w, http.StatusBadRequest, "无效的变更清单")
		return
	}
	if len(manifest.Writes)+len(manifest.Deletes) > 1024 {
		writeErr(w, http.StatusBadRequest, "单次最多保存 1024 个文件操作")
		return
	}
	if len(manifest.Deletes) > 0 && !d.scopeAll(r, sess, "workflow.delete") {
		writeErr(w, http.StatusForbidden, "删除资产需要全局删除权限")
		return
	}
	patch := workflowmcp.AssetPatch{Revision: manifest.Revision, Deletes: manifest.Deletes}
	parts := map[string]bool{}
	var total int64
	for _, op := range manifest.Writes {
		if op.Part == "" || parts[op.Part] || len(r.MultipartForm.File[op.Part]) != 1 {
			writeErr(w, http.StatusBadRequest, "上传文件与变更清单不匹配")
			return
		}
		parts[op.Part] = true
		fh := r.MultipartForm.File[op.Part][0]
		if fh.Size > workflowmcp.MaxAssetBytes {
			writeErr(w, http.StatusRequestEntityTooLarge, "单文件不能超过 16 MiB")
			return
		}
		f, err := fh.Open()
		if err != nil {
			writeAssetError(w, err)
			return
		}
		content, err := io.ReadAll(io.LimitReader(f, workflowmcp.MaxAssetBytes+1))
		f.Close()
		if err != nil {
			writeAssetError(w, err)
			return
		}
		total += int64(len(content))
		if len(content) > workflowmcp.MaxAssetBytes || total > 64<<20 {
			writeErr(w, http.StatusRequestEntityTooLarge, "文件超过上传大小限制")
			return
		}
		patch.Writes = append(patch.Writes, workflowmcp.SkillFile{Path: op.Path, Content: content})
	}
	if len(parts) != len(r.MultipartForm.File) {
		writeErr(w, http.StatusBadRequest, "存在未声明的上传文件")
		return
	}
	sp, changed, err := d.WorkflowMCP.PatchAssets(r.Context(), r.PathValue("id"), patch)
	if err != nil {
		writeAssetError(w, err)
		return
	}
	if changed {
		d.auditUser(r, sess, "skill.assets.patch", map[string]string{"id": sp.ID, "writes": fmt.Sprint(len(patch.Writes)), "deletes": fmt.Sprint(len(patch.Deletes))})
	}
	// The response must describe this commit, never a subsequent writer's revision.
	savedFiles := make([]workflowmcp.SkillFile, 0, len(patch.Writes))
	for _, f := range patch.Writes {
		sum := sha256.Sum256(f.Content)
		savedFiles = append(savedFiles, workflowmcp.SkillFile{Path: f.Path, SHA256: hex.EncodeToString(sum[:]), Size: int64(len(f.Content))})
	}
	writeJSON(w, http.StatusOK, map[string]any{"status": "ok", "changed": changed, "revision": sp.Revision, "content_hash": sp.ContentHash, "skill": sp, "files": savedFiles})
}
