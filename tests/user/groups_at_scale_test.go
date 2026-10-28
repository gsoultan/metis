package user_test

import (
	"fmt"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/gsoultan/metis/server/repositories/store/group"
	"github.com/gsoultan/metis/server/repositories/store/membership"
	"github.com/gsoultan/metis/server/repositories/store/userorganization"
	"github.com/gsoultan/metis/tests/testutils"
	"github.com/gsoultan/storm/runtime"
)

// A group's member list shows everybody in it.
//
// It read the memberships, and then the accounts, through queries the store
// caps at a thousand rows. A group of everyone in a large department listed a
// thousand of them, and nobody past the window could be seen or taken out of
// the group from that list.
func TestAGroupListsEveryOneOfItsMembers(t *testing.T) {
	w := newGroupWorld(t)
	w.seedMembers(t, w.orgA, membersAheadOfTheSecondAdministrator)
	ex := testutils.Executor(w.db)
	everybody, err := userorganization.New().OrganizationIDEq(w.orgA).Limit(2*membersAheadOfTheSecondAdministrator).
		All(t.Context(), ex, nil)
	if err != nil {
		t.Fatalf("read the organization's members: %v", err)
	}
	joined := make([]membership.Row, len(everybody))
	for i, member := range everybody {
		joined[i] = membership.Row{UserID: member.UserID, GroupID: w.groupA}
	}
	if _, err := membership.InsertAll(t.Context(), ex, joined); err != nil {
		t.Fatalf("put the organization in the group: %v", err)
	}
	want := membersAheadOfTheSecondAdministrator + 2

	members, err := w.groups.ListGroupMembers(actingAs(t, w.adminA, w.orgA), w.groupA)
	if err != nil {
		t.Fatalf("list the group's members: %v", err)
	}
	if len(members) != want {
		t.Fatalf("the group lists %d of its %d members", len(members), want)
	}
}

// An organization's group list shows every group it has, by the same reasoning.
func TestAnOrganizationListsEveryOneOfItsGroups(t *testing.T) {
	w := newGroupWorld(t)
	now := time.Now()
	teams := make([]group.Row, membersAheadOfTheSecondAdministrator)
	for i := range teams {
		teams[i] = group.Row{
			ID: uuid.New(), CreatedAt: now, UpdatedAt: now, OrganizationID: w.orgA,
			Name: fmt.Sprintf("Team %05d", i+1), Roles: runtime.JSON("[]"),
		}
	}
	if _, err := group.InsertAll(t.Context(), testutils.Executor(w.db), teams); err != nil {
		t.Fatalf("seed %d groups: %v", membersAheadOfTheSecondAdministrator, err)
	}
	want := membersAheadOfTheSecondAdministrator + 1

	groups, err := w.groups.ListGroups(actingAs(t, w.adminA, w.orgA), w.orgA)
	if err != nil {
		t.Fatalf("list groups: %v", err)
	}
	if len(groups) != want {
		t.Fatalf("the organization lists %d of its %d groups", len(groups), want)
	}
	for _, group := range groups {
		if group.Organization == nil || group.Organization.ID != w.orgA {
			t.Fatalf("group %q belongs to %v, not the organization listing it", group.Name, group.Organization)
		}
	}
}
