package tenant

import (
	"errors"
	"testing"

	"github.com/google/uuid"
	"github.com/gsoultan/gobpm/internal/pkg/features"
	"github.com/gsoultan/gobpm/server/domains/entities"
	"github.com/gsoultan/gobpm/server/repositories/gorms"
	"github.com/gsoultan/gobpm/server/repositories/models"
	"gorm.io/gorm"
)

// TestStrictScope_DeniesAContextWithNoIdentity is the point of the flag.
//
// Without it, a context carrying no TenantContext reads everything, so a code
// path that forgets to resolve a tenant gets full access rather than an error —
// which is how the OIDC hole reached every tenant. With it, the same context
// reads nothing, and the mistake surfaces as missing data rather than as a
// silent cross-tenant leak.
func TestStrictScope_DeniesAContextWithNoIdentity(t *testing.T) {
	defer features.OverrideForTest(features.StrictTenantScope, true)()

	forEachDialect(t, func(t *testing.T, db *gorm.DB) {
		f := seedTenantFixture(t, db)
		// No tenant, no system marker: an unidentified caller.
		ctx := t.Context()

		t.Run("lists return nothing", func(t *testing.T) {
			rows, err := gorms.NewFormRepository(db).ListByProject(ctx, uuid.Nil)
			if err != nil {
				t.Fatalf("list: %v", err)
			}
			assertSameIDs(t, idsOf(rows, func(m models.FormModel) uuid.UUID { return uuid.UUID(m.ID) }), nil)
		})

		t.Run("get by id is not found", func(t *testing.T) {
			if _, err := gorms.NewFormRepository(db).Get(ctx, f.formA); !errors.Is(err, gorm.ErrRecordNotFound) {
				t.Fatalf("got %v, want %v", err, gorm.ErrRecordNotFound)
			}
		})

		t.Run("writes are refused", func(t *testing.T) {
			if err := gorms.NewFormRepository(db).Delete(ctx, f.formA); !errors.Is(err, gorm.ErrRecordNotFound) {
				t.Errorf("delete: got %v, want %v", err, gorm.ErrRecordNotFound)
			}
			if !rowExists(t, db, &models.FormModel{}, "forms", f.formA) {
				t.Fatal("the delete was refused but the row is gone")
			}
		})

		t.Run("creates are refused", func(t *testing.T) {
			err := gorms.NewFormRepository(db).Create(ctx, models.FormModel{
				Base: models.Base{ID: models.FromUUID(uuid.New())}, ProjectID: models.FromUUID(f.projectA), Key: "x",
			})
			if !errors.Is(err, gorm.ErrRecordNotFound) {
				t.Errorf("create: got %v, want %v", err, gorm.ErrRecordNotFound)
			}
		})
	})
}

// TestStrictScope_SystemWorkStillSeesEverything is the other half. The job
// worker, message consumers and migrations legitimately span every tenant, and
// denying them would stop the engine rather than secure it.
//
// This is also the test that would catch a background entry point which forgot
// to mark itself: those paths read nothing, which looks exactly like an engine
// with no work to do.
func TestStrictScope_SystemWorkStillSeesEverything(t *testing.T) {
	defer features.OverrideForTest(features.StrictTenantScope, true)()

	forEachDialect(t, func(t *testing.T, db *gorm.DB) {
		f := seedTenantFixture(t, db)
		ctx := entities.WithSystemContext(t.Context())

		forms, err := gorms.NewFormRepository(db).ListByProject(ctx, uuid.Nil)
		if err != nil {
			t.Fatalf("list as system: %v", err)
		}
		assertSameIDs(t, idsOf(forms, func(m models.FormModel) uuid.UUID { return uuid.UUID(m.ID) }),
			[]uuid.UUID{f.formA, f.formB})

		// Both tenants' instances, which is what a job worker needs to see.
		instances, err := gorms.NewProcessRepository(db).List(ctx)
		if err != nil {
			t.Fatalf("list instances as system: %v", err)
		}
		assertSameIDs(t, idsOf(instances, func(m models.ProcessInstanceModel) uuid.UUID { return uuid.UUID(m.ID) }),
			[]uuid.UUID{f.instanceA, f.instanceB})

		if _, err := gorms.NewProcessRepository(db).Get(ctx, f.instanceB); err != nil {
			t.Errorf("system work could not read an instance: %v", err)
		}
	})
}

// TestStrictScope_TenantScopingIsUnchanged confirms the flag only decides what
// happens with *no* identity. A request that resolved a tenant behaves the same
// either way, which is what makes turning the flag on a bounded change.
func TestStrictScope_TenantScopingIsUnchanged(t *testing.T) {
	defer features.OverrideForTest(features.StrictTenantScope, true)()

	forEachDialect(t, func(t *testing.T, db *gorm.DB) {
		f := seedTenantFixture(t, db)
		ctx := f.ctxAsA(t)

		forms, err := gorms.NewFormRepository(db).ListByProject(ctx, uuid.Nil)
		if err != nil {
			t.Fatalf("list: %v", err)
		}
		assertSameIDs(t, idsOf(forms, func(m models.FormModel) uuid.UUID { return uuid.UUID(m.ID) }),
			[]uuid.UUID{f.formA})

		if _, err := gorms.NewFormRepository(db).Get(ctx, f.formB); !errors.Is(err, gorm.ErrRecordNotFound) {
			t.Errorf("cross-tenant get: got %v, want %v", err, gorm.ErrRecordNotFound)
		}
		if _, err := gorms.NewFormRepository(db).Get(ctx, f.formA); err != nil {
			t.Errorf("own get: %v", err)
		}
	})
}
