package api_test

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/json"
	"io"
	"mime/multipart"
	"net"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/ai-employee-platform/server/internal/api"
	"github.com/ai-employee-platform/server/internal/audit"
	"github.com/ai-employee-platform/server/internal/auth"
	"github.com/ai-employee-platform/server/internal/infrastructure"
	"github.com/ai-employee-platform/server/internal/secret"
	"github.com/gorilla/websocket"
	"golang.org/x/crypto/ssh"
)

type infraFixture struct {
	handler http.Handler
	token   string
	auth    *auth.Service
	users   *auth.MemoryUserStore
	service *infrastructure.Service
	vault   secret.Store
}

func setupInfrastructureAPI(t *testing.T) infraFixture {
	t.Helper()
	users := auth.NewMemoryUserStore()
	if err := users.SeedAdmin("admin", "admin123", "Admin"); err != nil {
		t.Fatal(err)
	}
	aud := audit.NewMemory()
	authSvc := auth.NewService(users, auth.NewMemorySessionStore(), aud)
	vault, err := secret.NewMemoryVault()
	if err != nil {
		t.Fatal(err)
	}
	mgr := secret.NewManager(vault, secret.NewMemoryBindings(), aud)
	svc := infrastructure.New(infrastructure.NewMemoryStore(), mgr, vault, t.TempDir())
	if err := svc.Seed(context.Background()); err != nil {
		t.Fatal(err)
	}
	h := api.NewRouter(api.Deps{Auth: authSvc, Audit: aud, Secrets: vault, SecretMgr: mgr, Infrastructure: svc})
	return infraFixture{handler: h, token: login(t, h), auth: authSvc, users: users, service: svc, vault: vault}
}
func (f infraFixture) request(method, path string, body any) *httptest.ResponseRecorder {
	var reader io.Reader
	if body != nil {
		b, _ := json.Marshal(body)
		reader = bytes.NewReader(b)
	}
	req := httptest.NewRequest(method, path, reader)
	req.Header.Set("Authorization", "Bearer "+f.token)
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	f.handler.ServeHTTP(rr, req)
	return rr
}
func (f infraFixture) stepUp(t *testing.T) {
	t.Helper()
	if _, err := f.auth.StepUp(context.Background(), f.token, "admin123", "", time.Minute); err != nil {
		t.Fatal(err)
	}
}

func TestInfrastructureRequiresAllScopeAndAnonymousDenied(t *testing.T) {
	f := setupInfrastructureAPI(t)
	rr := httptest.NewRecorder()
	f.handler.ServeHTTP(rr, httptest.NewRequest("GET", "/api/infrastructure", nil))
	if rr.Code != 401 {
		t.Fatal(rr.Code)
	}
	rr = f.request("GET", "/api/infrastructure", nil)
	if rr.Code != 200 {
		t.Fatal(rr.Code, rr.Body.String())
	}
	if err := f.users.SetRolePermissionScope(context.Background(), "ADMIN", "infra.read", "OWN"); err != nil {
		t.Fatal(err)
	}
	rr = f.request("GET", "/api/infrastructure", nil)
	if rr.Code != 403 {
		t.Fatal(rr.Code, rr.Body.String())
	}
}

func TestInfraCredentialRevealRequiresStepUpAndCannotReadSystemSecrets(t *testing.T) {
	f := setupInfrastructureAPI(t)
	body := map[string]string{"kind": "ssh", "name": "vm", "value": `{"password":"secret-password"}`}
	rr := f.request("POST", "/api/infrastructure/credentials", body)
	if rr.Code != 403 {
		t.Fatal(rr.Code, rr.Body.String())
	}
	f.stepUp(t)
	rr = f.request("POST", "/api/infrastructure/credentials", body)
	if rr.Code != 201 || strings.Contains(rr.Body.String(), "secret-password") {
		t.Fatal(rr.Code, rr.Body.String())
	}
	var meta secret.Meta
	_ = json.Unmarshal(rr.Body.Bytes(), &meta)
	rr = f.request("GET", "/api/infrastructure/credentials", nil)
	if rr.Code != 200 || strings.Contains(rr.Body.String(), "secret-password") {
		t.Fatal(rr.Code, rr.Body.String())
	}
	rr = f.request("POST", "/api/infrastructure/credentials/"+meta.ID+"/reveal", nil)
	if rr.Code != 200 || !strings.Contains(rr.Body.String(), "secret-password") || rr.Header().Get("Cache-Control") != "no-store" {
		t.Fatal(rr.Code, rr.Body.String())
	}
	ref, _ := f.vault.Put("totp.internal", "system-only")
	rr = f.request("POST", "/api/infrastructure/credentials/"+ref.ID+"/reveal", nil)
	if rr.Code == 200 || strings.Contains(rr.Body.String(), "system-only") {
		t.Fatal(rr.Code, rr.Body.String())
	}
}

