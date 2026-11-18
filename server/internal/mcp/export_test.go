package mcp

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
)

func TestExportRegistryAndAuthentication(t *testing.T) {
	ctx := context.Background()
	svc := authTestService(t)
	cred, err := svc.CreateCredential(ctx, CreateCredentialInput{
		CredentialName: "my Jira", OwnerType: OwnerTypeUser, OwnerID: "user-a", CreatedBy: "user-a",
		AuthType: AuthTypeToken, SecretValue: "synthetic-jira-secret",
	})
	if err != nil {
		t.Fatal(err)
	}
	servers := []CreateServerInput{
		{Name: "public", Endpoint: "https://public.example/mcp"},
		{Name: "jira", Endpoint: "https://jira.example/mcp", Config: map[string]any{
			"headers":              map[string]any{"authorization": "old", "X-Client": "desktop"},
			"export_credential_id": cred.ID,
		}},
		{Name: "local", Transport: TransportStdio, Endpoint: "node", Config: map[string]any{
			"command": "C:\\Program Files\\node.exe", "args": []string{"server.js", "--name=中文"},
			"env": map[string]string{"MY_TOKEN": "synthetic-env-token"}, "export_credential_id": "",
		}},
		{Name: "disabled", Endpoint: "https://disabled.example/mcp", Status: StatusDisabled},
	}
	for _, server := range servers {
		if _, err := svc.CreateServer(ctx, server); err != nil {
			t.Fatal(err)
		}
	}
	config, builtin, _, err := svc.PrepareExport(ctx, "user-a", "https://platform.example/mcp", "json")
	if err != nil {
		t.Fatal(err)
	}
	if len(config.MCPServers) != 6 || builtin != "workflow-mcp" || config.MCPServers[builtin].URL != "https://platform.example/mcp" {
		t.Fatal("export must include every registered server and the public builtin URL")
	}
	if entry := config.MCPServers["personal-work-collector"]; entry == nil || entry.URL != "https://platform.example/mcp/collector" {
		t.Fatal("collector export must use its own public endpoint")
	}
	jira := config.MCPServers["jira"]
	if len(jira.Headers) != 2 || jira.Headers["Authorization"] != "Token synthetic-jira-secret" || jira.Headers["X-Client"] != "desktop" {
		t.Fatal("credential injection did not replace the case-insensitive header")
	}
	if !config.MCPServers["disabled"].Disabled {
		t.Fatal("disabled registration lost its state")
	}
	content, filename, err := RenderExport(config, "json")
	if err != nil || filename != "mcp.json" || !json.Valid([]byte(content)) {
		t.Fatal("invalid JSON export")
	}
	if strings.Contains(content, "export_credential_id") || strings.Contains(content, cred.ID) {
		t.Fatal("export contains internal registry metadata")
	}
	content, filename, err = RenderExport(config, "codex")
	if err != nil || filename != "mcp.codex.toml" ||
		!strings.Contains(content, `[mcp_servers."jira".http_headers]`) ||
		!strings.Contains(content, `[mcp_servers."local".env]`) ||
		!strings.Contains(content, `command = "C:\\Program Files\\node.exe"`) ||
		!strings.Contains(content, "enabled = false") {
		t.Fatal("invalid Codex configuration")
	}
	original, _ := svc.GetServer(ctx, findServerID(t, svc, "jira"))
	if original.Config["headers"].(map[string]any)["authorization"] != "old" {
		t.Fatal("export mutated the registered headers")
	}
	if _, _, _, err := svc.PrepareExport(ctx, "user-b", "https://platform.example/mcp", "json"); err == nil {
		t.Fatal("a different user exported someone else's credential")
	}
}

func findServerID(t *testing.T, svc *Service, name string) string {
	t.Helper()
	servers, err := svc.ListServers(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	for _, server := range servers {
		if server.Name == name {
			return server.ID
		}
	}
	t.Fatal("server missing")
	return ""
}

func TestExportCredentialSelectionAndValidation(t *testing.T) {
	ctx := context.Background()
	svc := authTestService(t)
	server, _ := svc.CreateServer(ctx, CreateServerInput{Name: "remote", Endpoint: "https://remote.example/mcp"})
	newCredential := func(name string) *Credential {
		t.Helper()
		cred, err := svc.CreateCredential(ctx, CreateCredentialInput{
			CredentialName: name, OwnerID: "owner", AuthType: AuthTypeBearer, SecretValue: "synthetic-secret",
		})
		if err != nil {
			t.Fatal(err)
		}
		return cred
	}
	first := newCredential("first")
	if _, err := svc.BindEmployee(ctx, "emp-1", BindEmployeeInput{MCPServerID: server.ID, CredentialID: first.ID}); err != nil {
		t.Fatal(err)
	}
	config, _, _, err := svc.PrepareExport(ctx, "owner", "https://platform.example/mcp", "json")
	if err != nil || config.MCPServers["remote"].Headers["Authorization"] != "Bearer synthetic-secret" {
		t.Fatal("unique owned binding was not exported")
	}
	second := newCredential("second")
	if _, err := svc.BindEmployee(ctx, "emp-2", BindEmployeeInput{MCPServerID: server.ID, CredentialID: second.ID}); err != nil {
		t.Fatal(err)
	}
	if _, _, _, err := svc.PrepareExport(ctx, "owner", "https://platform.example/mcp", "json"); err == nil {
		t.Fatal("ambiguous credentials must not be picked arbitrarily")
	}
	server.Config = map[string]any{"export_credential_id": first.ID}
	_ = svc.store.UpdateServer(ctx, server)
	first.Status = StatusRevoked
	_ = svc.store.UpdateCredential(ctx, first)
	if _, _, _, err := svc.PrepareExport(ctx, "owner", "https://platform.example/mcp", "json"); err == nil {
		t.Fatal("revoked credential was exported")
	}
	server.Config = map[string]any{"export_credential_id": ""}
	_ = svc.store.UpdateServer(ctx, server)
	config, _, _, err = svc.PrepareExport(ctx, "owner", "https://platform.example/mcp", "json")
	if err != nil || len(config.MCPServers["remote"].Headers) != 0 {
		t.Fatal("explicit server-only mode must not inject an employee identity")
	}
	server.Transport = TransportSSE
	_ = svc.store.UpdateServer(ctx, server)
	if _, _, _, err := svc.PrepareExport(ctx, "owner", "https://platform.example/mcp", "codex"); err == nil {
		t.Fatal("Codex export silently included unsupported SSE")
	}
	if _, _, _, err := svc.PrepareExport(ctx, "owner", "https://platform.example/mcp", "json"); err != nil {
		t.Fatal("JSON export rejected SSE")
	}
	server.Name = "workflow-mcp"
	_ = svc.store.UpdateServer(ctx, server)
	if _, _, _, err := svc.PrepareExport(ctx, "owner", "https://platform.example/mcp", "json"); err == nil {
		t.Fatal("duplicate server names silently dropped a registration")
	}
}
