package tenant

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/gsoultan/metis/internal/pkg/features"
	"github.com/gsoultan/metis/internal/pkg/tenantscope"
	"github.com/gsoultan/metis/server/domains/entities"
	"github.com/gsoultan/metis/server/repositories"
	"github.com/gsoultan/metis/server/repositories/models"
	"github.com/gsoultan/metis/server/repositories/pg"
	"github.com/gsoultan/metis/tests/testutils"
	"gorm.io/gorm"
)

// A request reads its organization's projects once and every scoped call in it
// reuses that (tenantscope.Request). These are the isolation properties with the
// reuse in play, under the default scope and the strict one: whatever a request
// keeps, it keeps for its own organization only, never for another, never past
// a change to its own projects, and never past its end.
//
// The rest of this package reads through contexts that carry a tenant and no
// request — background work, the event stream and the message consumers look
// like that, and read the list every time.

// asRequestOfA is what the tenant resolver hands an endpoint: organization A
// as the tenant, and a request to keep its scope in for the length of the call.
func (f tenantFixture) asRequestOfA(t *testing.T) context.Context {
	t.Helper()
	ctx, request := tenantscope.WithRequest(f.ctxAsA(t), f.orgA.String())
	t.Cleanup(request.End)
	return ctx
}

// eachScope runs body under the default scope and under the strict one. A
// request that resolved a tenant must behave identically in both.
func eachScope(t *testing.T, body func(t *testing.T, db *gorm.DB)) {
	t.Helper()
	for _, scope := range []struct {
		name   string
		strict bool
	}{{"default scope", false}, {"strict scope", true}} {
		t.Run(scope.name, func(t *testing.T) {
			defer features.OverrideForTest(features.StrictTenantScope, scope.strict)()
			forEachDialect(t, body)
		})
	}
}

func TestARequestsReusedScopeStillDeniesAnotherOrganization(t *testing.T) {
	eachScope(t, func(t *testing.T, db *gorm.DB) {
		f := seedTenantFixture(t, db)
		repo := repositories.NewRepository(testutils.StormConn(db))
		ctx := f.asRequestOfA(t)
		reads := pg.ScopeReads()

		tasks, err := repo.Task().List(ctx)
		if err != nil {
			t.Fatalf("list tasks: %v", err)
		}
		assertSameIDs(t, idsOf(tasks, func(m models.TaskModel) uuid.UUID { return uuid.UUID(m.ID) }), []uuid.UUID{f.taskA})

		instances, err := repo.Process().List(ctx)
		if err != nil {
			t.Fatalf("list instances: %v", err)
		}
		assertSameIDs(t, idsOf(instances, func(m models.ProcessInstanceModel) uuid.UUID { return uuid.UUID(m.ID) }),
			[]uuid.UUID{f.instanceA})

		definitions, err := repo.Definition().ListByProject(ctx, uuid.Nil)
		if err != nil {
			t.Fatalf("list definitions: %v", err)
		}
		assertSameIDs(t, idsOf(definitions, func(m models.ProcessDefinitionModel) uuid.UUID { return uuid.UUID(m.ID) }),
			[]uuid.UUID{f.definitionA})

		forms, err := repo.Form().ListByProject(ctx, f.projectB)
		if err != nil {
			t.Fatalf("list the other organization's forms: %v", err)
		}
		assertSameIDs(t, idsOf(forms, func(m models.FormModel) uuid.UUID { return uuid.UUID(m.ID) }), nil)

		for name, read := range map[string]func() error{
			"their task":     func() error { _, err := repo.Task().Get(ctx, f.taskB); return err },
			"their instance": func() error { _, err := repo.Process().Get(ctx, f.instanceB); return err },
			"their project":  func() error { _, err := repo.Project().Get(ctx, f.projectB); return err },
			"their form":     func() error { _, err := repo.Form().Get(ctx, f.formB); return err },
			"deleting their form": func() error {
				return repo.Form().Delete(ctx, f.formB)
			},
			"creating in their project": func() error {
				return repo.Form().Create(ctx, models.FormModel{
					Base: models.Base{ID: models.FromUUID(uuid.New())}, ProjectID: models.FromUUID(f.projectB), Key: "planted",
				})
			},
		} {
			if err := read(); !isNotFound(err) {
				t.Errorf("%s: got %v, want a not-found", name, err)
			}
		}
		if !rowExists(t, db, &models.FormModel{}, "forms", f.formB) {
			t.Fatal("the delete was refused but the other organization's form is gone")
		}
		if _, err := repo.Task().Get(ctx, f.taskA); err != nil {
			t.Errorf("own task: %v", err)
		}

		// Everything above was answered from one read: this is the reuse under
		// test, not a request that happened to read the list each time.
		if n := pg.ScopeReads() - reads; n != 1 {
			t.Fatalf("the request read its organization's projects %d times, want 1", n)
		}
	})
}

