package artifactlocal

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	input "github.com/ai-employee-platform/gen/jobinput"
	"github.com/ai-employee-platform/workstation/internal/acp"
)

// DownloadInputs uses the node's existing certificate identity, never a public
// download URL. Every body is size/hash checked before it reaches an Agent.
func (u *Uploader) DownloadInputs(ctx context.Context, jobID, workspace string, files []input.File) ([]acp.InputFile, error) {
	if len(files) == 0 {
		return nil, nil
	}
	if err := input.Validate(files); err != nil {
		return nil, err
	}
	if u == nil || u.Bundle == nil || u.HTTPBase == "" || workspace == "" {
		return nil, fmt.Errorf("附件下载身份或工作区未配置")
	}
	key, err := parseECKey(u.Bundle.KeyPEM)
	if err != nil {
		return nil, err
	}
	root, err := filepath.Abs(workspace)
	if err != nil {
		return nil, err
	}
	jobHash := sha256.Sum256([]byte(jobID))
	dir := root
	for _, part := range []string{".aie-inputs", hex.EncodeToString(jobHash[:8])} {
		dir = filepath.Join(dir, part)
		if err := os.Mkdir(dir, 0o700); err != nil && !os.IsExist(err) {
			return nil, err
		}
		info, err := os.Lstat(dir)
		if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
			return nil, fmt.Errorf("附件目录不是普通工作区目录")
		}
	}
	cli := u.Client
	if cli == nil {
		cli = &http.Client{Timeout: 90 * time.Second}
	}
	// Signed identity headers must never follow redirects to another host.
	clientCopy := *cli
	clientCopy.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	var result []acp.InputFile
	for i, file := range files {
		ts := time.Now().UTC().Format(time.RFC3339)
		sig, err := signASN1(key, input.DownloadPayload(u.Bundle.WorkstationID, ts, jobID, file.SHA256))
		if err != nil {
			return nil, err
		}
		endpoint := strings.TrimRight(u.HTTPBase, "/") + "/api/integrations/workstation/jobs/" + url.PathEscape(jobID) + "/inputs/" + file.SHA256
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
		if err != nil {
			return nil, err
		}
		req.Header.Set("X-AIE-Workstation-Id", u.Bundle.WorkstationID)
		req.Header.Set("X-AIE-Timestamp", ts)
		req.Header.Set("X-AIE-Signature", sig)
		req.Header.Set("X-AIE-Client-Cert", strings.ReplaceAll(string(u.Bundle.CertPEM), "\n", `\n`))
		resp, err := clientCopy.Do(req)
		if err != nil {
			return nil, fmt.Errorf("下载任务附件失败: %w", err)
		}
		data, readErr := io.ReadAll(io.LimitReader(resp.Body, input.MaxBytes+1))
		resp.Body.Close()
		if readErr != nil || resp.StatusCode != http.StatusOK {
			return nil, fmt.Errorf("下载任务附件失败 HTTP %d", resp.StatusCode)
		}
		sum := sha256.Sum256(data)
		if int64(len(data)) != file.Size || hex.EncodeToString(sum[:]) != file.SHA256 {
			return nil, fmt.Errorf("附件大小或 SHA256 校验失败")
		}
		path := filepath.Join(dir, fmt.Sprintf("%02d-%s-%s", i+1, file.SHA256[:12], file.Name))
		temp, err := os.CreateTemp(dir, ".download-*")
		if err != nil {
			return nil, err
		}
		_, err = temp.Write(data)
		closeErr := temp.Close()
		if err == nil {
			err = closeErr
		}
		if err == nil {
			err = os.Rename(temp.Name(), path)
		}
		os.Remove(temp.Name())
		if err != nil {
			return nil, err
		}
		result = append(result, acp.InputFile{Path: path, Name: file.Name, MIMEType: file.MIMEType})
	}
	return result, nil
}
