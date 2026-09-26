package user_test

import (
	"errors"
	"fmt"
	"testing"

	"github.com/google/uuid"
	"github.com/gsoultan/metis/internal/pkg/apierr"
	"github.com/gsoultan/metis/server/domains/entities"
)

// membersAheadOfTheSecondAdministrator is as many members as one read of the
// generated store returns: storm starts every query with a limit of 1000.
const membersAheadOfTheSecondAdministrator = 1000

// seedMembers adds n ordinary members to an organization in one statement.
//
// Usernames sort ahead of anything this file names an administrator, so a
// read that stops at a thousand stops before the administrator does.
func (w accountWorld) seedMembers(t *testing.T, organizationID uuid.UUID, n int) {
	t.Helper()
	if err := w.db.WithContext(t.Context()).Exec(`
		WITH seeded AS (
			INSERT INTO users (id, created_at, updated_at, username, password_hash,
			                   full_name, display_name, organization, email, roles)
			SELECT gen_random_uuid(), now(), now(), format('member-%s', lpad(n::text, 5, '0')), '',
			       '', '', '', '', '["USER"]'
			  FROM generate_series(1, ?) AS n
			RETURNING id)
		INSERT INTO user_organizations (user_id, organization_id)
		SELECT id, ? FROM seeded`, n, organizationID).Error; err != nil {
		t.Fatalf("seed %d members: %v", n, err)
	}
}

// The last-administrator guard answers "is anybody else an administrator here",
// and in an organization of more than a thousand people it has to answer it
// about all of them.
//
// It read the members through a query the store caps at a thousand rows, so an
// administrator who joined after the first thousand members was not there to
// be found: taking the role from one of two administrators was refused as
// though it were the last. The refusal of the real last one must survive the
// fix, which is the second half.
func TestTheLastAdministratorGuardSeesPastTheFirstThousandMembers(t *testing.T) {
	w := newAccountWorld(t)
	w.seedMembers(t, w.orgA, membersAheadOfTheSecondAdministrator)
	second := w.seedAccount(t, "zz-second-admin", []string{entities.RoleAdmin}, w.orgA)

	if err := w.svc.UpdateUser(actingAs(t, w.adminA, w.orgA), entities.User{
		ID: w.adminA.ID, Username: "admin-a", Roles: []string{entities.RoleDesigner},
	}); err != nil {
		t.Fatalf("demoting one of two administrators of an organization with %d members was refused: %v",
			membersAheadOfTheSecondAdministrator+3, err)
	}

	err := w.svc.DeleteUser(actingAs(t, second, w.orgA), second.ID)
	if !errors.Is(err, apierr.ErrForbidden) {
		t.Fatalf("deleting the last administrator of an organization with %d members must be refused, got %v",
			membersAheadOfTheSecondAdministrator+3, err)
	}
	if _, err := w.stored(t, second.ID); err != nil {
		t.Fatalf("the last administrator is gone after a refused delete: %v", err)
	}
}

// The guard's question is answered in SQL now, and it must recognise an
// administrator exactly as entities.HasRole does: in any letter case, and not
// a role that merely contains the word.
func TestAnotherAdministratorIsRecognisedTheWayHasRoleRecognisesOne(t *testing.T) {
	w := newAccountWorld(t)
	cases := []struct {
		roles  []string
		counts bool
	}{
		{[]string{entities.RoleAdmin}, true},
		{[]string{"admin"}, true},
		{[]string{entities.RoleUser, "Admin"}, true},
		{[]string{"ADMINISTRATOR"}, false},
		{[]string{"SYSADMIN"}, false},
		{[]string{entities.RoleUser}, false},
		{nil, false},
	}
	for i, tc := range cases {
		organizationID := seedOrganization(t, w.repo, fmt.Sprintf("Organization %d", i))
		w.seedAccount(t, fmt.Sprintf("member-of-%d", i), tc.roles, organizationID)
		ctx := entities.WithTenantContext(t.Context(), entities.TenantContext{TenantID: organizationID.String()})

		another, err := w.repo.User().HasAnotherAdministrator(ctx, organizationID, w.adminA.ID)
		if err != nil {
			t.Fatalf("roles %q: %v", tc.roles, err)
		}
		if another != tc.counts {
			t.Errorf("an account with roles %q counts as another administrator: %v, want %v", tc.roles, another, tc.counts)
		}
	}
}