func TestInfraUploadDownloadAndDuplicateVersion(t *testing.T) {
	f := setupInfrastructureAPI(t)
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	_ = writer.WriteField("metadata", `{"software_id":"aew","version":"1.0.0","os":"linux","arch":"amd64","format":"binary"}`)
	file, err := writer.CreateFormFile("file", "../../escape")
	if err != nil {
		t.Fatal(err)
	}
	_, _ = file.Write([]byte("program"))
	_ = writer.Close()
	req := httptest.NewRequest("POST", "/api/infrastructure/releases", &body)
	req.Header.Set("Authorization", "Bearer "+f.token)
	req.Header.Set("Content-Type", writer.FormDataContentType())
	rr := httptest.NewRecorder()
	f.handler.ServeHTTP(rr, req)
	if rr.Code != 201 {
		t.Fatal(rr.Code, rr.Body.String())
	}
	var release infrastructure.Release
	_ = json.Unmarshal(rr.Body.Bytes(), &release)
	if !release.Cached || len(release.SHA256) != 64 {
		t.Fatal(release)
	}
	rr = f.request("GET", "/api/infrastructure/releases/"+release.ID+"/download", nil)
	if rr.Code != 200 || rr.Body.String() != "program" {
		t.Fatal(rr.Code, rr.Body.String())
	}
	if strings.Contains(rr.Header().Get("Content-Disposition"), "escape") {
		t.Fatal(rr.Header())
	}
	rr = f.request("POST", "/api/infrastructure/releases", map[string]any{"software_id": "aew", "version": "1.0.0", "os": "linux", "arch": "amd64", "format": "binary", "url": "https://example.com/pkg", "sha256": release.SHA256})
	if rr.Code != 409 {
		t.Fatal(rr.Code, rr.Body.String())
	}
}

func TestInfraDeploymentRequiresStepUpAndNoRawCredentialField(t *testing.T) {
	f := setupInfrastructureAPI(t)
	h, err := f.service.SaveHost(context.Background(), infrastructure.Host{Name: "vm", Address: "127.0.0.1", SSHUser: "ubuntu", SSHPort: 22, OS: "linux", Arch: "amd64"})
	if err != nil {
		t.Fatal(err)
	}
	r, err := f.service.AddRelease(context.Background(), infrastructure.Release{SoftwareID: "aew", Version: "1", OS: "linux", Arch: "amd64", Format: "binary"}, strings.NewReader("program"))
	if err != nil {
		t.Fatal(err)
	}
	payload := map[string]any{"action": "install", "host_id": h.ID, "release_id": r.ID}
	rr := f.request("POST", "/api/infrastructure/operations", payload)
	if rr.Code != 403 {
		t.Fatal(rr.Code, rr.Body.String())
	}
	f.stepUp(t)
	payload["password"] = "must-not-persist"
	rr = f.request("POST", "/api/infrastructure/operations", payload)
	if rr.Code != 400 {
		t.Fatal(rr.Code, rr.Body.String())
	}
	delete(payload, "password")
	rr = f.request("POST", "/api/infrastructure/operations", payload)
	if rr.Code != 202 {
		t.Fatal(rr.Code, rr.Body.String())
	}
	var op infrastructure.Operation
	_ = json.Unmarshal(rr.Body.Bytes(), &op)
	rr = f.request("POST", "/api/infrastructure/operations/"+op.ID+"/cancel", nil)
	if rr.Code != 200 {
		t.Fatal(rr.Code, rr.Body.String())
	}
}

func TestInfraTerminalRejectsCrossOrigin(t *testing.T) {
	f := setupInfrastructureAPI(t)
	req := httptest.NewRequest("GET", "http://platform.example/api/infrastructure/terminal?ticket=bad", nil)
	req.Header.Set("Origin", "https://attacker.example")
	rr := httptest.NewRecorder()
	f.handler.ServeHTTP(rr, req)
	if rr.Code != 403 {
		t.Fatal(rr.Code, rr.Body.String())
	}
}

