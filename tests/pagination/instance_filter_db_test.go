package pagination_test

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/gsoultan/metis/server/domains/entities"
	"github.com/gsoultan/metis/server/repositories"
	"github.com/gsoultan/metis/server/repositories/contracts"
	"github.com/gsoultan/metis/server/repositories/models"
	"github.com/gsoultan/metis/tests/testutils"
	"gorm.io/gorm"
)

// The instance list's status filter used to run in the browser, over the rows
// that had already arrived. These prove it now runs in the query — which is the
// only version of the feature that is any use, and the difference is invisible
// until the thing you are looking for is not on the first page.

// seedProject writes the organization and project an instance hangs off.
//
// process_instances has foreign keys onto both, so an instance cannot be
// written without them — which is the schema doing its job, and why the
// fixtures here are rows rather than bare ids.
func seedProject(t *testing.T, db *gorm.DB, orgID, projectID uuid.UUID, name string) {
	t.Helper()
	rows := []any{
		&models.OrganizationModel{Base: models.Base{ID: models.FromUUID(orgID)}, Name: name},
		&models.ProjectModel{
			Base:           models.Base{ID: models.FromUUID(projectID)},
			OrganizationID: models.FromUUID(orgID),
			Name:           name,
		},
	}
	for _, row := range rows {
		if err := db.Create(row).Error; err != nil {
			t.Fatalf("seed %T: %v", row, err)
		}
	}
}

// seedDefinition writes the process an instance is a run of.
func seedDefinition(t *testing.T, db *gorm.DB, projectID, definitionID uuid.UUID, key string, version int) {
	t.Helper()
	err := db.Create(&models.ProcessDefinitionModel{
		Base:      models.Base{ID: models.FromUUID(definitionID)},
		ProjectID: models.FromUUID(projectID),
		Key:       key,
		Name:      key,
		Version:   version,
	}).Error
	if err != nil {
		t.Fatalf("seed definition %s: %v", key, err)
	}
}

// seedInstances writes n instances of one definition in one state.
//
// The ids are v7, so they sort by creation time, and the list orders newest
// first. Seeding the failures *before* the healthy rows therefore buries them on
// the last page, which is exactly where a page-scoped filter cannot see them.
func seedInstances(
	t *testing.T,
	repo repositories.Repository,
	ctx context.Context,
	projectID, definitionID uuid.UUID,
	status models.ProcessStatus,
	n int,
) {
	t.Helper()
	for i := range n {
		_, err := repo.Process().Create(ctx, models.ProcessInstanceModel{
			Base:         models.Base{ID: models.FromUUID(uuid.Must(uuid.NewV7()))},
			ProjectID:    models.FromUUID(projectID),
			DefinitionID: models.FromUUID(definitionID),
			Status:       status,
		})
		if err != nil {
			t.Fatalf("seed %s instance %d: %v", status, i, err)
		}
	}
}

