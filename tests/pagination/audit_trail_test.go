package pagination_test

import (
	"context"
	"fmt"
	"testing"

	"github.com/google/uuid"
	"github.com/gsoultan/metis/server/domains/entities"
	observersimpl "github.com/gsoultan/metis/server/domains/observers/impl"
	"github.com/gsoultan/metis/server/domains/services/impl"
	"github.com/gsoultan/metis/server/repositories"
	"github.com/gsoultan/metis/server/repositories/models"
	"github.com/gsoultan/metis/tests/testutils"
)

// longTrail is past one page of the timeline, so a read that returns
// everything and one that returns a page are told apart.
const longTrail = impl.AuditPageMax + 50

// loopingSteps are the nodes the seeded instance goes round, in the order it
// first reaches them.
var loopingSteps = []string{"start", "review", "pay"}

// seedLongTrail is an instance that has gone round loopingSteps for longTrail
// entries, each entry naming its place in the trail.
func seedLongTrail(t *testing.T) (context.Context, repositories.Repository, uuid.UUID) {
	t.Helper()
	db := testutils.SetupTestDB(t)
	repo := repositories.NewRepository(testutils.StormConn(db))
	ctx, _, projectID := testutils.ScopedProject(t, repo)

	definitionID := uuid.Must(uuid.NewV7())
	seedDefinition(t, db, projectID, definitionID, "looping", 1)
	instanceID := uuid.Must(uuid.NewV7())
	if _, err := repo.Process().Create(ctx, models.ProcessInstanceModel{
		Base:         models.Base{ID: models.FromUUID(instanceID)},
		ProjectID:    models.FromUUID(projectID),
		DefinitionID: models.FromUUID(definitionID),
		Status:       models.ProcessActive,
	}); err != nil {
		t.Fatalf("seed the instance: %v", err)
	}
	for n := range longTrail {
		if err := repo.Audit().Create(ctx, models.AuditModel{
			ProjectID:  models.FromUUID(projectID),
			InstanceID: models.FromUUID(instanceID),
			Type:       entities.EventNodeReached,
			NodeID:     loopingSteps[n%len(loopingSteps)],
			Message:    fmt.Sprintf("entry %d", n),
		}); err != nil {
			t.Fatalf("seed audit entry %d: %v", n, err)
		}
	}
	return ctx, repo, instanceID
}

// The timeline reads a page of an instance's trail — its newest entries, in
// the order they were written — and how long the trail is, rather than every
// entry the instance has ever written. The next page back is reached by
// offset, and asking for more than a page is given a page.
func TestTheTimelineReadsTheNewestPageOfALongTrailAndHowLongItIs(t *testing.T) {
	ctx, repo, instanceID := seedLongTrail(t)
	engine := impl.NewExecutionEngine(repo, observersimpl.NewEventDispatcher())

	newest, total, err := engine.GetLatestAuditLogs(ctx, instanceID, 0, 0)
	if err != nil {
		t.Fatalf("read the newest page: %v", err)
	}
	if total != longTrail {
		t.Fatalf("total = %d; want the %d entries the trail holds", total, longTrail)
	}
	if len(newest) != impl.AuditPageMax {
		t.Fatalf("a read with no limit returned %d entries; want one page of %d", len(newest), impl.AuditPageMax)
	}
	if first, last := newest[0].Message, newest[len(newest)-1].Message; first != "entry 50" || last != fmt.Sprintf("entry %d", longTrail-1) {
		t.Fatalf("the page runs from %q to %q; want the newest %d, oldest first: entry 50 to entry %d",
			first, last, impl.AuditPageMax, longTrail-1)
	}

	older, _, err := engine.GetLatestAuditLogs(ctx, instanceID, impl.AuditPageMax, impl.AuditPageMax)
	if err != nil {
		t.Fatalf("read the page before it: %v", err)
	}
	if len(older) != 50 || older[0].Message != "entry 0" || older[49].Message != "entry 49" {
		t.Fatalf("the page before the newest holds %d entries; want the oldest 50, entry 0 to entry 49", len(older))
	}

	widened, _, err := engine.GetLatestAuditLogs(ctx, instanceID, 1_000_000, 0)
	if err != nil {
		t.Fatalf("read with a large limit: %v", err)
	}
	if len(widened) != impl.AuditPageMax {
		t.Fatalf("asking for a million entries returned %d; want the page maximum of %d", len(widened), impl.AuditPageMax)
	}
}

// The execution path is counted by the database rather than from the whole
// trail, and still says what the trail says: every node the instance reached,
// in the order it first reached them, and how often.
func TestTheExecutionPathOfALongTrailCountsEveryVisit(t *testing.T) {
	ctx, repo, instanceID := seedLongTrail(t)
	engine := impl.NewExecutionEngine(repo, observersimpl.NewEventDispatcher())

	path, err := engine.GetExecutionPath(ctx, instanceID)
	if err != nil {
		t.Fatalf("read the execution path: %v", err)
	}
	if len(path.Nodes) != len(loopingSteps) {
		t.Fatalf("the path has %d nodes; want %d", len(path.Nodes), len(loopingSteps))
	}
	for i, node := range path.Nodes {
		if node.ID != loopingSteps[i] {
			t.Fatalf("node %d of the path is %q; want %q, the order they were first reached in", i, node.ID, loopingSteps[i])
		}
	}
	total := 0
	for _, step := range loopingSteps {
		total += path.Frequencies[step]
	}
	if total != longTrail || path.Frequencies["start"] != (longTrail+2)/3 {
		t.Fatalf("frequencies %v add up to %d; want every one of the %d visits counted", path.Frequencies, total, longTrail)
	}

	// Another organization asking about the instance learns nothing.
	elsewhere, _ := testutils.ScopedContext(t, repo)
	foreign, err := engine.GetExecutionPath(elsewhere, instanceID)
	if err != nil {
		t.Fatalf("read the path from another organization: %v", err)
	}
	if len(foreign.Nodes) != 0 {
		t.Fatalf("another organization read %d nodes of the path; want none", len(foreign.Nodes))
	}
}
