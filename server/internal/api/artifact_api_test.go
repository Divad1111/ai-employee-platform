package api_test

import (
	"bytes"
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/asn1"
	"encoding/hex"
	"encoding/json"
	"encoding/pem"
	"io"
	"math/big"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/ai-employee-platform/server/internal/api"
	"github.com/ai-employee-platform/server/internal/artifact"
	"github.com/ai-employee-platform/server/internal/audit"
	"github.com/ai-employee-platform/server/internal/auth"
	"github.com/ai-employee-platform/server/internal/certca"
	"github.com/ai-employee-platform/server/internal/employee"
	"github.com/ai-employee-platform/server/internal/enrollment"
	"github.com/ai-employee-platform/server/internal/eventbus"
	"github.com/ai-employee-platform/server/internal/job"
	"github.com/ai-employee-platform/server/internal/message"
	"github.com/ai-employee-platform/server/internal/metrics"
	"github.com/ai-employee-platform/server/internal/permission"
	"github.com/ai-employee-platform/server/internal/registry"
	"github.com/ai-employee-platform/server/internal/reliability"
	"github.com/ai-employee-platform/server/internal/secret"
	"github.com/ai-employee-platform/server/internal/session"
	"github.com/ai-employee-platform/server/internal/workstation"
	"github.com/ai-employee-platform/server/internal/workspace"
)

type artJobLookup struct{ svc *job.Service }

func (j artJobLookup) LookupJob(ctx context.Context, id string) (*artifact.JobInfo, error) {
	jb, err := j.svc.Get(ctx, id)
	if err != nil {
		return nil, artifact.ErrJobRequired
	}
	return &artifact.JobInfo{ID: jb.ID, WorkstationID: jb.WorkstationID, Status: jb.Status}, nil
}

func setupArtifactAPI(t *testing.T) (http.Handler, string, *job.Service, *certca.Authority) {
	t.Helper()
	auditor := audit.NewMemory()
	bus := eventbus.New(50)
	users := auth.NewMemoryUserStore()
	_ = users.SeedAdmin("admin", "admin123", "Admin")
	authSvc := auth.NewService(users, auth.NewMemorySessionStore(), auditor)
	ca, err := certca.NewDevAuthority()
	if err != nil {
		t.Fatal(err)
	}
	jobSvc := job.NewService(job.NewMemoryStore(), auditor, bus)
	art := artifact.New(artifact.NewMemoryStore(), t.TempDir())
	art.SetJobLookup(artJobLookup{svc: jobSvc})
	permStore := permission.NewMemoryStore()
	_ = permission.EnsureDefault(permStore)
	reg, _, err := registry.New(registry.NewMemoryStore(), "")
	if err != nil {
		t.Fatal(err)
	}
	vault, err := secret.NewMemoryVault()
	if err != nil {
		t.Fatal(err)
	}
	h := api.NewRouter(api.Deps{
		Auth: authSvc, Enrollment: enrollment.NewService(enrollment.NewMemoryStore(), ca, auditor), CA: ca,
		Employees:    employee.NewService(employee.NewMemoryStore(), auditor, bus),
		Workspaces:   workspace.NewService(workspace.NewMemoryStore(), auditor),
		Workstations: workstation.NewService(ca, reliability.NewPresence(5, 15), nil),
		Sessions:     session.NewService(session.NewMemoryStore(), auditor, bus),
		Jobs:         jobSvc, Messages: message.NewService(message.NewMemoryStore(), auditor),
		Bus: bus, Audit: auditor, Secrets: vault,
		Permission: permission.NewEngine(permStore, auditor), PermissionStore: permStore,
		Artifacts: art, Registry: reg, Metrics: metrics.New(),
	})
	return h, login(t, h), jobSvc, ca
}

func mustJob(t *testing.T, jobs *job.Service) *job.Job {
	t.Helper()
	j, _, err := jobs.Create(context.Background(), job.CreateInput{
		EmployeeID: "E1", Prompt: "p", IdempotencyKey: "idem-" + strconv.FormatInt(time.Now().UnixNano(), 10),
	}, "u", "")
	if err != nil {
		t.Fatal(err)
	}
	return j
}

func TestAdminUpload_RequiresExistingJob(t *testing.T) {
	h, tok, _, _ := setupArtifactAPI(t)
	code, body := postArtifactAdmin(t, h, tok, "JOB-nope", "a.json", "json", `{}`)
	if code != http.StatusBadRequest {
		t.Fatalf("期望拒绝不存在 Job: %d %s", code, body)
	}
}

