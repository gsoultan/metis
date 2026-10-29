package postgres_test

import (
	"reflect"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/gsoultan/metis/server/domains/entities"
	"github.com/gsoultan/metis/server/repositories"
	"github.com/gsoultan/metis/server/repositories/contracts"
	"github.com/gsoultan/metis/server/repositories/models"
	"github.com/gsoultan/metis/server/repositories/store/auditentry"
	"github.com/gsoultan/metis/tests/testutils"
)

// An instance's node visits are counted per node and listed in the order the
// trail first reached them: by created_at, and within one moment by seq with
// the unnumbered entries first. Entries of another type, another instance, no
// node, or marked removed are not visits.
func TestNodeVisitsAreInTheOrderTheTrailFirstReachedThem(t *testing.T) {
	db := testutils.SetupTestDB(t)
	repo := repositories.NewRepository(db)
	ctx, _, project := testutils.ScopedProject(t, repo)
	definition := seedDefinition(t, db, project, 1)
	instance := func() uuid.UUID {
		t.Helper()
		id := uuid.Must(uuid.NewV7())
		if _, err := repo.Process().Create(ctx, models.ProcessInstanceModel{
			Base: models.Base{ID: models.FromUUID(id)}, ProjectID: models.FromUUID(project),
			DefinitionID: models.FromUUID(definition), Status: models.ProcessActive,
		}); err != nil {
			t.Fatalf("seed an instance: %v", err)
		}
		return id
	}
	mine, other := instance(), instance()

	at := time.Date(2026, 3, 1, 9, 0, 0, 0, time.UTC)
	entry := func(inst uuid.UUID, kind string, node any, seq any, when time.Time, removed bool) {
		t.Helper()
		row := auditentry.Create()
		row.SetCreatedAt(when)
		row.SetUpdatedAt(when)
		row.SetProjectID(project)
		row.SetInstanceID(inst)
		row.SetType(kind)
		row.SetMessage("test")
		row.SetData(nil)
		if node, ok := node.(string); ok {
			row.SetNodeID(node)
		}
		// An entry written before entries were numbered reads as 0.
		if seq, ok := seq.(int); ok {
			row.SetSeq(int64(seq))
		}
		if removed {
			row.SetDeletedAt(when)
		}
		if _, err := row.Insert(t.Context(), testutils.Executor(db)); err != nil {
			t.Fatalf("seed an entry: %v", err)
		}
	}
	reached := entities.EventNodeReached
	// One moment holds three first visits: the unnumbered one first, then by seq.
	entry(mine, reached, "task", 10, at, false)
	entry(mine, reached, "gate", 5, at, false)
	entry(mine, reached, "start", nil, at, false)
	// A later visit with a smaller seq does not move task ahead of gate.
	entry(mine, reached, "task", 2, at.Add(time.Second), false)
	entry(mine, reached, "loop", 1, at.Add(2*time.Second), false)
	entry(mine, reached, "loop", 3, at.Add(3*time.Second), false)
	// None of these is a visit.
	entry(mine, reached, "", 20, at.Add(4*time.Second), false)
	entry(mine, reached, nil, 21, at.Add(4*time.Second), false)
	entry(mine, reached, "removed", 22, at.Add(4*time.Second), true)
	entry(mine, "NodeCompleted", "completed", 23, at.Add(4*time.Second), false)
	entry(other, reached, "elsewhere", 0, at.Add(-time.Second), false)

	got, err := repo.Audit().NodeVisits(ctx, mine, reached)
	if err != nil {
		t.Fatalf("node visits: %v", err)
	}
	want := []contracts.NodeVisit{
		{NodeID: "start", Visits: 1}, {NodeID: "gate", Visits: 1},
		{NodeID: "task", Visits: 2}, {NodeID: "loop", Visits: 2},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("node visits = %+v, want %+v", got, want)
	}
}