func TestAutoPolicyCannotBypassDeployPermission(t *testing.T) {
	f := setupInfrastructureAPI(t)
	f.stepUp(t)
	if err := f.users.SetRolePermissionScope(context.Background(), "ADMIN", "infra.deploy", "NONE"); err != nil {
		t.Fatal(err)
	}
	rr := f.request("PUT", "/api/infrastructure/policies", infrastructure.Policy{AutoInstall: true})
	if rr.Code != 403 {
		t.Fatal(rr.Code, rr.Body.String())
	}
}

func TestInfraConsolePTYUsesOneUseTicket(t *testing.T) {
	f := setupInfrastructureAPI(t)
	f.stepUp(t)
	_, private, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	signer, _ := ssh.NewSignerFromKey(private)
	config := &ssh.ServerConfig{PasswordCallback: func(_ ssh.ConnMetadata, _ []byte) (*ssh.Permissions, error) { return nil, nil }}
	config.AddHostKey(signer)
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	go func() {
		for {
			conn, err := listener.Accept()
			if err != nil {
				return
			}
			go func() {
				server, channels, requests, err := ssh.NewServerConn(conn, config)
				if err != nil {
					conn.Close()
					return
				}
				defer server.Close()
				go ssh.DiscardRequests(requests)
				for next := range channels {
					channel, requests, err := next.Accept()
					if err != nil {
						return
					}
					go func() {
						defer channel.Close()
						for req := range requests {
							switch req.Type {
							case "pty-req":
								_ = req.Reply(true, nil)
							case "shell":
								_ = req.Reply(true, nil)
								_, _ = io.Copy(channel, channel)
								return
							case "window-change":
								_ = req.Reply(true, nil)
							default:
								_ = req.Reply(false, nil)
							}
						}
					}()
				}
			}()
		}
	}()
	credential, err := f.service.SaveCredential(context.Background(), "ssh", "console", `{"password":"test-only"}`, "admin", "")
	if err != nil {
		t.Fatal(err)
	}
	_, port, _ := net.SplitHostPort(listener.Addr().String())
	number, _ := strconv.Atoi(port)
	host, err := f.service.SaveHost(context.Background(), infrastructure.Host{Name: "console", Address: "127.0.0.1", SSHPort: number, SSHUser: "test", SecretID: credential.ID, HostKey: ssh.FingerprintSHA256(signer.PublicKey()), OS: "linux", Arch: "amd64"})
	if err != nil {
		t.Fatal(err)
	}
	rr := f.request("POST", "/api/infrastructure/hosts/"+host.ID+"/terminal-ticket", nil)
	if rr.Code != 200 {
		t.Fatal(rr.Code, rr.Body.String())
	}
	var ticket struct {
		Ticket string `json:"ticket"`
	}
	_ = json.Unmarshal(rr.Body.Bytes(), &ticket)
	server := httptest.NewServer(f.handler)
	defer server.Close()
	wsURL := "ws" + strings.TrimPrefix(server.URL, "http") + "/api/infrastructure/terminal?ticket=" + ticket.Ticket
	headers := http.Header{"Origin": []string{server.URL}}
	ws, _, err := websocket.DefaultDialer.Dial(wsURL, headers)
	if err != nil {
		t.Fatal(err)
	}
	defer ws.Close()
	_ = ws.SetReadDeadline(time.Now().Add(5 * time.Second))
	if err := ws.WriteJSON(map[string]any{"type": "input", "data": "terminal-echo\n"}); err != nil {
		t.Fatal(err)
	}
	_, content, err := ws.ReadMessage()
	if err != nil || !strings.Contains(string(content), "terminal-echo") {
		t.Fatal(string(content), err)
	}
	ws.Close()
	second, _, err := websocket.DefaultDialer.Dial(wsURL, headers)
	if err != nil {
		t.Fatal(err)
	}
	defer second.Close()
	_ = second.SetReadDeadline(time.Now().Add(5 * time.Second))
	_, content, _ = second.ReadMessage()
	if !strings.Contains(string(content), "票据无效") {
		t.Fatal("ticket was reused", string(content))
	}
}