func TestAdminUpload_RejectsPathTraversalAndBadType(t *testing.T) {
	h, tok, jobs, _ := setupArtifactAPI(t)
	j := mustJob(t, jobs)
	code, _ := postArtifactAdmin(t, h, tok, j.ID, "../evil.txt", "txt", "x")
	if code != http.StatusBadRequest {
		t.Fatalf("坏文件名应 400, got %d", code)
	}
	code, _ = postArtifactAdmin(t, h, tok, j.ID, "ok.bin", "exe", "x")
	if code != http.StatusBadRequest {
		t.Fatalf("非法 type 应 400, got %d", code)
	}
}

func TestAdminUpload_SuccessDownloadDedup(t *testing.T) {
	h, tok, jobs, _ := setupArtifactAPI(t)
	j := mustJob(t, jobs)
	code, body := postArtifactAdmin(t, h, tok, j.ID, "result.json", "json", `{"ok":true}`)
	if code != http.StatusCreated && code != http.StatusOK {
		t.Fatalf("upload failed code=%d body=%q", code, body)
	}
	var out map[string]any
	if err := json.Unmarshal([]byte(body), &out); err != nil {
		t.Fatalf("bad json code=%d body=%q err=%v", code, body, err)
	}
	art, _ := out["artifact"].(map[string]any)
	if art == nil {
		t.Fatalf("missing artifact code=%d body=%q", code, body)
	}
	id, _ := art["id"].(string)
	if id == "" {
		t.Fatal(art)
	}

	req := httptest.NewRequest(http.MethodGet, "/api/artifacts/"+id+"/download", nil)
	req.Header.Set("Authorization", "Bearer "+tok)
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)
	if rr.Code != 200 || rr.Body.String() != `{"ok":true}` {
		t.Fatal(rr.Code, rr.Body.String())
	}

	j2 := mustJob(t, jobs)
	code, body = postArtifactAdmin(t, h, tok, j2.ID, "copy.json", "json", `{"ok":true}`)
	if code != http.StatusCreated && code != http.StatusOK {
		t.Fatal(code, body)
	}
	_ = json.Unmarshal([]byte(body), &out)
	if out["deduplicated"] != true {
		t.Fatal(out)
	}
}

func TestWorkstationUpload_RequiresCertSigAndJobBind(t *testing.T) {
	h, _, jobs, ca := setupArtifactAPI(t)
	wsID := "WS-art-bind-1"
	key, certPEM := issueWSCert(t, ca, wsID)
	j := mustJob(t, jobs)

	code, body := postWSArtifact(t, h, wsID, key, certPEM, j.ID, "out.txt", "txt", "hello")
	if code != http.StatusForbidden {
		t.Fatalf("未绑定工作站应 403: %d %s", code, body)
	}

	if err := jobs.BindWorkstation(context.Background(), j.ID, wsID); err != nil {
		t.Fatal(err)
	}
	_, _ = jobs.Transition(context.Background(), j.ID, job.StatusQueued, "t", "", nil)
	_, _ = jobs.Transition(context.Background(), j.ID, job.StatusAssigned, "t", wsID, nil)
	_, _ = jobs.Transition(context.Background(), j.ID, job.StatusStarting, "t", wsID, nil)
	_, _ = jobs.Transition(context.Background(), j.ID, job.StatusRunning, "t", wsID, nil)

	code, body = postWSArtifact(t, h, wsID, key, certPEM, j.ID, "out.txt", "txt", "hello")
	if code != http.StatusCreated {
		t.Fatalf("合法上传应 201: %d %s", code, body)
	}

	code, _ = postWSArtifactUnsigned(t, h, wsID, certPEM, j.ID, "out2.txt", "txt", "x")
	if code != http.StatusUnauthorized {
		t.Fatalf("无签名应 401, got %d", code)
	}

	code, _ = postWSArtifact(t, h, "WS-forged", key, certPEM, j.ID, "out3.txt", "txt", "x")
	if code != http.StatusUnauthorized {
		t.Fatalf("证书 CN 不一致应 401, got %d", code)
	}
}

func TestAnonymousUploadRejected(t *testing.T) {
	h, _, jobs, _ := setupArtifactAPI(t)
	j := mustJob(t, jobs)
	var buf bytes.Buffer
	w := multipart.NewWriter(&buf)
	_ = w.WriteField("job_id", j.ID)
	_ = w.WriteField("name", "x.txt")
	fw, _ := w.CreateFormFile("file", "x.txt")
	_, _ = fw.Write([]byte("x"))
	_ = w.Close()
	req := httptest.NewRequest(http.MethodPost, "/api/artifacts", &buf)
	req.Header.Set("Content-Type", w.FormDataContentType())
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)
	if rr.Code == http.StatusCreated {
		t.Fatal("匿名 Admin 上传不应成功")
	}
	req2 := httptest.NewRequest(http.MethodPost, "/api/integrations/workstation/artifacts", &buf)
	req2.Header.Set("Content-Type", w.FormDataContentType())
	rr2 := httptest.NewRecorder()
	h.ServeHTTP(rr2, req2)
	if rr2.Code != http.StatusUnauthorized {
		t.Fatalf("匿名工作站上传应 401, got %d", rr2.Code)
	}
}

