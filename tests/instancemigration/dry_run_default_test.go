package instancemigration

import (
	"encoding/json"
	"testing"

	observersimpl "github.com/gsoultan/metis/server/domains/observers/impl"
	"github.com/gsoultan/metis/server/domains/services"
	"github.com/gsoultan/metis/server/endpoints/definition"
	"github.com/gsoultan/metis/server/repositories"
	"github.com/gsoultan/metis/tests/testutils"
)

// The migration endpoint rewrites running instances — somebody's purchase
// order — and its documentation says a request that does not ask to commit is
// a dry run. The flag was a plain bool, so a request that left it out was an
// apply: the documented examples, which leave it out, moved every instance on
// the source version.
func TestAMigrationRequestThatDoesNotSayDryRunIsOne(t *testing.T) {
	db := testutils.SetupTestDB(t)
	repo := repositories.NewRepository(testutils.StormConn(db))
	svc := services.NewServiceFacade(repo, observersimpl.NewEventDispatcher(), observersimpl.NewSSEObserver(),
		"dry-run-default-test", nil, nil, nil)
	ctx, _, projectID := testutils.ScopedProject(t, repo)

	v1, err := svc.CreateDefinition(ctx, waitThenWork(projectID, "wait", "work"))
	if err != nil {
		t.Fatalf("deploy v1: %v", err)
	}
	instanceID, err := svc.StartProcess(ctx, projectID, "cooling-off", nil)
	if err != nil {
		t.Fatalf("start: %v", err)
	}
	v2, err := svc.CreateDefinition(ctx, waitThenWork(projectID, "wait", "work"))
	if err != nil {
		t.Fatalf("deploy v2: %v", err)
	}

	// As a client following the docs sends it: no dry_run at all.
	var request definition.MigrateInstancesRequest
	if err := json.Unmarshal([]byte(`{"source_definition_id":"`+v1.String()+`","target_definition_id":"`+v2.String()+`"}`), &request); err != nil {
		t.Fatalf("decode: %v", err)
	}
	reply, err := definition.MakeMigrateInstancesEndpoint(svc)(ctx, request)
	if err != nil {
		t.Fatalf("migrate: %v", err)
	}
	if response := reply.(definition.MigrateInstancesResponse); response.Applied || response.Err != nil {
		t.Fatalf("a request that did not ask to commit was applied=%v (err %v)", response.Applied, response.Err)
	}
	instance, err := svc.GetInstance(ctx, instanceID)
	if err != nil {
		t.Fatalf("read the instance: %v", err)
	}
	if instance.Definition == nil || instance.Definition.ID != v1 {
		t.Fatalf("the instance moved to %v on a request that did not ask to commit", instance.Definition)
	}

	// Asking is still how it is done.
	if err := json.Unmarshal([]byte(`{"source_definition_id":"`+v1.String()+`","target_definition_id":"`+v2.String()+`","dry_run":false}`), &request); err != nil {
		t.Fatalf("decode: %v", err)
	}
	reply, err = definition.MakeMigrateInstancesEndpoint(svc)(ctx, request)
	if err != nil {
		t.Fatalf("migrate: %v", err)
	}
	if response := reply.(definition.MigrateInstancesResponse); !response.Applied {
		t.Fatalf("an explicit dry_run=false did not apply: %v", response.Err)
	}
}
