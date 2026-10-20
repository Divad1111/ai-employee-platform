package mcp

import (
	"context"
	"errors"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/ai-employee-platform/server/internal/secret"
)

func authTestService(t *testing.T) *Service {
	t.Helper()
	vault, err := secret.NewMemoryVault()
	if err != nil {
		t.Fatal(err)
	}
	return NewService(NewMemoryStore(), secret.NewManager(vault, secret.NewMemoryBindings(), nil))
}

func TestResolveAuthentication(t *testing.T) {
	for _, tc := range []struct {
		name, authType, value, transport, header, want string
		metadata                                       map[string]any
	}{
		{name: "legacy API key", authType: AuthTypeAPIKey, value: "key", header: "Authorization", want: "Bearer key"},
		{name: "bearer", authType: AuthTypeBearer, value: "key", header: "Authorization", want: "Bearer key"},
		{name: "oauth access token", authType: AuthTypeOAuth, value: "key", header: "Authorization", want: "Bearer key"},
		{name: "pat", authType: AuthTypePAT, value: "key", header: "Authorization", want: "Bearer key"},
		{name: "token", authType: AuthTypeToken, value: "key", header: "Authorization", want: "Token key"},
		{name: "basic", authType: AuthTypeBasic, value: "user:p:a:ss", header: "Authorization", want: "Basic dXNlcjpwOmE6c3M="},
		{name: "custom header", authType: AuthTypeAPIKey, value: "key", metadata: map[string]any{"header_name": "X-API-Key"}, header: "X-API-Key", want: "key"},
		{name: "custom prefix", authType: AuthTypePAT, value: "key", metadata: map[string]any{"header_prefix": "Token"}, header: "Authorization", want: "Token key"},
		{name: "raw authorization", authType: AuthTypeAPIKey, value: "key", metadata: map[string]any{"header_prefix": ""}, header: "Authorization", want: "key"},
		{name: "stdio", authType: AuthTypeAPIKey, value: "key", transport: TransportStdio, metadata: map[string]any{"env_name": "MCP_API_KEY"}, want: "key"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			svc := authTestService(t)
			cred, err := svc.CreateCredential(context.Background(), CreateCredentialInput{
				CredentialName: tc.name, AuthType: tc.authType, SecretValue: tc.value, Metadata: tc.metadata,
			})
			if err != nil {
				t.Fatal(err)
			}
			transport := tc.transport
			if transport == "" {
				transport = TransportHTTP
			}
			got, err := svc.ResolveAuthentication(context.Background(), cred.ID, transport)
			if err != nil {
				t.Fatal(err)
			}
			if transport == TransportStdio {
				if got.Env["MCP_API_KEY"] != tc.want || len(got.Headers) != 0 {
					t.Fatal("STDIO credential was not injected as an environment variable")
				}
				return
			}
			req := httptest.NewRequest("POST", "http://mcp.test/mcp", nil)
			for k, v := range got.Headers {
				req.Header.Set(k, v)
			}
			if req.Header.Get(tc.header) != tc.want {
				t.Fatalf("unexpected authentication for %s", tc.name)
			}
			if tc.authType == AuthTypeBasic {
				user, password, ok := req.BasicAuth()
				if !ok || user != "user" || password != "p:a:ss" {
					t.Fatal("Basic authentication cannot be decoded")
				}
			}
		})
	}
}

func TestUnusableCredentialsAreRejected(t *testing.T) {
	for _, tc := range []struct {
		name, status string
		expired      bool
		want         error
	}{
		{"revoked", StatusRevoked, false, ErrCredentialInactive},
		{"disabled", StatusDisabled, false, ErrCredentialInactive},
		{"expired", StatusActive, true, ErrCredentialExpired},
		{"marked expired", StatusExpired, false, ErrCredentialExpired},
	} {
		t.Run(tc.name, func(t *testing.T) {
			svc := authTestService(t)
			cred, err := svc.CreateCredential(context.Background(), CreateCredentialInput{CredentialName: "test", SecretValue: "secret-not-in-error"})
			if err != nil {
				t.Fatal(err)
			}
			cred.Status = tc.status
			if tc.expired {
				expiry := time.Now().Add(-time.Minute)
				cred.ExpiresAt = &expiry
			}
			if err := svc.store.UpdateCredential(context.Background(), cred); err != nil {
				t.Fatal(err)
			}
			_, err = svc.ResolveAuthentication(context.Background(), cred.ID, TransportHTTP)
			if !errors.Is(err, tc.want) {
				t.Fatalf("expected %v, got %v", tc.want, err)
			}
			if strings.Contains(err.Error(), "secret-not-in-error") {
				t.Fatal("secret leaked")
			}
			if _, err := svc.ResolveSecretValue(context.Background(), cred.ID); !errors.Is(err, tc.want) {
				t.Fatal("raw resolution bypassed credential validation")
			}
		})
	}
}

func TestAuthenticationFailsClosed(t *testing.T) {
	for _, tc := range []struct {
		name, authType, value, transport string
		metadata                         map[string]any
	}{
		{name: "missing secret", authType: AuthTypeBearer, transport: TransportHTTP},
		{name: "invalid basic", authType: AuthTypeBasic, value: "secret", transport: TransportHTTP},
		{name: "header injection", authType: AuthTypeBearer, value: "key\r\nX-Injected: true", transport: TransportHTTP},
		{name: "stdio missing env", authType: AuthTypeAPIKey, value: "key", transport: TransportStdio},
		{name: "env on HTTP", authType: AuthTypeAPIKey, value: "key", transport: TransportHTTP, metadata: map[string]any{"env_name": "MCP_KEY"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			svc := authTestService(t)
			cred, err := svc.CreateCredential(context.Background(), CreateCredentialInput{CredentialName: tc.name, AuthType: tc.authType, SecretValue: tc.value, Metadata: tc.metadata})
			if err != nil {
				t.Fatal(err)
			}
			if _, err := svc.ResolveAuthentication(context.Background(), cred.ID, tc.transport); err == nil {
				t.Fatal("expected invalid authentication to fail")
			}
		})
	}
}

func TestInvalidAuthMetadataRejected(t *testing.T) {
	for _, metadata := range []map[string]any{
		{"header_name": "Bad:Header"}, {"header_prefix": "Token\nInjected"}, {"header_name": 42},
		{"env_name": "KEY=VALUE"}, {"env_name": "KEY", "header_name": "Authorization"},
	} {
		svc := authTestService(t)
		if _, err := svc.CreateCredential(context.Background(), CreateCredentialInput{CredentialName: "invalid", Metadata: metadata}); err == nil {
			t.Fatal("invalid metadata accepted")
		}
	}
}
