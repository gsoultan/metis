package pagination_test

import (
	"fmt"
	"testing"

	"github.com/google/uuid"
	"github.com/gsoultan/gobpm/server/repositories"
	"github.com/gsoultan/gobpm/server/repositories/models"
	"github.com/gsoultan/gobpm/tests/testutils"
)

// ScanWithGraphs is batched, because a definition graph is a whole BPMN
// document and every version of every process is kept forever. Batching is
// also where a scan quietly loses rows: an off-by-one at the boundary, or a
// reused buffer read after the next batch has overwritten it.
//
// That matters more here than it usually would. The only caller is the
// javascript-conditions worklist, and a worklist that silently stops at the
// first batch tells an operator the migration is finished when it is not —
// which is exactly the assurance they would turn the flag off on.

// seedDefinitionsWithFlows seeds n definitions, each carrying a flow condition,
// so the scan has a graph to lose rather than an empty skeleton.
func seedDefinitionsWithFlows(t *testing.T, repo repositories.Repository, projectID uuid.UUID, n int) {
	t.Helper()
	for i := range n {
		if err := repo.Definition().Create(t.Context(), models.ProcessDefinitionModel{
			Base:      models.Base{ID: models.UUID(uuid.Must(uuid.NewV7()))},
			ProjectID: models.UUID(projectID),
			Key:       fmt.Sprintf("scanned-%04d", i),
			Name:      fmt.Sprintf("Scanned %d", i),
			Version:   1,
			Flows: []models.SequenceFlow{
				{ID: fmt.Sprintf("flow-%04d", i), SourceRef: "a", TargetRef: "b", Condition: "js:amount > 100"},
			},
		}); err != nil {
			t.Fatalf("seed definition %d: %v", i, err)
		}
	}
}

// TestScanWithGraphs_CrossesBatchBoundariesWithoutLosingRows seeds more than
// the internal batch size so the scan must page at least three times.
func TestScanWithGraphs_CrossesBatchBoundariesWithoutLosingRows(t *testing.T) {
	repo := repositories.NewRepository(testutils.SetupTestDB(t))
	projectID := uuid.Must(uuid.NewV7())

	const seeded = 451 // > 2 × the 200-row batch size, so the tail is a partial batch
	seedDefinitionsWithFlows(t, repo, projectID, seeded)

	seen := make(map[string]int)
	batches := 0
	err := repo.Definition().ScanWithGraphs(t.Context(), func(batch []models.ProcessDefinitionModel) error {
		batches++
		for _, m := range batch {
			seen[m.Key]++
			// The graph must arrive hydrated; List projects these columns away,
			// and a scan that returned skeletons would report an empty worklist
			// over definitions that are full of js: conditions.
			if len(m.Flows) != 1 || m.Flows[0].Condition != "js:amount > 100" {
				t.Fatalf("definition %s came back without its graph: %+v", m.Key, m.Flows)
			}
		}
		return nil
	})
	if err != nil {
		t.Fatalf("scan: %v", err)
	}

	if len(seen) != seeded {
		t.Fatalf("scan saw %d distinct definitions, want %d — the batched scan is losing rows", len(seen), seeded)
	}
	for key, count := range seen {
		if count != 1 {
			t.Fatalf("definition %s was visited %d times; a batched scan must not repeat rows", key, count)
		}
	}
	if batches < 2 {
		t.Fatalf("the scan completed in %d batch(es); this test is meant to cross a boundary and no longer does", batches)
	}
}

// TestScanWithGraphs_StopsWhenTheVisitorFails pins that an error from the
// callback ends the scan instead of being swallowed and reported as a
// complete, and therefore empty, result.
func TestScanWithGraphs_StopsWhenTheVisitorFails(t *testing.T) {
	repo := repositories.NewRepository(testutils.SetupTestDB(t))
	projectID := uuid.Must(uuid.NewV7())
	seedDefinitionsWithFlows(t, repo, projectID, 250)

	err := repo.Definition().ScanWithGraphs(t.Context(), func([]models.ProcessDefinitionModel) error {
		return fmt.Errorf("the caller gave up")
	})
	if err == nil {
		t.Fatal("a failing visitor reported a successful scan; the caller would read a truncated worklist as complete")
	}
}
