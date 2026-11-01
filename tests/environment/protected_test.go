package environment_test

import (
	"testing"

	serviceimpl "github.com/gsoultan/metis/server/domains/services/impl"
	stormdb "github.com/gsoultan/metis/server/repositories/db"
)

// A request is in a protected runtime when the environment its port serves is
// marked protected, and in the installation's own runtime only when
// METIS_PROTECTED_RUNTIME says so. The mark survives an edit that does not
// mention it, because the endpoint keeps what is stored.
func TestARuntimeIsProtectedWhereItIsMarked(t *testing.T) {
	svc, projectID, ctx := fixture(t)

	production := staging(projectID, "production", 8091)
	production.Protected = true
	prodID, err := svc.CreateEnvironment(ctx, production)
	if err != nil {
		t.Fatalf("create production: %v", err)
	}
	stagingID, err := svc.CreateEnvironment(ctx, staging(projectID, "staging", 8092))
	if err != nil {
		t.Fatalf("create staging: %v", err)
	}

	if got, err := svc.RuntimeProtected(stormdb.Bind(ctx, prodID)); err != nil || !got {
		t.Fatalf("production: protected=%v err=%v, want protected", got, err)
	}
	if got, err := svc.RuntimeProtected(stormdb.Bind(ctx, stagingID)); err != nil || got {
		t.Fatalf("staging: protected=%v err=%v, want unprotected", got, err)
	}

	t.Setenv(serviceimpl.EnvProtectedRuntime, "")
	if got, err := svc.RuntimeProtected(ctx); err != nil || got {
		t.Fatalf("main runtime with the setting unset: protected=%v err=%v", got, err)
	}
	t.Setenv(serviceimpl.EnvProtectedRuntime, "true")
	if got, err := svc.RuntimeProtected(ctx); err != nil || !got {
		t.Fatalf("main runtime marked protected: protected=%v err=%v", got, err)
	}

	stored, err := svc.GetEnvironment(ctx, prodID)
	if err != nil || !stored.Protected {
		t.Fatalf("read back production: protected=%v err=%v", stored.Protected, err)
	}
	stored.Protected = false
	if err := svc.UpdateEnvironment(ctx, stored); err != nil {
		t.Fatalf("unprotect: %v", err)
	}
	if got, _ := svc.RuntimeProtected(stormdb.Bind(ctx, prodID)); got {
		t.Fatal("an administrator unprotected production and it stayed protected")
	}
}