// The failures are findable even though none of them is on the first page.
//
// This is the whole point of the change. Twelve failures among four hundred
// instances, all of them older than every healthy row, so under newest-first
// ordering they sit on the last page. Filtering the twenty-five rows of page one
// finds nothing and reports a healthy project.
func TestInstanceFilter_ReachesBeyondTheFirstPage(t *testing.T) {
	db := testutils.SetupTestDB(t)
	repo := repositories.NewRepository(testutils.StormConn(db))
	orgID := uuid.New()
	projectID := uuid.Must(uuid.NewV7())
	definitionID := uuid.Must(uuid.NewV7())
	seedProject(t, db, orgID, projectID, "Filtering")
	// The rows below are reached by joining through the project to its
	// organization, so the caller has to name one.
	ctx := entities.WithTenantContext(t.Context(), entities.TenantContext{TenantID: orgID.String()})
	seedDefinition(t, db, projectID, definitionID, "expenses", 1)

	seedInstances(t, repo, ctx, projectID, definitionID, models.ProcessFailed, 12)
	seedInstances(t, repo, ctx, projectID, definitionID, models.ProcessCompleted, 400)

	// Page one, unfiltered: every row is a completed one, and nothing on screen
	// suggests otherwise. This is the state the old page left an operator in.
	unfiltered, err := repo.Process().ListByProjectPaged(
		ctx, projectID, contracts.InstanceFilter{}, contracts.Pagination{Page: 1, PageSize: 25})
	if err != nil {
		t.Fatalf("unfiltered page 1: %v", err)
	}
	for _, instance := range unfiltered.Items {
		if instance.Status == models.ProcessFailed {
			t.Fatal("a failed instance landed on page 1; the fixture no longer buries them and proves nothing")
		}
	}

	filtered, err := repo.Process().ListByProjectPaged(
		ctx, projectID,
		contracts.InstanceFilter{Status: models.ProcessFailed},
		contracts.Pagination{Page: 1, PageSize: 25})
	if err != nil {
		t.Fatalf("filtered page 1: %v", err)
	}

	if len(filtered.Items) != 12 {
		t.Fatalf("filtering for failures returned %d rows, want 12 — the filter is not reaching the query",
			len(filtered.Items))
	}
	for _, instance := range filtered.Items {
		if instance.Status != models.ProcessFailed {
			t.Errorf("a %s instance came back from a filter for failures", instance.Status)
		}
	}
	// Total has to describe the matching rows, not the project: it is what the
	// page prints beside them, and 412 beside twelve failures is a lie.
	if filtered.Total != 12 {
		t.Errorf("Total = %d, want 12 — the count is not filtered with the rows", filtered.Total)
	}
}

// Narrowing to one process does the same, and combines with the state.
func TestInstanceFilter_NarrowsToOneDefinition(t *testing.T) {
	db := testutils.SetupTestDB(t)
	repo := repositories.NewRepository(testutils.StormConn(db))
	projectID := uuid.Must(uuid.NewV7())
	expenses := uuid.Must(uuid.NewV7())
	onboarding := uuid.Must(uuid.NewV7())
	orgID := uuid.New()
	seedProject(t, db, orgID, projectID, "Two processes")
	ctx := entities.WithTenantContext(t.Context(), entities.TenantContext{TenantID: orgID.String()})
	seedDefinition(t, db, projectID, expenses, "expenses", 1)
	seedDefinition(t, db, projectID, onboarding, "onboarding", 1)

	seedInstances(t, repo, ctx, projectID, expenses, models.ProcessFailed, 3)
	seedInstances(t, repo, ctx, projectID, expenses, models.ProcessActive, 5)
	seedInstances(t, repo, ctx, projectID, onboarding, models.ProcessFailed, 7)

	page, err := repo.Process().ListByProjectPaged(
		ctx, projectID,
		contracts.InstanceFilter{Status: models.ProcessFailed, DefinitionID: expenses},
		contracts.Pagination{Page: 1, PageSize: 25})
	if err != nil {
		t.Fatalf("filtered page: %v", err)
	}
	if page.Total != 3 {
		t.Fatalf("Total = %d, want 3 — the two filters are not both applied", page.Total)
	}
}

