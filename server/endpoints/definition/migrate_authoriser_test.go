package definition

import (
	"context"
	"testing"

	"github.com/google/uuid"
	pkgauth "github.com/gsoultan/metis/internal/pkg/auth"
	"github.com/gsoultan/metis/server/domains/entities"
	"github.com/gsoultan/metis/server/domains/services"
	servicecontracts "github.com/gsoultan/metis/server/domains/services/contracts"
)

// askedOfTheService is a facade that only plans and migrates, and keeps the
// options it was asked with. Anything else the endpoint called would panic.
type askedOfTheService struct {
	services.ServiceFacade
	plans, applies int
	actor          string
}

func (s *askedOfTheService) PlanInstanceMigration(_ context.Context, _, _ uuid.UUID, _ map[string]string, opts ...servicecontracts.MigrationOption) (entities.MigrationPlan, error) {
	s.plans++
	s.actor = servicecontracts.ApplyMigrationOptions(opts).Actor
	return entities.MigrationPlan{}, nil
}

func (s *askedOfTheService) MigrateInstances(_ context.Context, _, _ uuid.UUID, _ map[string]string, opts ...servicecontracts.MigrationOption) error {
	s.applies++
	s.actor = servicecontracts.ApplyMigrationOptions(opts).Actor
	return nil
}

func signedInAs(username string) context.Context {
	return context.WithValue(context.Background(), pkgauth.UserContextKey,
		entities.User{ID: uuid.Must(uuid.NewV7()), Username: username, Roles: []string{entities.RoleAdmin}})
}

// TestAMigrationNamesItsAuthoriserOrIsRefused.
//
// Root cause: the endpoint took the caller's name only when asking for it
// succeeded, and asking fails the same way for nobody signed in and for an
// account with no username. The second was passed on as the first, and the
// service wrote "System" for a decision a person made.
func TestAMigrationNamesItsAuthoriserOrIsRefused(t *testing.T) {
	t.Parallel()
	apply := false
	request := MigrateInstancesRequest{
		SourceDefinitionID: uuid.NewString(), TargetDefinitionID: uuid.NewString(), DryRun: &apply,
	}
	cases := []struct {
		name      string
		ctx       context.Context
		refused   bool
		wantActor string
	}{
		{"nobody signed in is the server, and the service names it", context.Background(), false, ""},
		{"a signed-in account is named", signedInAs("dita"), false, "dita"},
		{"a signed-in account with no username", signedInAs(""), true, ""},
		{"a signed-in account whose username is only spaces", signedInAs("   "), true, ""},
	}
	for _, c := range cases {
		svc := &askedOfTheService{}
		reply, err := MakeMigrateInstancesEndpoint(svc)(c.ctx, request)
		if err != nil {
			t.Fatalf("%s: %v", c.name, err)
		}
		response, ok := reply.(MigrateInstancesResponse)
		if !ok {
			t.Fatalf("%s: the reply is a %T", c.name, reply)
		}
		if c.refused {
			if response.Err == nil || response.Applied || svc.plans+svc.applies != 0 {
				t.Errorf("%s: err %v, applied %v, the service was asked %d time(s); want a refusal before anything is planned",
					c.name, response.Err, response.Applied, svc.plans+svc.applies)
			}
			continue
		}
		if response.Err != nil || !response.Applied || svc.applies != 1 || svc.actor != c.wantActor {
			t.Errorf("%s: err %v, applied %v, %d apply, actor %q; want it applied once by %q",
				c.name, response.Err, response.Applied, svc.applies, svc.actor, c.wantActor)
		}
	}
}

// The refusal is for a preview too: a dry run shows what the apply would
// answer, not a friendlier set.
func TestADryRunByAnAccountWithNoNameIsRefusedToo(t *testing.T) {
	t.Parallel()
	svc := &askedOfTheService{}
	reply, err := MakeMigrateInstancesEndpoint(svc)(signedInAs(""), MigrateInstancesRequest{
		SourceDefinitionID: uuid.NewString(), TargetDefinitionID: uuid.NewString(),
	})
	if err != nil {
		t.Fatalf("migrate: %v", err)
	}
	if response := reply.(MigrateInstancesResponse); response.Err == nil || svc.plans != 0 {
		t.Fatalf("a preview by an account with no name: err %v, planned %d time(s); want it refused", response.Err, svc.plans)
	}
}