// The account service asks each of an account's organizations in turn, inside
// one request. Each is answered with its own projects.
func TestAnotherTenantInsideARequestIsNotAnsweredFromTheRequestsScope(t *testing.T) {
	eachScope(t, func(t *testing.T, db *gorm.DB) {
		f := seedTenantFixture(t, db)
		repo := repositories.NewRepository(testutils.StormConn(db))
		ctx := f.asRequestOfA(t)

		if _, err := repo.Task().List(ctx); err != nil {
			t.Fatalf("list as A: %v", err)
		}
		asB := entities.WithTenantContext(ctx, entities.TenantContext{TenantID: f.orgB.String()})
		tasks, err := repo.Task().List(asB)
		if err != nil {
			t.Fatalf("list as B: %v", err)
		}
		assertSameIDs(t, idsOf(tasks, func(m models.TaskModel) uuid.UUID { return uuid.UUID(m.ID) }), []uuid.UUID{f.taskB})
		if _, err := repo.Task().Get(asB, f.taskA); !isNotFound(err) {
			t.Errorf("A's task read as B inside A's request: got %v, want a not-found", err)
		}

		tasks, err = repo.Task().List(ctx)
		if err != nil {
			t.Fatalf("list as A again: %v", err)
		}
		assertSameIDs(t, idsOf(tasks, func(m models.TaskModel) uuid.UUID { return uuid.UUID(m.ID) }), []uuid.UUID{f.taskA})
	})
}

// Creating a project and deploying into it is one conversation, and the scope
// the request read before the project existed must not refuse the deploy.
func TestAProjectCreatedInARequestIsInItsScopeAtOnce(t *testing.T) {
	eachScope(t, func(t *testing.T, db *gorm.DB) {
		f := seedTenantFixture(t, db)
		repo := repositories.NewRepository(testutils.StormConn(db))
		ctx := f.asRequestOfA(t)

		if _, err := repo.Form().ListByProject(ctx, uuid.Nil); err != nil {
			t.Fatalf("read before the project exists: %v", err)
		}
		created := uuid.New()
		if err := repo.Project().Create(ctx, models.ProjectModel{
			Base:           models.Base{ID: models.FromUUID(created)},
			OrganizationID: models.FromUUID(f.orgA),
			Name:           "Created during the request",
		}); err != nil {
			t.Fatalf("create the project: %v", err)
		}
		if err := repo.Definition().Create(ctx, models.ProcessDefinitionModel{
			Base:      models.Base{ID: models.FromUUID(uuid.New())},
			ProjectID: models.FromUUID(created),
			Key:       "intake", Name: "Intake", Version: 1,
		}); err != nil {
			t.Fatalf("deploying into the project the same request created was refused: %v", err)
		}
	})
}

func TestAProjectDeletedInARequestLeavesItsScopeAtOnce(t *testing.T) {
	eachScope(t, func(t *testing.T, db *gorm.DB) {
		f := seedTenantFixture(t, db)
		repo := repositories.NewRepository(testutils.StormConn(db))
		ctx := f.asRequestOfA(t)

		doomed, form := uuid.New(), uuid.New()
		for _, row := range []any{
			&models.ProjectModel{Base: models.Base{ID: models.FromUUID(doomed)}, OrganizationID: models.FromUUID(f.orgA), Name: "Doomed"},
			&models.FormModel{Base: models.Base{ID: models.FromUUID(form)}, ProjectID: models.FromUUID(doomed), Key: "doomed-form"},
		} {
			if err := db.Create(row).Error; err != nil {
				t.Fatalf("seed %T: %v", row, err)
			}
		}
		if _, err := repo.Form().Get(ctx, form); err != nil {
			t.Fatalf("the form is in scope before its project is deleted: %v", err)
		}
		if err := repo.Project().Delete(ctx, doomed); err != nil {
			t.Fatalf("delete the project: %v", err)
		}
		if _, err := repo.Form().Get(ctx, form); !isNotFound(err) {
			t.Errorf("a form of the project this request deleted: got %v, want a not-found", err)
		}
	})
}

// A goroutine the request started can hold its context past the end. From
// there it reads for itself, and sees what was created since.
func TestARequestsScopeEndsWithTheRequest(t *testing.T) {
	eachScope(t, func(t *testing.T, db *gorm.DB) {
		f := seedTenantFixture(t, db)
		repo := repositories.NewRepository(testutils.StormConn(db))
		ctx, request := tenantscope.WithRequest(f.ctxAsA(t), f.orgA.String())

		if _, err := repo.Form().ListByProject(ctx, uuid.Nil); err != nil {
			t.Fatalf("read during the request: %v", err)
		}
		request.End()

		later := uuid.New()
		if err := repo.Project().Create(f.ctxAsA(t), models.ProjectModel{
			Base:           models.Base{ID: models.FromUUID(later)},
			OrganizationID: models.FromUUID(f.orgA),
			Name:           "Created by another request",
		}); err != nil {
			t.Fatalf("create the project elsewhere: %v", err)
		}
		if _, err := repo.Project().Get(ctx, later); err != nil {
			t.Fatalf("after the request ended its context still answered from the old list: %v", err)
		}
	})
}

// Keeping a scope is not an identity. A context with a request and no tenant is
// whatever the flag says a context with no tenant is: nothing under the strict
// scope, as before.
func TestARequestWithNoTenantIsNotAnIdentity(t *testing.T) {
	defer features.OverrideForTest(features.StrictTenantScope, true)()

	forEachDialect(t, func(t *testing.T, db *gorm.DB) {
		f := seedTenantFixture(t, db)
		repo := repositories.NewRepository(testutils.StormConn(db))
		ctx, request := tenantscope.WithRequest(t.Context(), f.orgA.String())
		defer request.End()

		forms, err := repo.Form().ListByProject(ctx, uuid.Nil)
		if err != nil {
			t.Fatalf("list: %v", err)
		}
		assertSameIDs(t, idsOf(forms, func(m models.FormModel) uuid.UUID { return uuid.UUID(m.ID) }), nil)
		if _, err := repo.Form().Get(ctx, f.formA); !isNotFound(err) {
			t.Errorf("get with a request and no tenant: got %v, want a not-found", err)
		}
	})
}
