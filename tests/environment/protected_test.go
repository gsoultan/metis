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

// A page learns which runtime it is served from — by the port it reached —
// and the project's other environments, by name, to say "you are in
// production" and to offer where a release comes from.
func TestARequestLearnsWhichRuntimeItIsIn(t *testing.T) {
	svc, projectID, ctx := fixture(t)
	production := staging(projectID, "production", 8093)
	production.Protected = true
	prodID, err := svc.CreateEnvironment(ctx, production)
	if err != nil {
		t.Fatalf("create production: %v", err)
	}
	if _, err := svc.CreateEnvironment(ctx, staging(projectID, "staging", 8094)); err != nil {
		t.Fatalf("create staging: %v", err)
	}

	inProd, err := svc.CurrentRuntime(stormdb.Bind(ctx, prodID), projectID)
	if err != nil {
		t.Fatalf("current runtime in production: %v", err)
	}
	if inProd.Current.ID != prodID || inProd.Current.Name != "production" || !inProd.Current.Protected {
		t.Fatalf("in production the current runtime is %+v", inProd.Current)
	}
	if len(inProd.Environments) != 2 {
		t.Fatalf("the project lists %d environments, want 2", len(inProd.Environments))
	}

	own, err := svc.CurrentRuntime(ctx, projectID)
	if err != nil {
		t.Fatalf("current runtime on the main port: %v", err)
	}
	if own.Current.Name != "" || own.Current.Protected {
		t.Fatalf("on the main port the current runtime is %+v, want the installation's own, unprotected", own.Current)
	}
}
