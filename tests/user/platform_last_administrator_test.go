package user_test

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/gsoultan/metis/server/domains/entities"
	"github.com/gsoultan/metis/server/domains/services/contracts"
	service_impl "github.com/gsoultan/metis/server/domains/services/impl"
	"github.com/gsoultan/metis/server/repositories/pg"
	"github.com/gsoultan/metis/tests/testutils"
	"gorm.io/gorm"
)

// grantsAheadOfTheAdministrator is as many role grants as one read of the
// generated store returns: storm starts every query with a limit of 1000.
const grantsAheadOfTheAdministrator = 1000

type platformWorld struct {
	svc contracts.PlatformUserService
	db  *gorm.DB
	ctx context.Context
}

func newPlatformWorld(t *testing.T) platformWorld {
	t.Helper()
	db := testutils.SetupTestDB(t)
	accounts := pg.NewPlatformUserRepository(testutils.StormConn(db))
	ctx := entities.WithSystemContext(t.Context())
	if err := accounts.EnsureBuiltInRoles(ctx); err != nil {
		t.Fatalf("seed the built-in roles: %v", err)
	}
	return platformWorld{svc: service_impl.NewPlatformUserService(accounts), db: db, ctx: ctx}
}

// seedDesigners adds n platform accounts holding the designer role, in two
// statements. Their ids sort ahead of any the repository issues, and a read of
// the grants in key order meets all of theirs first.
func (w platformWorld) seedDesigners(t *testing.T, n int) {
	t.Helper()
	if err := w.db.WithContext(w.ctx).Exec(`
		INSERT INTO platform_users (id, created_at, updated_at, username, password_hash)
		SELECT ('00000000-0000-7000-8000-' || lpad(n::text, 12, '0'))::uuid, now(), now(),
		       format('designer-%s', n), 'not-a-real-hash'
		  FROM generate_series(1, ?) AS n`, n).Error; err != nil {
		t.Fatalf("seed %d accounts: %v", n, err)
	}
	if err := w.db.WithContext(w.ctx).Exec(`
		INSERT INTO platform_role_assignments (platform_user_id, platform_role_id)
		SELECT u.id, r.id FROM platform_users u, platform_roles r
		 WHERE u.username LIKE 'designer-%' AND r.name = ?`, entities.RoleDesigner).Error; err != nil {
		t.Fatalf("grant them the designer role: %v", err)
	}
}

func (w platformWorld) createAdministrator(t *testing.T, username string) uuid.UUID {
	t.Helper()
	id, err := w.svc.CreatePlatformUser(w.ctx, entities.PlatformUser{
		Username: username,
		Roles:    []string{entities.RoleAdmin},
	}, "a-password-long-enough")
	if err != nil {
		t.Fatalf("create %s: %v", username, err)
	}
	return id
}

// An installation with nobody who can administer it cannot be repaired from
// inside it, so deleting or demoting its last administrator is refused.
//
// The refusal only runs for an account that holds the role, and whether it
// did was read from every grant in the installation — through a query the
// store caps at a thousand rows. Past a thousand grants, the administrator's
// own could fall outside the window: the account read as holding no role, the
// check was skipped, and the last administrator could be deleted.
func TestTheLastPlatformAdministratorIsKeptPastAThousandGrants(t *testing.T) {
	w := newPlatformWorld(t)
	w.seedDesigners(t, grantsAheadOfTheAdministrator)
	admin := w.createAdministrator(t, "the-administrator")

	if err := w.svc.SetPlatformRoles(w.ctx, admin, []string{entities.RoleDesigner}); err == nil {
		t.Errorf("the only administrator of an installation with %d role grants was demoted",
			grantsAheadOfTheAdministrator+1)
	}
	if err := w.svc.DeletePlatformUser(w.ctx, admin); err == nil {
		t.Fatalf("the only administrator of an installation with %d role grants was deleted",
			grantsAheadOfTheAdministrator+1)
	}
}
