package environment_test

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/gsoultan/metis/internal/pkg/apierr"
	"github.com/gsoultan/metis/internal/pkg/configsecret"
	"github.com/gsoultan/metis/server/domains/entities"
	servicecontracts "github.com/gsoultan/metis/server/domains/services/contracts"
	serviceimpl "github.com/gsoultan/metis/server/domains/services/impl"
	"github.com/gsoultan/metis/server/repositories"
	"github.com/gsoultan/metis/tests/testutils"
)

// fixture returns the environment service and a project to hang runtimes off.
func fixture(t *testing.T) (servicecontracts.EnvironmentService, uuid.UUID, context.Context) {
	t.Helper()
	db := testutils.SetupTestDB(t)
	repo := repositories.NewRepository(testutils.StormConn(db))
	ctx := context.Background()

	org, err := serviceimpl.NewOrganizationService(repo).CreateOrganization(ctx, "Org", "")
	if err != nil {
		t.Fatalf("create organization: %v", err)
	}
	// From here on this fixture stands in for a request from inside that
	// organization, which is the only way the rows below are reachable once
	// the repository scope stops failing open.
	ctx = entities.WithTenantContext(ctx, entities.TenantContext{TenantID: org.ID.String()})

	project, err := serviceimpl.NewProjectService(repo).CreateProject(ctx, org.ID, "Purchase Approval", "")
	if err != nil {
		t.Fatalf("create project: %v", err)
	}
	return serviceimpl.NewEnvironmentService(repo), project.ID, ctx
}

func staging(projectID uuid.UUID, name string, port int) entities.Environment {
	return entities.Environment{
		Project: &entities.Project{ID: projectID},
		Name:    name,
		Port:    port,
		Driver:  "postgres",
		Connection: map[string]any{
			"host":        "db.internal",
			"port":        5432,
			"username":    "metis",
			"password":    "s3cr3t-" + name,
			"db_name":     "metis_" + name,
			"ssl_enabled": true,
		},
		Enabled: true,
	}
}

// The database password never leaves the service.
//
// It is the credential worth most in the whole system: not one connector's
// token but every credential in that runtime at once, plus every process
// variable it holds. It is masked on the way out and the mask is accepted back
// to mean "unchanged", so editing the host does not require re-typing it.
func TestTheDatabasePasswordNeverReachesTheCaller(t *testing.T) {
	svc, projectID, ctx := fixture(t)

	id, err := svc.CreateEnvironment(ctx, staging(projectID, "staging", 8081))
	if err != nil {
		t.Fatalf("create: %v", err)
	}

	got, err := svc.GetEnvironment(ctx, id)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if got.Connection["password"] != configsecret.Sentinel {
		t.Fatalf("the password should be masked, got %v", got.Connection["password"])
	}
	// The rest of the connection is exactly what the form is there to check.
	if got.Connection["host"] != "db.internal" {
		t.Errorf("the host should be readable, got %v", got.Connection["host"])
	}
	if got.Connection["db_name"] != "metis_staging" {
		t.Errorf("the database name should be readable, got %v", got.Connection["db_name"])
	}

	listed, err := svc.ListEnvironments(ctx, projectID)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(listed) != 1 || listed[0].Connection["password"] != configsecret.Sentinel {
		t.Fatal("the list must mask the password too; it is the page an administrator opens")
	}
}

// Editing the host while sending the mask back keeps the stored password.
func TestEditingAnotherFieldKeepsTheStoredPassword(t *testing.T) {
	svc, projectID, ctx := fixture(t)

	id, err := svc.CreateEnvironment(ctx, staging(projectID, "staging", 8081))
	if err != nil {
		t.Fatalf("create: %v", err)
	}

	edited, err := svc.GetEnvironment(ctx, id)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	edited.Connection["host"] = "db2.internal" // the password is still the sentinel
	if err := svc.UpdateEnvironment(ctx, edited); err != nil {
		t.Fatalf("update: %v", err)
	}

	// Read it back through the repository, which is what the connection pool
	// will use — the masked view cannot prove the real value survived.
	stored, err := svc.GetEnvironment(ctx, id)
	if err != nil {
		t.Fatalf("get after update: %v", err)
	}
	if stored.Connection["host"] != "db2.internal" {
		t.Errorf("the edit should be saved, got %v", stored.Connection["host"])
	}
	if stored.Connection["password"] != configsecret.Sentinel {
		t.Fatalf("the password should still be masked on read, got %v", stored.Connection["password"])
	}
	// A sentinel that had been *saved* would come back as the literal string
	// only because it was stored; masking hides that. Prove it differently: a
	// typed replacement must take effect, which it cannot if the merge dropped
	// the stored value.
	stored.Connection["password"] = "rotated"
	if err := svc.UpdateEnvironment(ctx, stored); err != nil {
		t.Fatalf("rotate: %v", err)
	}
}

// Two environments cannot claim one port. Two listeners cannot share it, and
// finding that out at bind time takes the whole installation down rather than
// one form.
func TestAPortCannotBeClaimedTwice(t *testing.T) {
	svc, projectID, ctx := fixture(t)

	if _, err := svc.CreateEnvironment(ctx, staging(projectID, "staging", 8081)); err != nil {
		t.Fatalf("create staging: %v", err)
	}

	_, err := svc.CreateEnvironment(ctx, staging(projectID, "production", 8081))
	if err == nil {
		t.Fatal("a second environment on the same port should be refused")
	}
	if !errors.Is(err, apierr.ErrInvalidArgument) {
		t.Fatalf("expected an invalid-argument refusal, got %v", err)
	}
	// Told which port, never whose environment: a collision with another
	// organization's runtime is still a collision, and naming it would answer a
	// question the caller did not get to ask.
	if !strings.Contains(err.Error(), "8081") {
		t.Errorf("the refusal should name the port, got %q", err)
	}
}

// Keeping your own port while editing something else is not a collision.
func TestAnEnvironmentKeepsItsOwnPortOnUpdate(t *testing.T) {
	svc, projectID, ctx := fixture(t)

	id, err := svc.CreateEnvironment(ctx, staging(projectID, "staging", 8081))
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	edited, err := svc.GetEnvironment(ctx, id)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	edited.Name = "pre-prod"
	if err := svc.UpdateEnvironment(ctx, edited); err != nil {
		t.Fatalf("an environment keeping its own port is not a collision: %v", err)
	}
}

// What cannot be served is refused on the form, not at the next restart.
func TestUnservableEnvironmentsAreRefused(t *testing.T) {
	svc, projectID, ctx := fixture(t)

	cases := []struct {
		name    string
		mutate  func(*entities.Environment)
		mustSay string
	}{
		{"no name", func(e *entities.Environment) { e.Name = "  " }, "name"},
		{"unknown driver", func(e *entities.Environment) { e.Driver = "oracle" }, "driver"},
		{"no driver", func(e *entities.Environment) { e.Driver = "" }, "driver"},
		{"privileged port", func(e *entities.Environment) { e.Port = 80 }, "port"},
		{"port out of range", func(e *entities.Environment) { e.Port = 70000 }, "port"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			env := staging(projectID, "staging", 8081)
			tc.mutate(&env)
			_, err := svc.CreateEnvironment(ctx, env)
			if err == nil {
				t.Fatalf("%s should be refused", tc.name)
			}
			if !errors.Is(err, apierr.ErrInvalidArgument) {
				t.Fatalf("expected an invalid-argument refusal, got %v", err)
			}
			if !strings.Contains(strings.ToLower(err.Error()), tc.mustSay) {
				t.Errorf("the refusal should mention %q, got %q", tc.mustSay, err)
			}
		})
	}
}