// The counts describe the project, whichever page is on screen.
//
// That is what lets twenty-five completed runs sit under the words "12 need
// attention" and both be true.
func TestInstanceFilter_CountsDescribeTheWholeProject(t *testing.T) {
	db := testutils.SetupTestDB(t)
	repo := repositories.NewRepository(testutils.StormConn(db))
	projectID := uuid.Must(uuid.NewV7())
	definitionID := uuid.Must(uuid.NewV7())
	orgID := uuid.New()
	seedProject(t, db, orgID, projectID, "Counting")
	ctx := entities.WithTenantContext(t.Context(), entities.TenantContext{TenantID: orgID.String()})
	seedDefinition(t, db, projectID, definitionID, "expenses", 1)

	seedInstances(t, repo, ctx, projectID, definitionID, models.ProcessFailed, 12)
	seedInstances(t, repo, ctx, projectID, definitionID, models.ProcessCompleted, 400)
	seedInstances(t, repo, ctx, projectID, definitionID, models.ProcessActive, 37)

	counts, err := repo.Process().CountByStatuses(ctx, projectID, contracts.InstanceFilter{})
	if err != nil {
		t.Fatalf("count by status: %v", err)
	}

	for status, want := range map[models.ProcessStatus]int64{
		models.ProcessFailed:    12,
		models.ProcessCompleted: 400,
		models.ProcessActive:    37,
	} {
		if counts[status] != want {
			t.Errorf("%s = %d, want %d", status, counts[status], want)
		}
	}
	// A state the project has none of must be absent rather than present as
	// zero: the list drops empty states, and a chip reading "Paused 0" is a
	// control that can only ever empty the table.
	if _, ok := counts[models.ProcessSuspended]; ok {
		t.Errorf("suspended is reported with %d, but nothing is suspended", counts[models.ProcessSuspended])
	}
}

// A filter is not a way around tenant scoping.
//
// The predicates are added to the scope rather than swapped for it, but that is
// a property of how scopedQuery composes and would break silently if a later
// change built the query the other way round.
func TestInstanceFilter_DoesNotReachAnotherTenantsRows(t *testing.T) {
	db := testutils.SetupTestDB(t)
	repo := repositories.NewRepository(testutils.StormConn(db))

	orgA, orgB := uuid.New(), uuid.New()
	projectA, projectB := uuid.New(), uuid.New()
	definitionA, definitionB := uuid.Must(uuid.NewV7()), uuid.Must(uuid.NewV7())

	seedProject(t, db, orgA, projectA, "Org A")
	ctx := entities.WithTenantContext(t.Context(), entities.TenantContext{TenantID: orgA.String()})
	seedProject(t, db, orgB, projectB, "Org B")
	// The same key in both projects, so a read that returns the wrong tenant's
	// rows cannot be explained away as a filter mismatch.
	seedDefinition(t, db, projectA, definitionA, "expenses", 1)
	seedDefinition(t, db, projectB, definitionB, "expenses", 1)

	// Seeded as system work so both tenants' rows land: a context scoped to
	// one organization cannot write into the other's project, which is the
	// property under test rather than something to work around. The reads
	// below are the ones that matter.
	seedCtx := entities.WithSystemContext(t.Context())
	seedInstances(t, repo, seedCtx, projectA, definitionA, models.ProcessFailed, 2)
	seedInstances(t, repo, seedCtx, projectB, definitionB, models.ProcessFailed, 9)

	failures := contracts.InstanceFilter{Status: models.ProcessFailed}

	t.Run("naming another tenant's project", func(t *testing.T) {
		page, err := repo.Process().ListByProjectPaged(ctx, projectB, failures, contracts.Pagination{Page: 1, PageSize: 25})
		if err != nil {
			t.Fatalf("list project B as org A: %v", err)
		}
		if len(page.Items) != 0 || page.Total != 0 {
			t.Fatalf("org A read %d of org B's failed instances (total %d)", len(page.Items), page.Total)
		}
	})

	t.Run("naming no project at all", func(t *testing.T) {
		page, err := repo.Process().ListPaged(ctx, failures, contracts.Pagination{Page: 1, PageSize: 25})
		if err != nil {
			t.Fatalf("list every project as org A: %v", err)
		}
		if page.Total != 2 {
			t.Fatalf("org A has two failed instances; reading %d includes another tenant's", page.Total)
		}
	})

	t.Run("counting", func(t *testing.T) {
		counts, err := repo.Process().CountByStatuses(ctx, uuid.Nil, contracts.InstanceFilter{})
		if err != nil {
			t.Fatalf("count as org A: %v", err)
		}
		if counts[models.ProcessFailed] != 2 {
			t.Fatalf("org A has two failed instances; counting %d includes another tenant's",
				counts[models.ProcessFailed])
		}
	})
}

