package user_test

import (
	"errors"
	"slices"
	"testing"

	"github.com/google/uuid"
	"github.com/gsoultan/metis/internal/pkg/apierr"
	"github.com/gsoultan/metis/server/domains/entities"
	"github.com/gsoultan/metis/server/domains/services/contracts"
	service_impl "github.com/gsoultan/metis/server/domains/services/impl"
)

// Group membership crossed organizations. Adding somebody to a group went
// straight to the repository, which checks that the group is the caller's and
// never looks at the account — so an administrator of one organization could
// put another organization's account into one of their groups, then read that
// account's name and email back from the member list.

type groupWorld struct {
	accountWorld
	groups contracts.GroupService
	groupA uuid.UUID
	groupB uuid.UUID
}

func newGroupWorld(t *testing.T) groupWorld {
	t.Helper()
	w := groupWorld{accountWorld: newAccountWorld(t)}
	w.groups = service_impl.NewGroupService(w.repo)
	w.groupA = w.seedGroup(t, "Approvers A", w.orgA)
	w.groupB = w.seedGroup(t, "Approvers B", w.orgB)
	return w
}

func (w groupWorld) seedGroup(t *testing.T, name string, org uuid.UUID) uuid.UUID {
	t.Helper()
	id := uuid.Must(uuid.NewV7())
	if err := w.groups.CreateGroup(entities.WithSystemContext(t.Context()), entities.Group{
		ID:           id,
		Organization: &entities.Organization{ID: org},
		Name:         name,
	}); err != nil {
		t.Fatalf("seed %s: %v", name, err)
	}
	return id
}

// members reads a group's members as the system, so what the test sees does
// not depend on the check under test.
func (w groupWorld) members(t *testing.T, groupID uuid.UUID) []uuid.UUID {
	t.Helper()
	users, err := w.groups.ListGroupMembers(entities.WithSystemContext(t.Context()), groupID)
	if err != nil {
		t.Fatalf("list the members of %s: %v", groupID, err)
	}
	ids := make([]uuid.UUID, 0, len(users))
	for _, u := range users {
		ids = append(ids, u.ID)
	}
	return ids
}

func TestAMembershipCannotCrossOrganizations(t *testing.T) {
	w := newGroupWorld(t)

	cases := []struct {
		name  string
		user  uuid.UUID
		group uuid.UUID
	}{
		{"another organization's account into the caller's group", w.userB.ID, w.groupA},
		{"the caller's account into another organization's group", w.userA.ID, w.groupB},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := w.groups.AddMembership(actingAs(t, w.adminA, w.orgA), tc.user, tc.group)
			if !errors.Is(err, apierr.ErrNotFound) {
				t.Fatalf("an administrator of organization A added %s: got %v, want not found", tc.name, err)
			}
			if slices.Contains(w.members(t, tc.group), tc.user) {
				t.Fatal("the refused membership was written anyway")
			}
		})
	}
}

func TestAMembershipWithinTheOrganizationStillWorks(t *testing.T) {
	w := newGroupWorld(t)
	asAdminA := actingAs(t, w.adminA, w.orgA)

	if err := w.groups.AddMembership(asAdminA, w.userA.ID, w.groupA); err != nil {
		t.Fatalf("adding an account to a group of its own organization: %v", err)
	}
	if !slices.Contains(w.members(t, w.groupA), w.userA.ID) {
		t.Fatal("the membership was accepted and not written")
	}

	if err := w.groups.RemoveMembership(asAdminA, w.userA.ID, w.groupA); err != nil {
		t.Fatalf("removing an account from a group of its own organization: %v", err)
	}
	if slices.Contains(w.members(t, w.groupA), w.userA.ID) {
		t.Fatal("the removal was accepted and the account is still a member")
	}
}

func TestAMembershipCannotBeRemovedFromAnotherOrganizationsGroup(t *testing.T) {
	w := newGroupWorld(t)
	if err := w.repo.Group().AddMembership(entities.WithSystemContext(t.Context()), w.userB.ID, w.groupB); err != nil {
		t.Fatalf("seed a membership in organization B: %v", err)
	}

	err := w.groups.RemoveMembership(actingAs(t, w.adminA, w.orgA), w.userB.ID, w.groupB)
	if !errors.Is(err, apierr.ErrNotFound) {
		t.Fatalf("an administrator of organization A removed somebody from organization B's group: got %v, want not found", err)
	}
	if !slices.Contains(w.members(t, w.groupB), w.userB.ID) {
		t.Fatal("the refused removal happened anyway")
	}
}

// A membership that already crosses organizations — added before adding one
// was refused — is a row in the caller's own group, and taking it apart is
// the only way to be rid of it. Removing somebody places them nowhere.
func TestAMembershipThatAlreadyCrossesOrganizationsCanBeRemoved(t *testing.T) {
	w := newGroupWorld(t)
	if err := w.repo.Group().AddMembership(entities.WithSystemContext(t.Context()), w.userB.ID, w.groupA); err != nil {
		t.Fatalf("seed a membership that crosses organizations: %v", err)
	}

	if err := w.groups.RemoveMembership(actingAs(t, w.adminA, w.orgA), w.userB.ID, w.groupA); err != nil {
		t.Fatalf("removing another organization's account from the caller's own group: %v", err)
	}
	if slices.Contains(w.members(t, w.groupA), w.userB.ID) {
		t.Fatal("the removal was accepted and the account is still a member")
	}
}
