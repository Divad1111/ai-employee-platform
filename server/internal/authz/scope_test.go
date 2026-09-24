package authz_test

import (
	"testing"

	"github.com/ai-employee-platform/server/internal/authz"
)

func TestResolve(t *testing.T) {
	grants := []authz.Grant{
		{Name: "employee.read", Scope: authz.ScopeOWN},
		{Name: "workstation.read", Scope: authz.ScopeASSIGNED},
	}
	if authz.Resolve(grants, "employee.read") != authz.ScopeOWN {
		t.Fatal("own")
	}
	if authz.Resolve(grants, "missing") != authz.ScopeNONE {
		t.Fatal("none")
	}
	if authz.Resolve([]authz.Grant{{Name: "*", Scope: authz.ScopeALL}}, "anything") != authz.ScopeALL {
		t.Fatal("star")
	}
}
