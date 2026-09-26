package user_test

import "testing"

// A group's member list shows everybody in it.
//
// It read the memberships, and then the accounts, through queries the store
// caps at a thousand rows. A group of everyone in a large department listed a
// thousand of them, and nobody past the window could be seen or taken out of
// the group from that list.
func TestAGroupListsEveryOneOfItsMembers(t *testing.T) {
	w := newGroupWorld(t)
	w.seedMembers(t, w.orgA, membersAheadOfTheSecondAdministrator)
	if err := w.db.WithContext(t.Context()).Exec(`
		INSERT INTO memberships (user_id, group_id)
		SELECT m.user_id, ? FROM user_organizations m WHERE m.organization_id = ?`, w.groupA, w.orgA).Error; err != nil {
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
	if err := w.db.WithContext(t.Context()).Exec(`
		INSERT INTO groups (id, created_at, updated_at, organization_id, name, description, roles)
		SELECT gen_random_uuid(), now(), now(), ?, format('Team %s', lpad(n::text, 5, '0')), '', '[]'
		  FROM generate_series(1, ?) AS n`, w.orgA, membersAheadOfTheSecondAdministrator).Error; err != nil {
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
