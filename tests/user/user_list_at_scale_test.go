package user_test

import "testing"

// An organization's user list shows every member, each with the organizations
// they belong to.
//
// It read the memberships, and then the accounts, through queries the store
// caps at a thousand rows, so an organization of more than a thousand people
// listed a thousand of them.
func TestTheUserListShowsEveryMemberOfALargeOrganization(t *testing.T) {
	w := newAccountWorld(t)
	w.seedMembers(t, w.orgA, membersAheadOfTheSecondAdministrator)
	want := membersAheadOfTheSecondAdministrator + 2

	users, err := w.svc.ListUsers(actingAs(t, w.adminA, w.orgA), w.orgA)
	if err != nil {
		t.Fatalf("list users: %v", err)
	}
	if len(users) != want {
		t.Fatalf("the user list shows %d of the organization's %d members", len(users), want)
	}
	for _, u := range users {
		if len(u.Organizations) != 1 || u.Organizations[0].ID != w.orgA {
			t.Fatalf("%s is listed with organizations %v; they belong to one", u.Username, u.Organizations)
		}
	}
}
