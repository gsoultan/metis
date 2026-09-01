package pagination_test

import (
	"fmt"
	"testing"

	"github.com/google/uuid"
	"github.com/gsoultan/metis/server/domains/entities"
	"github.com/gsoultan/metis/server/repositories"
	"github.com/gsoultan/metis/server/repositories/contracts"
	"github.com/gsoultan/metis/server/repositories/models"
	"github.com/gsoultan/metis/tests/testutils"
)

// Definitions and decisions returned every row.
//
// Tasks and instances were paged because they grow with use; these grow with
// modelling, which is slower but does not stop. A project that has been worked
// on for a year has every version of every process in that list, and the list
// exists to pick one from.

func seedDefinitions(t *testing.T, repo repositories.Repository, projectID uuid.UUID, n int) {
	t.Helper()
	for i := range n {
		if err := repo.Definition().Create(t.Context(), models.ProcessDefinitionModel{
			Base:      models.Base{ID: models.UUID(uuid.Must(uuid.NewV7()))},
			ProjectID: models.UUID(projectID),
			Key:       fmt.Sprintf("process-%03d", i),
			Name:      fmt.Sprintf("Process %d", i),
			Version:   1,
		}); err != nil {
			t.Fatalf("seed definition %d: %v", i, err)
		}
	}
}

func seedDecisions(t *testing.T, repo repositories.Repository, projectID uuid.UUID, n int) {
	t.Helper()
	for i := range n {
		if err := repo.Decision().Create(t.Context(), models.DecisionDefinitionModel{
			Base:      models.Base{ID: models.UUID(uuid.Must(uuid.NewV7()))},
			ProjectID: models.UUID(projectID),
			Key:       fmt.Sprintf("decision-%03d", i),
			Name:      fmt.Sprintf("Decision %d", i),
			Version:   1,
			HitPolicy: "FIRST",
		}); err != nil {
			t.Fatalf("seed decision %d: %v", i, err)
		}
	}
}

func TestDefinitionsPaged_LimitsTheWindowAndCountsTheWhole(t *testing.T) {
	repo := repositories.NewRepository(testutils.SetupTestDB(t))
	projectID := uuid.Must(uuid.NewV7())
	seedDefinitions(t, repo, projectID, 137)

	page, err := repo.Definition().ListByProjectPaged(t.Context(), projectID, contracts.Pagination{Page: 1, PageSize: 50})
	if err != nil {
		t.Fatalf("page 1: %v", err)
	}
	if len(page.Items) != 50 {
		t.Fatalf("page 1 returned %d rows, want 50 — the window is not being limited", len(page.Items))
	}
	if page.Total != 137 {
		t.Fatalf("Total = %d, want 137 — the count is being limited along with the page", page.Total)
	}
	if !page.HasMore() {
		t.Error("HasMore() = false on page 1 of 3")
	}
}

func TestDefinitionsPaged_PagesDoNotOverlap(t *testing.T) {
	repo := repositories.NewRepository(testutils.SetupTestDB(t))
	projectID := uuid.Must(uuid.NewV7())
	seedDefinitions(t, repo, projectID, 30)

	seen := map[uuid.UUID]int{}
	for n := 1; n <= 3; n++ {
		page, err := repo.Definition().ListByProjectPaged(t.Context(), projectID, contracts.Pagination{Page: n, PageSize: 10})
		if err != nil {
			t.Fatalf("page %d: %v", n, err)
		}
		for _, row := range page.Items {
			seen[uuid.UUID(row.ID)]++
		}
	}
	if len(seen) != 30 {
		t.Fatalf("saw %d distinct rows across three pages, want 30", len(seen))
	}
	for id, count := range seen {
		if count != 1 {
			t.Fatalf("row %s appeared %d times — pages overlap, so the ordering is not stable", id, count)
		}
	}
}

func TestDecisionsPaged_LimitsTheWindowAndCountsTheWhole(t *testing.T) {
	repo := repositories.NewRepository(testutils.SetupTestDB(t))
	projectID := uuid.Must(uuid.NewV7())
	seedDecisions(t, repo, projectID, 137)

	page, err := repo.Decision().ListByProjectPaged(t.Context(), projectID, contracts.Pagination{Page: 3, PageSize: 50})
	if err != nil {
		t.Fatalf("page 3: %v", err)
	}
	if len(page.Items) != 37 {
		t.Fatalf("last page returned %d rows, want 37", len(page.Items))
	}
	if page.Total != 137 {
		t.Fatalf("Total = %d, want 137", page.Total)
	}
	if page.HasMore() {
		t.Error("HasMore() = true on the final page")
	}
}

// Tenant scoping joins the projects table, which carries created_at too, so a
// bare "created_at DESC" becomes ambiguous the moment scoping is active — which
// it is for every request-driven call. The task paging shipped with exactly
// that fault and failed every scoped request with
// "SQL logic error: ambiguous column name: created_at".
func TestDefinitionsPaged_WorksUnderTenantScoping(t *testing.T) {
	repo := repositories.NewRepository(testutils.SetupTestDB(t))
	ctx := t.Context()

	orgID := uuid.Must(uuid.NewV7())
	if err := repo.Organization().Create(ctx, models.OrganizationModel{
		Base: models.Base{ID: models.UUID(orgID)},
		Name: "Acme",
	}); err != nil {
		t.Fatalf("seed organization: %v", err)
	}
	projectID := uuid.Must(uuid.NewV7())
	if err := repo.Project().Create(ctx, models.ProjectModel{
		Base:           models.Base{ID: models.UUID(projectID)},
		OrganizationID: models.UUID(orgID),
		Name:           "Acme Project",
	}); err != nil {
		t.Fatalf("seed project: %v", err)
	}
	seedDefinitions(t, repo, projectID, 12)
	seedDecisions(t, repo, projectID, 12)

	scoped := entities.WithTenantContext(ctx, entities.TenantContext{TenantID: orgID.String()})

	defs, err := repo.Definition().ListByProjectPaged(scoped, projectID, contracts.Pagination{Page: 1, PageSize: 5})
	if err != nil {
		t.Fatalf("definitions under tenant scoping: %v", err)
	}
	if len(defs.Items) != 5 || defs.Total != 12 {
		t.Errorf("definitions: %d rows of %d, want 5 of 12", len(defs.Items), defs.Total)
	}

	decisions, err := repo.Decision().ListByProjectPaged(scoped, projectID, contracts.Pagination{Page: 1, PageSize: 5})
	if err != nil {
		t.Fatalf("decisions under tenant scoping: %v", err)
	}
	if len(decisions.Items) != 5 || decisions.Total != 12 {
		t.Errorf("decisions: %d rows of %d, want 5 of 12", len(decisions.Items), decisions.Total)
	}
}

func TestDefinitionsPaged_EmptyResultIsAnEmptyPageNotNil(t *testing.T) {
	repo := repositories.NewRepository(testutils.SetupTestDB(t))

	page, err := repo.Definition().ListByProjectPaged(t.Context(), uuid.Must(uuid.NewV7()), contracts.Pagination{Page: 1, PageSize: 10})
	if err != nil {
		t.Fatalf("empty page: %v", err)
	}
	if page.Items == nil {
		t.Fatal("Items is nil; an empty result must serialise as [] not null")
	}
	if page.Total != 0 || page.HasMore() {
		t.Fatalf("empty page reported Total=%d HasMore=%v", page.Total, page.HasMore())
	}
}
