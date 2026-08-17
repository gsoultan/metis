package postgres_test

import (
	"sync"
	"testing"

	"github.com/google/uuid"
	"github.com/gsoultan/gobpm/server/domains/entities"
	serviceimpl "github.com/gsoultan/gobpm/server/domains/services/impl"
	"github.com/gsoultan/gobpm/tests/testutils"
)

// racers is how many deployments of the same key are launched at once.
//
// Eight is enough that a read-then-write allocator loses on essentially every
// run, and small enough to stay inside the connection pool this test opens.
const racers = 8

// Deploying the same key concurrently must produce distinct versions.
//
// The allocator reads the highest version and adds one. Held behind a barrier,
// every one of these goroutines reads the same number, and before the unique
// index on (project_id, key, version) existed every one of them then wrote it:
// eight rows, all claiming to be version 1, and a caller asking to start
// "version 1" got whichever the planner happened to return.
//
// SQLite cannot show this — the suite runs it on one connection, which
// serialises the reads and hides the race — which is why the test lives here.
func TestConcurrentDeploysGetDistinctVersions(t *testing.T) {
	db := testutils.SetupPostgresDB(t, racers+2)
	ctx := t.Context()
	repo, _, projID := newPostgresEngine(t, db)

	defSvc := serviceimpl.NewDefinitionService(repo)

	start := make(chan struct{})
	var wg sync.WaitGroup
	ids := make([]uuid.UUID, racers)
	errs := make([]error, racers)

	for i := range racers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			ids[i], errs[i] = defSvc.CreateDefinition(ctx, paymentDefinition(projID, "concurrent-deploy"))
		}()
	}
	close(start)
	wg.Wait()

	for i, err := range errs {
		if err != nil {
			t.Fatalf("deploy %d failed: %v", i, err)
		}
	}

	seen := map[int]uuid.UUID{}
	for _, id := range ids {
		def, err := defSvc.GetDefinition(ctx, id)
		if err != nil {
			t.Fatalf("load definition %s: %v", id, err)
		}
		if other, taken := seen[def.Version]; taken {
			t.Fatalf("definitions %s and %s both claim version %d", other, id, def.Version)
		}
		seen[def.Version] = id
	}

	// Distinct is the property that matters, but a gap would mean a version
	// number was allocated and thrown away, and the next deploy would leave a
	// hole in a series operators read as a history.
	for version := 1; version <= racers; version++ {
		if _, ok := seen[version]; !ok {
			t.Errorf("no definition claims version %d; got %v", version, seen)
		}
	}
}

// The same race on decisions, which allocate versions the same way and had the
// same defect.
func TestConcurrentDecisionDeploysGetDistinctVersions(t *testing.T) {
	db := testutils.SetupPostgresDB(t, racers+2)
	ctx := t.Context()
	repo, _, projID := newPostgresEngine(t, db)

	decisionSvc := serviceimpl.NewDecisionService(repo, serviceimpl.NewDecisionTableEvaluator(serviceimpl.NewFEELEvaluator()))

	start := make(chan struct{})
	var wg sync.WaitGroup
	ids := make([]uuid.UUID, racers)
	errs := make([]error, racers)

	for i := range racers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			ids[i], errs[i] = decisionSvc.CreateDecision(ctx, entities.DecisionDefinition{
				Project:   &entities.Project{ID: projID},
				Key:       "concurrent-decision",
				Name:      "Concurrent decision",
				HitPolicy: entities.HitPolicyFirst,
				Inputs:    []entities.DecisionInput{{ID: "in1", Expression: "amount", Type: "number"}},
				Outputs:   []entities.DecisionOutput{{ID: "out1", Name: "band", Type: "string"}},
				Rules:     []entities.DecisionRule{{Inputs: []string{"> 10"}, Outputs: []any{"HIGH"}}},
			})
		}()
	}
	close(start)
	wg.Wait()

	for i, err := range errs {
		if err != nil {
			t.Fatalf("deploy %d failed: %v", i, err)
		}
	}

	seen := map[int]uuid.UUID{}
	for _, id := range ids {
		decision, err := decisionSvc.GetDecision(ctx, id)
		if err != nil {
			t.Fatalf("load decision %s: %v", id, err)
		}
		if other, taken := seen[decision.Version]; taken {
			t.Fatalf("decisions %s and %s both claim version %d", other, id, decision.Version)
		}
		seen[decision.Version] = id
	}
	if len(seen) != racers {
		t.Errorf("got %d distinct versions from %d deploys: %v", len(seen), racers, seen)
	}
}
