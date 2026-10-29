package user_test

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/gsoultan/metis/server/repositories/db"
	"github.com/gsoultan/metis/server/repositories/store/platformrole"
	"github.com/gsoultan/metis/server/repositories/store/platformroleassignment"
	"github.com/gsoultan/metis/server/repositories/store/platformuser"

	"github.com/google/uuid"
	"github.com/gsoultan/metis/server/domains/entities"
	"github.com/gsoultan/metis/server/domains/services/contracts"
	service_impl "github.com/gsoultan/metis/server/domains/services/impl"
	"github.com/gsoultan/metis/server/repositories/pg"
	"github.com/gsoultan/metis/tests/testutils"
)

// grantsAheadOfTheAdministrator is as many role grants as one read of the
// generated store returns: storm starts every query with a limit of 1000.
const grantsAheadOfTheAdministrator = 1000

type platformWorld struct {
	svc contracts.PlatformUserService
	db  *db.Conn
	ctx context.Context
}

func newPlatformWorld(t *testing.T) platformWorld {
	t.Helper()
	db := testutils.SetupTestDB(t)
	accounts := pg.NewPlatformUserRepository(db)
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
	ex, now := testutils.Executor(w.db), time.Now()
	designer, found, err := platformrole.New().NameEq(entities.RoleDesigner).One(w.ctx, ex)
	if err != nil || !found {
		t.Fatalf("find the designer role: found %v, %v", found, err)
	}
	accounts := make([]platformuser.Row, n)
	grants := make([]platformroleassignment.Row, n)
	for i := range accounts {
		accounts[i] = platformuser.Row{
			ID:        uuid.MustParse(fmt.Sprintf("00000000-0000-7000-8000-%012d", i+1)),
			CreatedAt: now, UpdatedAt: now, Username: fmt.Sprintf("designer-%d", i+1), PasswordHash: "not-a-real-hash",
		}
		grants[i] = platformroleassignment.Row{PlatformUserID: accounts[i].ID, PlatformRoleID: designer.ID}
	}
	if _, err := platformuser.InsertAll(w.ctx, ex, accounts); err != nil {
		t.Fatalf("seed %d accounts: %v", n, err)
	}
	if _, err := platformroleassignment.InsertAll(w.ctx, ex, grants); err != nil {
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

// A deleted account administers nothing, so it must not count as the other
// administrator that makes removing the last live one safe.
//
// Deleting an account marks its row and leaves its grants where they are, and
// the count of administrators counted grants. Once one of two administrators
// was deleted, the survivor still counted two — its own grant and the deleted
// account's — so it could be deleted or demoted too, leaving an installation
// nobody can sign in to administer.
func TestADeletedPlatformAdministratorDoesNotCountAsAnother(t *testing.T) {
	w := newPlatformWorld(t)
	first := w.createAdministrator(t, "first-administrator")
	second := w.createAdministrator(t, "second-administrator")

	if err := w.svc.DeletePlatformUser(w.ctx, first); err != nil {
		t.Fatalf("deleting one of two administrators must be allowed: %v", err)
	}
	if err := w.svc.SetPlatformRoles(w.ctx, second, []string{entities.RoleDesigner}); err == nil {
		t.Error("the last live administrator was demoted: the deleted one still counted")
	}
	if err := w.svc.DeletePlatformUser(w.ctx, second); err == nil {
		t.Fatal("the last live administrator was deleted: the deleted one still counted")
	}
}