// seedIncident raises an unresolved incident against an instance.
func seedIncident(t *testing.T, db *gorm.DB, instanceID, definitionID uuid.UUID) {
	t.Helper()
	err := db.Create(&models.IncidentModel{
		Base:         models.Base{ID: models.FromUUID(uuid.Must(uuid.NewV7()))},
		InstanceID:   models.FromUUID(instanceID),
		DefinitionID: models.FromUUID(definitionID),
		JobID:        models.FromUUID(uuid.Must(uuid.NewV7())),
		NodeID:       "Activity_ChargeCard",
		Error:        "the payment gateway refused the connection",
		Status:       models.IncidentOpen,
	}).Error
	if err != nil {
		t.Fatalf("seed incident: %v", err)
	}
}

// "Needs attention" cannot be read off the status column.
//
// The engine never writes models.ProcessFailed. A job that exhausts its retries
// raises an incident and stops, and the instance stays `active` — correctly, it
// has not failed, it is waiting for somebody. A list that looked for a failed
// status would mark nothing and report a healthy project for ever, which is the
// one answer this page must never give wrongly.
func TestInstanceAttention_IsNotTheStatusColumn(t *testing.T) {
	db := testutils.SetupTestDB(t)
	repo := repositories.NewRepository(testutils.StormConn(db))
	projectID := uuid.Must(uuid.NewV7())
	definitionID := uuid.Must(uuid.NewV7())
	orgID := uuid.New()
	seedProject(t, db, orgID, projectID, "Attention")
	ctx := entities.WithTenantContext(t.Context(), entities.TenantContext{TenantID: orgID.String()})
	seedDefinition(t, db, projectID, definitionID, "expenses", 1)
	seedInstances(t, repo, ctx, projectID, definitionID, models.ProcessActive, 5)

	page, err := repo.Process().ListByProjectPaged(
		ctx, projectID, contracts.InstanceFilter{}, contracts.Pagination{Page: 1, PageSize: 25})
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	// The premise: every one of them is active, so status says nothing is wrong.
	for _, instance := range page.Items {
		if instance.Status != models.ProcessActive {
			t.Fatalf("fixture wrote a %s instance; this test is about the active ones", instance.Status)
		}
	}

	broken := uuid.UUID(page.Items[0].ID)
	seedIncident(t, db, broken, definitionID)

	t.Run("the row is marked", func(t *testing.T) {
		ids := []uuid.UUID{broken, uuid.UUID(page.Items[1].ID)}
		counts, err := repo.Process().OpenIncidentsByInstance(ctx, ids)
		if err != nil {
			t.Fatalf("open incidents: %v", err)
		}
		if counts[broken] != 1 {
			t.Errorf("the instance with an incident reports %d; the failure drawer is unreachable from its row",
				counts[broken])
		}
		if counts[uuid.UUID(page.Items[1].ID)] != 0 {
			t.Error("an instance with no incident was marked")
		}
	})

	t.Run("the project total counts it", func(t *testing.T) {
		total, err := repo.Process().CountInstancesNeedingAttention(
			ctx, projectID, contracts.InstanceFilter{})
		if err != nil {
			t.Fatalf("count needing attention: %v", err)
		}
		if total != 1 {
			t.Errorf("needing attention = %d, want 1", total)
		}
	})

	t.Run("the filter reaches it", func(t *testing.T) {
		filtered, err := repo.Process().ListByProjectPaged(
			ctx, projectID,
			contracts.InstanceFilter{NeedsAttention: true},
			contracts.Pagination{Page: 1, PageSize: 25})
		if err != nil {
			t.Fatalf("filtered list: %v", err)
		}
		if filtered.Total != 1 || len(filtered.Items) != 1 {
			t.Fatalf("filtering for attention returned %d rows (total %d), want 1",
				len(filtered.Items), filtered.Total)
		}
		if uuid.UUID(filtered.Items[0].ID) != broken {
			t.Errorf("the wrong instance came back: %v", filtered.Items[0].ID)
		}
	})

	t.Run("a resolved incident stops counting", func(t *testing.T) {
		if err := db.Model(&models.IncidentModel{}).
			Where("instance_id = ?", models.FromUUID(broken)).
			Update("status", models.IncidentResolved).Error; err != nil {
			t.Fatalf("resolve: %v", err)
		}
		total, err := repo.Process().CountInstancesNeedingAttention(
			ctx, projectID, contracts.InstanceFilter{})
		if err != nil {
			t.Fatalf("count after resolving: %v", err)
		}
		if total != 0 {
			t.Errorf("needing attention = %d after the incident was resolved, want 0", total)
		}
	})
}

