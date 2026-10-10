package pagination_test

import (
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/gsoultan/metis/server/repositories"
	"github.com/gsoultan/metis/server/repositories/models"
	"github.com/gsoultan/metis/tests/testutils"
)

// An instance's variable history is every snapshot it has, oldest first,
// however many there are.
//
// The read used to take the store's default thousand rows, oldest first, so an
// instance that had looped past a thousand steps answered "what were the
// variables when it decided" with history that stopped a thousand steps in —
// the newest snapshots, the ones somebody looks at, were the ones dropped.
func TestAVariableHistoryPastAThousandSnapshotsEndsWithTheNewest(t *testing.T) {
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

	// Pairs share a captured_at, the way two snapshots taken in one step can,
	// so the walk has ties to get past as well as a thousand rows.
	const taken = 1050
	start := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	for n := range taken {
		if _, err := repo.VariableSnapshot().Create(ctx, models.VariableSnapshotModel{
			InstanceID: models.FromUUID(instanceID),
			NodeID:     "loop",
			Variables:  map[string]any{"n": n},
			CapturedAt: start.Add(time.Duration(n/2) * time.Second),
		}); err != nil {
			t.Fatalf("seed snapshot %d: %v", n, err)
		}
	}

	history, err := repo.VariableSnapshot().ListByInstance(ctx, instanceID)
	if err != nil {
		t.Fatalf("read the history: %v", err)
	}
	if len(history) != taken {
		t.Fatalf("the history holds %d of the instance's %d snapshots; want every one", len(history), taken)
	}
	newest := start.Add(time.Duration((taken-1)/2) * time.Second)
	if last := history[len(history)-1].CapturedAt; !last.Equal(newest) {
		t.Fatalf("the history ends at %s; want the newest snapshot, at %s", last, newest)
	}
	for i := 1; i < len(history); i++ {
		if history[i].CapturedAt.Before(history[i-1].CapturedAt) {
			t.Fatalf("snapshot %d, at %s, comes after one at %s; want oldest first",
				i, history[i].CapturedAt, history[i-1].CapturedAt)
		}
	}
	seen := make(map[models.UUID]bool, len(history))
	for _, s := range history {
		if seen[s.ID] {
			t.Fatalf("snapshot %s appears twice in the history", s.ID)
		}
		seen[s.ID] = true
	}
}
