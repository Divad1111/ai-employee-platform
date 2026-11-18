package mcp

import (
	"context"
	"testing"
)

func TestUpdateCredentialPreservesAndRotatesSecret(t *testing.T) {
	ctx := context.Background()
	svc := authTestService(t)
	cred, err := svc.CreateCredential(ctx, CreateCredentialInput{
		CredentialName: "original", OwnerType: OwnerTypeUser, OwnerID: "first-user", SecretValue: "original-secret",
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.UpdateCredential(ctx, cred.ID, CreateCredentialInput{OwnerID: "second-user", CredentialName: "renamed"}); err != nil {
		t.Fatal(err)
	}
	value, err := svc.ResolveSecretValue(ctx, cred.ID)
	if err != nil || value != "original-secret" {
		t.Fatal("editing without a secret did not preserve stored secret", err)
	}
	if _, err := svc.UpdateCredential(ctx, cred.ID, CreateCredentialInput{SecretValue: "replacement-secret"}); err != nil {
		t.Fatal(err)
	}
	value, err = svc.ResolveSecretValue(ctx, cred.ID)
	if err != nil || value != "replacement-secret" {
		t.Fatal("secret update did not take effect", err)
	}
	got, err := svc.GetCredential(ctx, cred.ID)
	if err != nil || got.OwnerType != OwnerTypeUser || got.OwnerID != "second-user" || got.CredentialName != "renamed" || got.SecretRef != cred.SecretRef {
		t.Fatal("partial update lost credential fields", err)
	}
}

func TestUpdateCredentialReportsVaultFailure(t *testing.T) {
	ctx := context.Background()
	svc := authTestService(t)
	cred, err := svc.CreateCredential(ctx, CreateCredentialInput{CredentialName: "original", SecretValue: "original-secret"})
	if err != nil {
		t.Fatal(err)
	}
	// An unavailable stored reference must not produce a successful save response.
	cred.SecretRef = "missing-secret"
	if err := svc.store.UpdateCredential(ctx, cred); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.UpdateCredential(ctx, cred.ID, CreateCredentialInput{CredentialName: "must-not-save", SecretValue: "replacement-secret"}); err == nil {
		t.Fatal("expected vault error")
	}
	got, err := svc.GetCredential(ctx, cred.ID)
	if err != nil || got.CredentialName != "original" || got.MaskedValue != cred.MaskedValue {
		t.Fatal("failed secret update saved credential metadata", err)
	}
}
