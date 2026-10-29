package tenant

import (
	"testing"

	"github.com/gsoultan/metis/server/repositories/store/group"
	"github.com/gsoultan/metis/server/repositories/store/organization"
	"github.com/gsoultan/storm/runtime"

	"github.com/gsoultan/metis/server/repositories/db"

	"github.com/google/uuid"
	"github.com/gsoultan/metis/server/domains/entities"
	"github.com/gsoultan/metis/server/repositories/models"
	"github.com/gsoultan/metis/server/repositories/pg"
	"github.com/gsoultan/metis/tests/testutils"
)

// directoryFixture seeds one organization, one group and one account per tenant.
//
// Separate from seedTenantFixture because that one is built around projects, and
// users and groups hang off the organization directly — a user through a
// membership table, which is its own scope shape.
type directoryFixture struct {
	orgA, orgB     uuid.UUID
	groupA, groupB uuid.UUID
	userA, userB   uuid.UUID
}

func seedDirectoryFixture(t *testing.T, db *db.Conn) directoryFixture {
	t.Helper()
	f := directoryFixture{
		orgA: uuid.New(), orgB: uuid.New(),
		groupA: uuid.New(), groupB: uuid.New(),
		userA: uuid.New(), userB: uuid.New(),
	}
	orgs := []uuid.UUID{f.orgA, f.orgB}
	groups := []uuid.UUID{f.groupA, f.groupB}
	users := []uuid.UUID{f.userA, f.userB}
	// Both tenants deliberately name their group the same thing, so a read that
	// returns the wrong one cannot be explained away as a filter mismatch.
	names := []string{"alice", "bob"}

	repo := pg.NewUserRepository(db)
	for i, org := range orgs {
		ex := testutils.Executor(db)
		o := organization.Create()
		o.SetID(org)
		o.SetName("org " + names[i])
		if _, err := o.Insert(t.Context(), ex); err != nil {
			t.Fatalf("seed organization: %v", err)
		}
		g := group.Create()
		g.SetID(groups[i])
		g.SetOrganizationID(org)
		g.SetName("approvers")
		g.SetDescription("")
		g.SetRoles(runtime.JSON("[]"))
		if _, err := g.Insert(t.Context(), ex); err != nil {
			t.Fatalf("seed group: %v", err)
		}
		user := models.UserModel{Base: models.Base{ID: models.FromUUID(users[i])}, Username: names[i]}
		if err := repo.Create(t.Context(), user, "hash"); err != nil {
			t.Fatalf("seed user: %v", err)
		}
		if err := repo.AddOrganization(t.Context(), users[i], org); err != nil {
			t.Fatalf("seed membership: %v", err)
		}
	}
	return f
}

// The user and group directories are tenant-scoped even when no organization is
// named.
//
// Both used to apply their filter only when one was given, and both list
// endpoints accept an empty organization_id — so a caller could read every
// account and group in the installation. The user list preloads organizations
// and projects, so that answer included each account's memberships too: the
// whole directory of an installation, not a count of it.
func TestTenantIsolation_DirectoryListsAreScoped(t *testing.T) {
	db := testutils.SetupTestDB(t)
	f := seedDirectoryFixture(t, db)
	ctx := entities.WithTenantContext(t.Context(), entities.TenantContext{TenantID: f.orgA.String()})

	t.Run("groups, no organization named", func(t *testing.T) {
		got, err := pg.NewGroupRepository(db).List(ctx, uuid.Nil)
		if err != nil {
			t.Fatalf("list groups: %v", err)
		}
		assertSameIDs(t, idsOf(got, func(m models.GroupModel) uuid.UUID { return uuid.UUID(m.ID) }),
			[]uuid.UUID{f.groupA})
	})

	t.Run("users, no organization named", func(t *testing.T) {
		got, err := pg.NewUserRepository(db).ListByOrganization(ctx, uuid.Nil)
		if err != nil {
			t.Fatalf("list users: %v", err)
		}
		assertSameIDs(t, idsOf(got, func(m models.UserModel) uuid.UUID { return uuid.UUID(m.ID) }),
			[]uuid.UUID{f.userA})
	})

	// Naming the caller's own organization still works: the scope is added to
	// the filter, not swapped for it.
	t.Run("users, own organization named", func(t *testing.T) {
		got, err := pg.NewUserRepository(db).ListByOrganization(ctx, f.orgA)
		if err != nil {
			t.Fatalf("list users in own organization: %v", err)
		}
		assertSameIDs(t, idsOf(got, func(m models.UserModel) uuid.UUID { return uuid.UUID(m.ID) }),
			[]uuid.UUID{f.userA})
	})

	// Naming somebody else's organization returns nothing rather than theirs.
	t.Run("users, foreign organization named", func(t *testing.T) {
		got, err := pg.NewUserRepository(db).ListByOrganization(ctx, f.orgB)
		if err != nil {
			t.Fatalf("list users in a foreign organization: %v", err)
		}
		if len(got) != 0 {
			t.Fatalf("organization A must not read organization B's accounts, got %d", len(got))
		}
	})

	t.Run("groups, foreign organization named", func(t *testing.T) {
		got, err := pg.NewGroupRepository(db).List(ctx, f.orgB)
		if err != nil {
			t.Fatalf("list groups in a foreign organization: %v", err)
		}
		if len(got) != 0 {
			t.Fatalf("organization A must not read organization B's groups, got %d", len(got))
		}
	})
}
