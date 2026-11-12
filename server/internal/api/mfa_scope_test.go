package api_test

import (
	"context"
	"net/http"
	"testing"

	"github.com/ai-employee-platform/server/internal/api"
	"github.com/ai-employee-platform/server/internal/audit"
	"github.com/ai-employee-platform/server/internal/auth"
	"github.com/ai-employee-platform/server/internal/employee"
	"github.com/ai-employee-platform/server/internal/eventbus"
	"github.com/ai-employee-platform/server/internal/job"
)

func TestOrdinaryUserAccessWithoutAdministratorTOTPEnrollment(t *testing.T) {
	ctx := context.Background()
	users := auth.NewMemoryUserStore()
	hash, _ := auth.HashPassword("user-password-123")
	u := &auth.User{ID: "ordinary", Username: "ordinary", PasswordHash: hash, Roles: []string{"USER"}, Status: auth.StatusActive}
	if err := users.Create(ctx, u); err != nil {
		t.Fatal(err)
	}
	if err := users.SeedAdmin("manager", "admin-password-123", "Manager"); err != nil {
		t.Fatal(err)
	}
	aud := audit.NewMemory()
	service := auth.NewService(users, auth.NewMemorySessionStore(), aud)
	service.EnforceAdminMFA = true
	bus := eventbus.New(10)
	h := api.NewRouter(api.Deps{
		Auth:      service,
		Jobs:      job.NewService(job.NewMemoryStore(), aud, bus),
		Employees: employee.NewService(employee.NewMemoryStore(), aud, bus),
	})
	session, err := service.Login(ctx, u.Username, "user-password-123", "")
	if err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{"/api/jobs", "/api/users/ordinary"} {
		code, body := doJSON(t, h, http.MethodGet, path, session.Token, nil)
		if code != http.StatusOK {
			t.Fatalf("ordinary user blocked at %s: %d %v", path, code, body)
		}
	}
	admin, err := service.Login(ctx, "manager", "admin-password-123", "")
	if err != nil {
		t.Fatal(err)
	}
	code, body := doJSON(t, h, http.MethodGet, "/api/jobs", admin.Token, nil)
	if code != http.StatusForbidden || body["needs_mfa_enrollment"] != true {
		t.Fatalf("administrator MFA bypassed: %d %v", code, body)
	}
}