func issueWSCert(t *testing.T, ca *certca.Authority, wsID string) (*ecdsa.PrivateKey, string) {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	csrDER, err := x509.CreateCertificateRequest(rand.Reader, &x509.CertificateRequest{
		Subject: pkix.Name{CommonName: wsID},
	}, key)
	if err != nil {
		t.Fatal(err)
	}
	csrPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE REQUEST", Bytes: csrDER})
	rec, err := ca.SignCSR(wsID, csrPEM, 30)
	if err != nil {
		t.Fatal(err)
	}
	return key, rec.CertPEM
}

func postArtifactAdmin(t *testing.T, h http.Handler, tok, jobID, name, typ, content string) (int, string) {
	t.Helper()
	var buf bytes.Buffer
	w := multipart.NewWriter(&buf)
	_ = w.WriteField("job_id", jobID)
	_ = w.WriteField("name", name)
	_ = w.WriteField("type", typ)
	fw, _ := w.CreateFormFile("file", name)
	_, _ = fw.Write([]byte(content))
	_ = w.Close()
	req := httptest.NewRequest(http.MethodPost, "/api/artifacts", &buf)
	req.Header.Set("Authorization", "Bearer "+tok)
	req.Header.Set("Content-Type", w.FormDataContentType())
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)
	return rr.Code, rr.Body.String()
}

func postWSArtifact(t *testing.T, h http.Handler, wsID string, key *ecdsa.PrivateKey, certPEM, jobID, name, typ, content string) (int, string) {
	t.Helper()
	data := []byte(content)
	sum := sha256.Sum256(data)
	sha := hex.EncodeToString(sum[:])
	ts := time.Now().UTC().Format(time.RFC3339)
	payload := strings.Join([]string{"aie-artifact-v1", wsID, ts, jobID, name, sha, strconv.FormatInt(int64(len(data)), 10)}, "\n")
	sig := signEC(t, key, payload)

	var buf bytes.Buffer
	w := multipart.NewWriter(&buf)
	_ = w.WriteField("job_id", jobID)
	_ = w.WriteField("name", name)
	_ = w.WriteField("type", typ)
	fw, _ := w.CreateFormFile("file", name)
	_, _ = fw.Write(data)
	_ = w.Close()
	req := httptest.NewRequest(http.MethodPost, "/api/integrations/workstation/artifacts", &buf)
	req.Header.Set("Content-Type", w.FormDataContentType())
	req.Header.Set("X-AIE-Workstation-Id", wsID)
	req.Header.Set("X-AIE-Timestamp", ts)
	req.Header.Set("X-AIE-Signature", sig)
	req.Header.Set("X-AIE-Client-Cert", strings.ReplaceAll(certPEM, "\n", `\n`))
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)
	return rr.Code, rr.Body.String()
}

func postWSArtifactUnsigned(t *testing.T, h http.Handler, wsID, certPEM, jobID, name, typ, content string) (int, string) {
	t.Helper()
	var buf bytes.Buffer
	w := multipart.NewWriter(&buf)
	_ = w.WriteField("job_id", jobID)
	_ = w.WriteField("name", name)
	_ = w.WriteField("type", typ)
	fw, _ := w.CreateFormFile("file", name)
	_, _ = io.WriteString(fw, content)
	_ = w.Close()
	req := httptest.NewRequest(http.MethodPost, "/api/integrations/workstation/artifacts", &buf)
	req.Header.Set("Content-Type", w.FormDataContentType())
	req.Header.Set("X-AIE-Workstation-Id", wsID)
	req.Header.Set("X-AIE-Timestamp", time.Now().UTC().Format(time.RFC3339))
	req.Header.Set("X-AIE-Client-Cert", strings.ReplaceAll(certPEM, "\n", `\n`))
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)
	return rr.Code, rr.Body.String()
}

func signEC(t *testing.T, key *ecdsa.PrivateKey, payload string) string {
	t.Helper()
	sum := sha256.Sum256([]byte(payload))
	r, s, err := ecdsa.Sign(rand.Reader, key, sum[:])
	if err != nil {
		t.Fatal(err)
	}
	der, err := asn1.Marshal(struct{ R, S *big.Int }{r, s})
	if err != nil {
		t.Fatal(err)
	}
	return hex.EncodeToString(der)
}
