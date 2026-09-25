package mcp

import (
	"context"
	"testing"
)

func TestMCPService(t *testing.T) {
	ctx := context.Background()
	store := NewMemoryStore()
	svc := NewService(store, nil)

	// 1. Built-in workflow-mcp is present and cannot be deleted
	wfSrv, err := svc.GetServer(ctx, "mcp-workflow")
	if err != nil || wfSrv == nil {
		t.Fatalf("expected workflow-mcp to exist, got %v", err)
	}
	if err := svc.DeleteServer(ctx, "mcp-workflow"); err != ErrCannotDeleteBuiltin {
		t.Fatalf("expected ErrCannotDeleteBuiltin, got %v", err)
	}

	// 2. Create custom MCP Server
	gh, err := svc.CreateServer(ctx, CreateServerInput{
		Name:        "github-mcp",
		Description: "GitHub MCP Server",
		Transport:   "http",
		Endpoint:    "https://api.github.com/mcp",
	})
	if err != nil {
		t.Fatalf("CreateServer failed: %v", err)
	}
	if gh.ServerType != ServerTypeCustom {
		t.Errorf("expected ServerTypeCustom, got %s", gh.ServerType)
	}

	// 3. Create Credential with masking
	cred, err := svc.CreateCredential(ctx, CreateCredentialInput{
		OwnerType:      OwnerTypeUser,
		OwnerID:        "user-1",
		Provider:       "github",
		AuthType:       AuthTypePAT,
		CredentialName: "张三 GitHub PAT",
		SecretValue:    "ghp_1234567890abcdef",
	})
	if err != nil {
		t.Fatalf("CreateCredential failed: %v", err)
	}
	if cred.MaskedValue == "ghp_1234567890abcdef" || cred.MaskedValue == "" {
		t.Errorf("credential secret should be masked, got: %s", cred.MaskedValue)
	}

	// 4. Bind employee to custom MCP
	bnd, err := svc.BindEmployee(ctx, "EMP-100", BindEmployeeInput{
		MCPServerID:  gh.ID,
		CredentialID: cred.ID,
		AllowedTools: []string{"search_code", "get_issue"},
	})
	if err != nil {
		t.Fatalf("BindEmployee failed: %v", err)
	}
	if len(bnd.AllowedTools) != 2 {
		t.Errorf("expected 2 allowed tools, got %d", len(bnd.AllowedTools))
	}

	// 5. Duplicate binding should fail
	_, err = svc.BindEmployee(ctx, "EMP-100", BindEmployeeInput{
		MCPServerID: gh.ID,
	})
	if err != ErrBindingConflict {
		t.Fatalf("expected ErrBindingConflict, got %v", err)
	}

	// 6. List bindings for employee
	bnds, err := svc.ListBindingsByEmployee(ctx, "EMP-100")
	if err != nil || len(bnds) != 1 {
		t.Fatalf("expected 1 binding, got %d (err: %v)", len(bnds), err)
	}

	// 7. Unbind
	if err := svc.UnbindEmployee(ctx, bnd.ID); err != nil {
		t.Fatalf("UnbindEmployee failed: %v", err)
	}
	bnds, _ = svc.ListBindingsByEmployee(ctx, "EMP-100")
	if len(bnds) != 0 {
		t.Errorf("expected 0 bindings after unbind, got %d", len(bnds))
	}
}
