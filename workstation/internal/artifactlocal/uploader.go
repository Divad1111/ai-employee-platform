package artifactlocal

import (
	"bytes"
	"crypto/ecdsa"
	"crypto/rand"
	"crypto/sha256"
	"crypto/x509"
	"encoding/asn1"
	"encoding/hex"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"io"
	"math/big"
	"mime/multipart"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/ai-employee-platform/workstation/internal/identity"
)

// Uploader 将本地队列推送到 Control Plane（证书签名，非 Admin JWT）。
type Uploader struct {
	HTTPBase string // 如 http://127.0.0.1:8080
	Bundle   *identity.Bundle
	Queue    *Queue
	Client   *http.Client
}

// Flush 上传所有未确认项；失败保留本地。
func (u *Uploader) Flush() (uploaded int, err error) {
	if u == nil || u.Queue == nil || u.Bundle == nil || u.HTTPBase == "" {
		return 0, fmt.Errorf("uploader 未配置")
	}
	cli := u.Client
	if cli == nil {
		cli = &http.Client{Timeout: 60 * time.Second}
	}
	key, err := parseECKey(u.Bundle.KeyPEM)
	if err != nil {
		return 0, err
	}
	pending := u.Queue.PendingUploads()
	var last error
	for _, p := range pending {
		if e := u.uploadOne(cli, key, p); e != nil {
			last = e
			continue
		}
		u.Queue.MarkUploaded(p.SHA256)
		uploaded++
	}
	return uploaded, last
}

func (u *Uploader) uploadOne(cli *http.Client, key *ecdsa.PrivateKey, p Pending) error {
	data, err := os.ReadFile(p.LocalPath)
	if err != nil {
		return err
	}
	ts := time.Now().UTC().Format(time.RFC3339)
	payload := canonical(u.Bundle.WorkstationID, ts, p.JobID, p.Name, p.SHA256, int64(len(data)))
	sigHex, err := signASN1(key, payload)
	if err != nil {
		return err
	}

	var buf bytes.Buffer
	w := multipart.NewWriter(&buf)
	_ = w.WriteField("job_id", p.JobID)
	_ = w.WriteField("name", p.Name)
	_ = w.WriteField("type", p.Type)
	fw, err := w.CreateFormFile("file", p.Name)
	if err != nil {
		return err
	}
	if _, err := fw.Write(data); err != nil {
		return err
	}
	_ = w.Close()

	url := strings.TrimRight(u.HTTPBase, "/") + "/api/integrations/workstation/artifacts"
	req, err := http.NewRequest(http.MethodPost, url, &buf)
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", w.FormDataContentType())
	req.Header.Set("X-AIE-Workstation-Id", u.Bundle.WorkstationID)
	req.Header.Set("X-AIE-Timestamp", ts)
	req.Header.Set("X-AIE-Signature", sigHex)
	// 单行 PEM，换行转义为 \n
	req.Header.Set("X-AIE-Client-Cert", strings.ReplaceAll(string(u.Bundle.CertPEM), "\n", `\n`))

	res, err := cli.Do(req)
	if err != nil {
		return err
	}
	defer res.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(res.Body, 1<<20))
	if res.StatusCode < 200 || res.StatusCode >= 300 {
		return fmt.Errorf("上传失败 HTTP %d: %s", res.StatusCode, string(body))
	}
	var out struct {
		Artifact struct {
			ID string `json:"id"`
		} `json:"artifact"`
	}
	_ = json.Unmarshal(body, &out)
	return nil
}

func canonical(wsID, ts, jobID, name, sha string, size int64) string {
	return strings.Join([]string{
		"aie-artifact-v1", wsID, ts, jobID, name, sha, strconv.FormatInt(size, 10),
	}, "\n")
}

func signASN1(key *ecdsa.PrivateKey, payload string) (string, error) {
	sum := sha256.Sum256([]byte(payload))
	r, s, err := ecdsa.Sign(rand.Reader, key, sum[:])
	if err != nil {
		return "", err
	}
	der, err := asn1.Marshal(struct{ R, S *big.Int }{r, s})
	if err != nil {
		return "", err
	}
	return hex.EncodeToString(der), nil
}

func parseECKey(pemBytes []byte) (*ecdsa.PrivateKey, error) {
	block, _ := pem.Decode(pemBytes)
	if block == nil {
		return nil, fmt.Errorf("无效私钥 PEM")
	}
	if block.Type == "EC PRIVATE KEY" {
		return x509.ParseECPrivateKey(block.Bytes)
	}
	k, err := x509.ParsePKCS8PrivateKey(block.Bytes)
	if err != nil {
		return nil, err
	}
	ek, ok := k.(*ecdsa.PrivateKey)
	if !ok {
		return nil, fmt.Errorf("非 ECDSA 私钥")
	}
	return ek, nil
}

// StageAndTryUpload 先 Stage，再尽力上传一次。
func (u *Uploader) StageAndTryUpload(jobID, name, typ string, data []byte) (Pending, error) {
	p, err := u.Queue.Stage(jobID, name, typ, data)
	if err != nil {
		return p, err
	}
	if u.HTTPBase == "" || u.Bundle == nil || len(u.Bundle.CertPEM) == 0 {
		return p, nil // 仅本地保留
	}
	cli := u.Client
	if cli == nil {
		cli = &http.Client{Timeout: 60 * time.Second}
	}
	key, err := parseECKey(u.Bundle.KeyPEM)
	if err != nil {
		return p, nil
	}
	if err := u.uploadOne(cli, key, p); err == nil {
		u.Queue.MarkUploaded(p.SHA256)
	}
	return p, nil
}
