package pagination_test

import (
	"testing"

	"github.com/google/uuid"
	"github.com/gsoultan/metis/server/domains/adapters"
	"github.com/gsoultan/metis/server/domains/entities"
	"github.com/gsoultan/metis/server/repositories"
	"github.com/gsoultan/metis/server/repositories/contracts"
	"github.com/gsoultan/metis/server/repositories/models"
	"github.com/gsoultan/metis/tests/testutils"
)

// Every list endpoint used to return every row. That is survivable with the
// hundred tasks a demo has and untenable with the hundred thousand a real
// deployment accumulates: the database serialises them all, the API holds them
// all, and the browser downloads and renders them all.
//
// These tests pin the two properties that make paging worth having and that
// are easy to get quietly wrong: the window is actually limited, and the total
// counts the whole result set rather than the page.

func seedTasks(t *testing.T, repo repositories.Repository, projectID uuid.UUID, assignee string, n int) {
	t.Helper()
	for i := range n {
		task := entities.Task{
			ID:       uuid.Must(uuid.NewV7()),
			Project:  &entities.Project{ID: projectID},
			Name:     "Task",
			Status:   entities.TaskClaimed,
			Node:     &entities.Node{ID: "n1"},
			Assignee: &entities.User{Username: assignee},
		}
		if err := repo.Task().Create(t.Context(), adapters.TaskModelAdapter{Task: task}.ToModel()); err != nil {
			t.Fatalf("seed task %d: %v", i, err)
		}
	}
}

func TestPagination_LimitsTheWindowAndCountsTheWhole(t *testing.T) {
	db := testutils.SetupTestDB(t)
	repo := repositories.NewRepository(db)
	projectID := uuid.Must(uuid.NewV7())

	seedTasks(t, repo, projectID, "alice", 137)

	page, err := repo.Task().ListByAssigneePaged(t.Context(), "alice", contracts.Pagination{Page: 1, PageSize: 50})
	if err != nil {
		t.Fatalf("page 1: %v", err)
	}

	if len(page.Items) != 50 {
		t.Fatalf("page 1 returned %d rows, want 50 — the window is not being limited", len(page.Items))
	}
	// The total must describe the whole result set, not the page. (GORM's Count
	// ignores LIMIT on its own, so this passes even against a naive
	// implementation — it is here to pin the contract, not to catch that
	// particular mistake.)
	if page.Total != 137 {
		t.Fatalf("Total = %d, want 137 — the count is being limited along with the page", page.Total)
	}
	if !page.HasMore() {
		t.Fatal("HasMore() = false on page 1 of 3")
	}
	if got := page.TotalPages(); got != 3 {
		t.Fatalf("TotalPages() = %d, want 3", got)
	}
}

func TestPagination_LastPageIsPartialAndReportsNoMore(t *testing.T) {
	db := testutils.SetupTestDB(t)
	repo := repositories.NewRepository(db)
	projectID := uuid.Must(uuid.NewV7())

	seedTasks(t, repo, projectID, "alice", 137)

	page, err := repo.Task().ListByAssigneePaged(t.Context(), "alice", contracts.Pagination{Page: 3, PageSize: 50})
	if err != nil {
		t.Fatalf("page 3: %v", err)
	}
	if len(page.Items) != 37 {
		t.Fatalf("last page returned %d rows, want 37", len(page.Items))
	}
	if page.HasMore() {
		t.Fatal("HasMore() = true on the final page")
	}
}

func TestPagination_PagesDoNotOverlap(t *testing.T) {
	db := testutils.SetupTestDB(t)
	repo := repositories.NewRepository(db)
	projectID := uuid.Must(uuid.NewV7())

	seedTasks(t, repo, projectID, "alice", 30)

	seen := map[uuid.UUID]int{}
	for pageNum := 1; pageNum <= 3; pageNum++ {
		page, err := repo.Task().ListByAssigneePaged(t.Context(), "alice", contracts.Pagination{Page: pageNum, PageSize: 10})
		if err != nil {
			t.Fatalf("page %d: %v", pageNum, err)
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

// An unbounded pageSize from a request would reinstate exactly the problem
// paging exists to solve, on demand, from outside.
func TestPagination_ClampsHostileInput(t *testing.T) {
	cases := []struct {
		name     string
		in       contracts.Pagination
		wantPage int
		wantSize int
	}{
		{"zero values get defaults", contracts.Pagination{}, 1, contracts.DefaultPageSize},
		{"negative page", contracts.Pagination{Page: -5, PageSize: 10}, 1, 10},
		{"negative size", contracts.Pagination{Page: 2, PageSize: -1}, 2, contracts.DefaultPageSize},
		{"oversized request", contracts.Pagination{Page: 1, PageSize: 1_000_000}, 1, contracts.MaxPageSize},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := tc.in.Normalize()
			if got.Page != tc.wantPage || got.PageSize != tc.wantSize {
				t.Fatalf("Normalize() = {Page:%d PageSize:%d}, want {Page:%d PageSize:%d}",
					got.Page, got.PageSize, tc.wantPage, tc.wantSize)
			}
		})
	}
}

func TestPagination_EmptyResultIsAnEmptyPageNotNil(t *testing.T) {
	db := testutils.SetupTestDB(t)
	repo := repositories.NewRepository(db)

	page, err := repo.Task().ListByAssigneePaged(t.Context(), "nobody", contracts.Pagination{Page: 1, PageSize: 10})
	if err != nil {
		t.Fatalf("empty page: %v", err)
	}
	// A nil slice serialises to JSON null, which every client then has to
	// special-case. An empty result is an empty list.
	if page.Items == nil {
		t.Fatal("Items is nil; an empty result must serialise as [] not null")
	}
	if page.Total != 0 || page.HasMore() {
		t.Fatalf("empty page reported Total=%d HasMore=%v", page.Total, page.HasMore())
	}
	if got := page.TotalPages(); got != 1 {
		t.Fatalf("TotalPages() = %d on an empty result, want 1", got)
	}
}

// The paged queries are ordered, and tenantScopeDB joins the projects table —
// which carries created_at too. A bare "created_at DESC" is therefore ambiguous
// the moment tenant scoping is active, which it is for every request-driven
// call.
//
// The tests above all run without a TenantContext, so no join is added and the
// ambiguity never appears. That gap let the bug reach a running server, where
// it failed every paged request with
// "SQL logic error: ambiguous column name: created_at".
func TestPagination_WorksUnderTenantScoping(t *testing.T) {
	db := testutils.SetupTestDB(t)
	repo := repositories.NewRepository(db)
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

	seedTasks(t, repo, projectID, "alice", 12)

	scoped := entities.WithTenantContext(ctx, entities.TenantContext{TenantID: orgID.String()})
	page, err := repo.Task().ListByAssigneePaged(scoped, "alice", contracts.Pagination{Page: 1, PageSize: 5})
	if err != nil {
		t.Fatalf("paged query under tenant scoping: %v", err)
	}
	if len(page.Items) != 5 {
		t.Fatalf("got %d rows, want 5", len(page.Items))
	}
	if page.Total != 12 {
		t.Fatalf("Total = %d, want 12", page.Total)
	}
}
