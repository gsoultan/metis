package postgres_test

import (
	"testing"

	"github.com/google/uuid"
	"github.com/gsoultan/metis/server/repositories"
	"github.com/gsoultan/metis/server/repositories/contracts"
	"github.com/gsoultan/metis/server/repositories/models"
	"github.com/gsoultan/metis/tests/testutils"
)

// A page read with the total the status counts gave is the page read by
// counting: the same rows in the same order, and the same total. Past the
// last instance it is empty without reading anything. Another organization's
// project shows nothing, whatever total it is handed.
func TestAPageOfAKnownTotalIsThePageThatCountsIt(t *testing.T) {
	db := testutils.SetupTestDB(t)
	repo := repositories.NewRepository(testutils.StormConn(db))
	ours, _, project := testutils.ScopedProject(t, repo)
	theirs, _, _ := testutils.ScopedProject(t, repo)
	definition := seedDefinition(t, db, project, 1)
	statuses := []models.ProcessStatus{models.ProcessActive, models.ProcessCompleted, models.ProcessSuspended}
	for i := range 12 {
		if _, err := repo.Process().Create(ours, models.ProcessInstanceModel{
			Base: models.Base{ID: models.FromUUID(uuid.Must(uuid.NewV7()))}, ProjectID: models.FromUUID(project),
			DefinitionID: models.FromUUID(definition), Status: statuses[i%len(statuses)],
		}); err != nil {
			t.Fatalf("seed an instance: %v", err)
		}
	}

	for _, f := range []contracts.InstanceFilter{{}, {Status: models.ProcessActive}, {DefinitionID: definition}} {
		counts, err := repo.Process().CountByStatuses(ours, project, contracts.InstanceFilter{DefinitionID: f.DefinitionID})
		if err != nil {
			t.Fatalf("count: %v", err)
		}
		total := counts[f.Status]
		if f.Status == "" {
			total = 0
			for _, n := range counts {
				total += n
			}
		}
		for page := 1; page <= 3; page++ {
			p := contracts.Pagination{Page: page, PageSize: 5}
			counted, err := repo.Process().ListByProjectPaged(ours, project, f, p)
			if err != nil {
				t.Fatalf("page by counting: %v", err)
			}
			known, err := repo.Process().ListPageOfTotal(ours, project, f, p, total)
			if err != nil {
				t.Fatalf("page of a known total: %v", err)
			}
			if known.Total != counted.Total || len(known.Items) != len(counted.Items) {
				t.Fatalf("%+v page %d: %d of %d, want %d of %d", f, page, len(known.Items), known.Total, len(counted.Items), counted.Total)
			}
			for i := range known.Items {
				if known.Items[i].ID != counted.Items[i].ID {
					t.Errorf("%+v page %d row %d: %v, want %v", f, page, i, known.Items[i].ID, counted.Items[i].ID)
				}
			}
		}
	}

	if got, err := repo.Process().ListPageOfTotal(theirs, project, contracts.InstanceFilter{}, contracts.Pagination{Page: 1, PageSize: 5}, 12); err != nil || len(got.Items) != 0 || got.Total != 0 {
		t.Errorf("another organization read %d of %d (%v), want nothing", len(got.Items), got.Total, err)
	}
}
