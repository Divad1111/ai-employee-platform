package api_test

import (
	"context"
	"testing"

	"github.com/ai-employee-platform/server/internal/api"
	"github.com/ai-employee-platform/server/internal/audit"
	"github.com/ai-employee-platform/server/internal/auth"
	"github.com/ai-employee-platform/server/internal/mcp"
)

func TestCredentialUpdatePersistsEditableFields(t *testing.T) {
	h, token := setupAPI(t)
	code, body := doJSON(t, h, "POST", "/api/credentials", token, map[string]any{
		"credential_name": "original", "owner_type": "USER", "owner_id": "first-user",
		"provider": "github", "auth_type": "pat", "secret_value": "original-secret",
	})
	if code != 201 {
		t.Fatal(code, body)
	}
	original := body["credential"].(map[string]any)
	path := "/api/credentials/" + original["id"].(string)
	for _, owner := range []struct{ kind, id string }{
		{"USER", "second-user"}, {"ORGANIZATION", "ORGANIZATION"}, {"SERVICE_ACCOUNT", "automation"},
	} {
		t.Run(owner.kind, func(t *testing.T) {
			changes := map[string]any{
				"credential_name": "updated", "owner_type": owner.kind, "owner_id": owner.id,
				"provider": "jira", "auth_type": "api_key",
				"metadata": map[string]any{"header_name": "X-API-Key", "header_prefix": ""},
			}
			code, body = doJSON(t, h, "PUT", path, token, changes)
			if code != 200 {
				t.Fatal(code, body)
			}
			code, body = doJSON(t, h, "GET", path, token, nil)
			if code != 200 {
				t.Fatal(code, body)
			}
			got := body["credential"].(map[string]any)
			for _, key := range []string{"credential_name", "owner_type", "owner_id", "provider", "auth_type"} {
				if got[key] != changes[key] {
					t.Fatalf("%s not persisted: got %v, want %v", key, got[key], changes[key])
				}
			}
			metadata := got["metadata"].(map[string]any)
			if metadata["header_name"] != "X-API-Key" || metadata["header_prefix"] != "" {
				t.Fatal("authentication metadata not persisted")
			}
			if got["masked_value"] != original["masked_value"] || got["created_by"] != original["created_by"] {
				t.Fatal("editing metadata changed secret or creator")
			}
		})
	}
}

func TestCredentialOwnerScopeCannotTransferOwnership(t *testing.T) {
	ctx := context.Background()
	users := auth.NewMemoryUserStore()
	if err := users.CreateRole(ctx, "CREDENTIAL_OWNER", "", []auth.PermissionGrant{{Code: "workflow.write", Scope: "OWN"}}); err != nil {
		t.Fatal(err)
	}
	hash, err := auth.HashPassword("test-password-123")
	if err != nil {
		t.Fatal(err)
	}
	u := &auth.User{ID: "owner", Username: "owner", PasswordHash: hash, Status: auth.StatusActive, Roles: []string{"CREDENTIAL_OWNER"}}
	if err := users.Create(ctx, u); err != nil {
		t.Fatal(err)
	}
	a := auth.NewService(users, auth.NewMemorySessionStore(), audit.NewMemory())
	sess, err := a.Login(ctx, u.Username, "test-password-123", "127.0.0.1")
	if err != nil {
		t.Fatal(err)
	}
	svc := mcp.NewService(mcp.NewMemoryStore(), nil)
	cred, err := svc.CreateCredential(ctx, mcp.CreateCredentialInput{CredentialName: "original", OwnerType: "USER", OwnerID: u.ID})
	if err != nil {
		t.Fatal(err)
	}
	h := api.NewRouter(api.Deps{Auth: a, MCP: svc})
	path := "/api/credentials/" + cred.ID
	for _, changes := range []map[string]string{
		{"owner_id": "another-user"},
		{"owner_type": "ORGANIZATION", "owner_id": "ORGANIZATION"},
		{"owner_type": "SERVICE_ACCOUNT", "owner_id": "bot"},
	} {
		changes["credential_name"] = "must-not-save"
		code, body := doJSON(t, h, "PUT", path, sess.Token, changes)
		if code != 403 {
			t.Fatal(code, body)
		}
	}
	got, err := svc.GetCredential(ctx, cred.ID)
	if err != nil || got.OwnerType != "USER" || got.OwnerID != u.ID || got.CredentialName != "original" {
		t.Fatal("rejected transfer modified credential", err)
	}
	for _, changes := range []map[string]string{
		{"credential_name": "renamed"},
		{"credential_name": "renamed", "owner_type": "USER", "owner_id": u.ID},
	} {
		code, body := doJSON(t, h, "PUT", path, sess.Token, changes)
		if code != 200 {
			t.Fatal("owner cannot edit own credential", code, body)
		}
	}
}
