package api_test

import (
	"context"
	"encoding/pem"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ai-employee-platform/server/internal/auth"
	"github.com/ai-employee-platform/server/internal/infrastructure"
)

func TestInfrastructureManagerCannotRetargetStoredCredential(t *testing.T) {
	ctx := context.Background()
	f := setupInfrastructureAPI(t)
	ref, err := f.service.SaveCredential(ctx, "pve", "target", "test@pve!token=synthetic-secret", "admin", "")
	if err != nil {
		t.Fatal(err)
	}
	c, err := f.service.SaveCluster(ctx, infrastructure.Cluster{Name: "trusted", URL: "https://trusted.invalid", SecretID: ref.ID})
	if err != nil {
		t.Fatal(err)
	}
	_ = f.users.CreateRole(ctx, "INFRA_MANAGER", "", []auth.PermissionGrant{{Code: "infra.manage", Scope: "ALL"}})
	hash, _ := auth.HashPassword("manager-password-123")
	u := &auth.User{ID: "manager", Username: "manager", PasswordHash: hash, Status: auth.StatusActive, Roles: []string{"INFRA_MANAGER"}}
	if err := f.users.Create(ctx, u); err != nil {
		t.Fatal(err)
	}
	sess, err := f.auth.Login(ctx, u.Username, "manager-password-123", "127.0.0.1")
	if err != nil {
		t.Fatal(err)
	}
	f.token = sess.Token
	_, _ = f.auth.StepUp(ctx, sess.Token, "manager-password-123", "", time.Minute)
	var captured atomic.Bool
	remote := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") == "PVEAPIToken=test@pve!token=synthetic-secret" {
			captured.Store(true)
		}
		_, _ = w.Write([]byte(`{"data":[]}`))
	}))
	defer remote.Close()
	c.URL = remote.URL
	c.CAPEM = string(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: remote.Certificate().Raw}))
	for _, path := range []string{"/api/infrastructure/clusters/" + c.ID, "/api/infrastructure/clusters"} {
		method := "PUT"
		if path == "/api/infrastructure/clusters" {
			method = "POST"
		}
		rr := f.request(method, path, c)
		if rr.Code != 403 {
			t.Fatal(rr.Code, rr.Body.String())
		}
	}
	for _, path := range []string{"hosts", "software"} {
		if rr := f.request("POST", "/api/infrastructure/"+path, map[string]string{}); rr.Code != 403 {
			t.Fatal(path, rr.Code)
		}
	}
	rows, _ := f.service.Clusters(ctx)
	if rows[0].URL != "https://trusted.invalid" || captured.Load() {
		t.Fatal("credential destination changed")
	}
	// 明确授予凭据权限后，才允许将凭据绑定至新目标。
	_ = f.users.SetRolePermissionScope(ctx, "INFRA_MANAGER", "infra.credential", "ALL")
	rr := f.request("PUT", "/api/infrastructure/clusters/"+c.ID, c)
	if rr.Code != 200 {
		t.Fatal(rr.Code, rr.Body.String())
	}
	rr = f.request("POST", "/api/infrastructure/clusters/"+c.ID+"/sync", nil)
	if rr.Code != 200 || !captured.Load() {
		t.Fatal("authorized target unusable", rr.Code)
	}
}