// Another tenant's incidents are neither counted nor reachable.
func TestInstanceAttention_IsTenantScoped(t *testing.T) {
	db := testutils.SetupTestDB(t)
	repo := repositories.NewRepository(testutils.StormConn(db))

	orgA, orgB := uuid.New(), uuid.New()
	projectA, projectB := uuid.New(), uuid.New()
	definitionA, definitionB := uuid.Must(uuid.NewV7()), uuid.Must(uuid.NewV7())
	seedProject(t, db, orgA, projectA, "Org A")
	seedProject(t, db, orgB, projectB, "Org B")
	seedDefinition(t, db, projectA, definitionA, "expenses", 1)
	seedDefinition(t, db, projectB, definitionB, "expenses", 1)
	// Seeding two tenants' rows is fixture construction rather than a request,
	// so it is system work. The reads below are per-tenant, which is the thing
	// this test is actually about.
	seedCtx := entities.WithSystemContext(t.Context())
	seedInstances(t, repo, seedCtx, projectA, definitionA, models.ProcessActive, 1)
	seedInstances(t, repo, seedCtx, projectB, definitionB, models.ProcessActive, 1)
	ctx := entities.WithTenantContext(t.Context(), entities.TenantContext{TenantID: orgA.String()})

	a, err := repo.Process().ListByProjectPaged(
		ctx, projectA, contracts.InstanceFilter{}, contracts.Pagination{Page: 1, PageSize: 25})
	if err != nil {
		t.Fatalf("list A: %v", err)
	}
	// Read as system work: this is how the test obtains an id belonging to the
	// *other* tenant, to probe with below. Asking for it as orgA is exactly
	// what the assertion further down proves does not answer, so it cannot
	// also be how the id is found.
	b, err := repo.Process().ListByProjectPaged(
		seedCtx, projectB, contracts.InstanceFilter{}, contracts.Pagination{Page: 1, PageSize: 25})
	if err != nil {
		t.Fatalf("list B: %v", err)
	}
	foreign := uuid.UUID(b.Items[0].ID)
	seedIncident(t, db, foreign, definitionB)
	seedIncident(t, db, uuid.UUID(a.Items[0].ID), definitionA)

	// Asking about the other tenant's instance by id must not answer, even
	// though the caller supplied the id and the row exists.
	counts, err := repo.Process().OpenIncidentsByInstance(ctx, []uuid.UUID{foreign})
	if err != nil {
		t.Fatalf("open incidents as org A: %v", err)
	}
	if counts[foreign] != 0 {
		t.Errorf("org A read %d of org B's incidents by naming the instance", counts[foreign])
	}

	total, err := repo.Process().CountInstancesNeedingAttention(ctx, uuid.Nil, contracts.InstanceFilter{})
	if err != nil {
		t.Fatalf("count as org A: %v", err)
	}
	if total != 1 {
		t.Errorf("org A has one instance needing attention; counting %d includes another tenant's", total)
	}
}
