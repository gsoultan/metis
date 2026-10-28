package user_test

import (
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/gsoultan/metis/internal/pkg/apierr"
	"github.com/gsoultan/metis/server/domains/entities"
	"github.com/gsoultan/metis/server/repositories/store/user"
	"github.com/gsoultan/metis/server/repositories/store/userorganization"
	"github.com/gsoultan/metis/tests/testutils"
	"github.com/gsoultan/storm/runtime"
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
	ex, now := testutils.Executor(w.db), time.Now()
	accounts := make([]user.Row, n)
	memberships := make([]userorganization.Row, n)
	for i := range accounts {
		accounts[i] = user.Row{
			ID: uuid.New(), CreatedAt: now, UpdatedAt: now, Username: fmt.Sprintf("member-%05d", i+1),
			Roles: runtime.JSON(`["USER"]`),
		}
		memberships[i] = userorganization.Row{UserID: accounts[i].ID, OrganizationID: organizationID, Roles: runtime.JSON("[]")}
	}
	if _, err := user.InsertAll(t.Context(), ex, accounts); err != nil {
		t.Fatalf("seed %d members: %v", n, err)
	}
	if _, err := userorganization.InsertAll(t.Context(), ex, memberships); err != nil {
		t.Fatalf("seed %d memberships: %v", n, err)
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
	// Named by the operator like the world's administrators, so the refusal
	// below is the guard's and not the platform's.
	secondID := uuid.Must(uuid.NewV7())
	w := newAccountWorld(t, secondID)
	w.seedMembers(t, w.orgA, membersAheadOfTheSecondAdministrator)
	second := w.seedAccountAs(t, secondID, "zz-second-admin", []string{entities.RoleAdmin}, w.orgA)

	if err := w.svc.UpdateUser(actingAs(t, w.adminA, w.orgA), entities.User{
		ID: w.adminA.ID, Username: "admin-a", Roles: []string{entities.RoleDesigner},
	}); err != nil {
		t.Fatalf("demoting one of two administrators of an organization with %d members was refused: %v",
			membersAheadOfTheSecondAdministrator+3, err)
	}

	err := w.svc.DeleteUser(actingAs(t, second, w.orgA), second.ID)
	if !errors.Is(err, apierr.ErrForbidden) || !strings.Contains(err.Error(), "last administrator") {
		t.Fatalf("deleting the last administrator of an organization with %d members must be refused as the last, got %v",
			membersAheadOfTheSecondAdministrator+3, err)
	}
	if _, err := w.stored(t, second.ID); err != nil {
		t.Fatalf("the last administrator is gone after a refused delete: %v", err)
	}
}

// The guard's question is answered in SQL now, and it must recognise an
// administrator exactly as entities.HasRole does: in any letter case, and not
// a role that merely contains the word — whether the role is held in every
// organization or in the one asked about alone.
func TestAnotherAdministratorIsRecognisedTheWayHasRoleRecognisesOne(t *testing.T) {
	w := newAccountWorld(t)
	cases := []struct {
		roles  []string
		here   []string
		counts bool
	}{
		{[]string{entities.RoleAdmin}, nil, true},
		{[]string{"admin"}, nil, true},
		{[]string{entities.RoleUser, "Admin"}, nil, true},
		{[]string{"ADMINISTRATOR"}, nil, false},
		{[]string{"SYSADMIN"}, nil, false},
		{[]string{entities.RoleUser}, nil, false},
		{nil, nil, false},
		{nil, []string{entities.RoleAdmin}, true},
		{nil, []string{"admin"}, true},
		{[]string{entities.RoleDesigner}, []string{entities.RoleOperator, "Admin"}, true},
		{nil, []string{"ADMINISTRATOR"}, false},
		{nil, []string{entities.RoleDesigner}, false},
	}
	for i, tc := range cases {
		organizationID := seedOrganization(t, w.repo, fmt.Sprintf("Organization %d", i))
		member := w.seedAccount(t, fmt.Sprintf("member-of-%d", i), tc.roles, organizationID)
		if tc.here != nil {
			w.holdsIn(t, member.ID, organizationID, tc.here...)
		}
		ctx := entities.WithTenantContext(t.Context(), entities.TenantContext{TenantID: organizationID.String()})

		another, err := w.repo.User().HasAnotherAdministrator(ctx, organizationID, w.adminA.ID)
		if err != nil {
			t.Fatalf("roles %q, %q here: %v", tc.roles, tc.here, err)
		}
		if another != tc.counts {
			t.Errorf("an account with roles %q, and %q here, counts as another administrator: %v, want %v",
				tc.roles, tc.here, another, tc.counts)
		}
	}

	// A role held in another organization is that organization's.
	elsewhere := seedOrganization(t, w.repo, "Elsewhere")
	visitor := w.seedAccount(t, "visitor", nil, w.orgB, elsewhere)
	w.holdsIn(t, visitor.ID, w.orgB, entities.RoleAdmin)
	ctx := entities.WithTenantContext(t.Context(), entities.TenantContext{TenantID: elsewhere.String()})
	if another, err := w.repo.User().HasAnotherAdministrator(ctx, elsewhere, w.adminA.ID); err != nil || another {
		t.Errorf("an administrator of another organization counts here: %v (%v)", another, err)
	}
}

// holdsIn puts roles on an account's membership of one organization.
func (w accountWorld) holdsIn(t *testing.T, account, organization uuid.UUID, roles ...string) {
	t.Helper()
	ctx := entities.WithTenantContext(t.Context(), entities.TenantContext{TenantID: organization.String()})
	if err := w.repo.User().SetOrganizationRoles(ctx, account, organization, roles); err != nil {
		t.Fatalf("give %s %v in %s: %v", account, roles, organization, err)
	}
}
