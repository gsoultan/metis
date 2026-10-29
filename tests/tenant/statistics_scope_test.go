package tenant

import (
	"testing"

	"github.com/google/uuid"
	"github.com/gsoultan/metis/server/repositories/models"
	"github.com/gsoultan/metis/server/repositories/pg"
	"github.com/gsoultan/metis/tests/testutils"
)

// The dashboard counters are tenant-scoped even when no project is named.
//
// They used to filter on project alone, and the statistics endpoint accepts an
// empty project_id — so its dashboard tile counted every organization's
// instances and tasks in the installation. Counts are not contents, but how much
// work a system carries is still somebody else's business, and this is the one
// read where the caller is *expected* to leave the filter off.
//
// The fixture seeds exactly one active instance and one task per organization,
// so "1" is the tenant's own and "2" is the leak.
func TestTenantIsolation_StatisticsCountersAreScoped(t *testing.T) {
	db := testutils.SetupTestDB(t)
	f := seedTenantFixture(t, db)
	ctx := f.ctxAsA(t)

	t.Run("instances", func(t *testing.T) {
		count, err := pg.NewProcessRepository(db).CountByStatus(ctx, uuid.Nil, models.ProcessActive)
		if err != nil {
			t.Fatalf("count instances: %v", err)
		}
		if count != 1 {
			t.Fatalf("organization A has one active instance; counting %d includes another tenant's", count)
		}
	})

	t.Run("tasks", func(t *testing.T) {
		count, err := pg.NewTaskRepository(db).CountByStatus(ctx, uuid.Nil, "")
		if err != nil {
			t.Fatalf("count tasks: %v", err)
		}
		if count != 1 {
			t.Fatalf("organization A has one task; counting %d includes another tenant's", count)
		}
	})

	// Naming the tenant's own project still works — the scope is added to the
	// project filter, not swapped for it.
	t.Run("with a project named", func(t *testing.T) {
		count, err := pg.NewProcessRepository(db).CountByStatus(ctx, f.projectA, models.ProcessActive)
		if err != nil {
			t.Fatalf("count instances in project A: %v", err)
		}
		if count != 1 {
			t.Fatalf("project A has one active instance, counted %d", count)
		}
	})

	// Naming another tenant's project counts nothing rather than counting theirs.
	t.Run("with a foreign project named", func(t *testing.T) {
		count, err := pg.NewProcessRepository(db).CountByStatus(ctx, f.projectB, models.ProcessActive)
		if err != nil {
			t.Fatalf("count instances in project B: %v", err)
		}
		if count != 0 {
			t.Fatalf("organization A must not count organization B's instances, got %d", count)
		}
	})
}
