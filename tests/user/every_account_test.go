package user_test

import (
	"fmt"
	"slices"
	"testing"

	"github.com/google/uuid"
	"github.com/gsoultan/metis/server/domains/entities"
	"github.com/gsoultan/metis/server/repositories/models"
)

// largeOrganization is more accounts than one read of the store returns:
// storm starts every query with a limit of 1000.
const largeOrganization = 1050

// seededHash stands in for a password nobody signs in with. The accounts are
// written through the repository rather than CreateUser, which would spend a
// bcrypt hash on each of them.
const seededHash = "$2a$10$N9qo8uLOickgx2ZMRZoMyeIjZAgcfl7p92ldGxad68LJZdL17lhWy"

// fillOrganization adds count accounts to the organization, in username order.
func (w accountWorld) fillOrganization(t *testing.T, org uuid.UUID, count int) {
	t.Helper()
	ctx := entities.WithSystemContext(t.Context())
	for i := range count {
		username := fmt.Sprintf("member-%04d", i)
		account := models.UserModel{
			Base:          models.Base{ID: models.UUID(uuid.Must(uuid.NewV7()))},
			Username:      username,
			FullName:      username,
			Organizations: []models.OrganizationModel{{Base: models.Base{ID: models.UUID(org)}}},
		}
		if err := w.repo.User().Create(ctx, account, seededHash); err != nil {
			t.Fatalf("seed %s: %v", username, err)
		}
	}
}

// The Users page and the role matrix list an organization's accounts, and the
// matrix says "Showing all N accounts". The list was read through two queries
// the store caps at a thousand rows — the memberships, in no order, then the
// accounts — so past a thousand the rest were missing with nothing to say so.
func TestEveryAccountInALargeOrganizationIsListed(t *testing.T) {
	w := newAccountWorld(t)
	w.fillOrganization(t, w.orgA, largeOrganization)

	listed, err := w.svc.ListUsers(actingAs(t, w.adminA, w.orgA), w.orgA)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	// admin-a and user-a, and the members added here.
	if want := largeOrganization + 2; len(listed) != want {
		t.Fatalf("the organization has %d accounts and the list shows %d", want, len(listed))
	}
	usernames := make([]string, 0, len(listed))
	for _, account := range listed {
		usernames = append(usernames, account.Username)
	}
	if !slices.IsSorted(usernames) {
		t.Errorf("the accounts are not in username order: %v …", usernames[:5])
	}
	if slices.Contains(usernames, "admin-b") || slices.Contains(usernames, "user-b") {
		t.Error("another organization's accounts are in the list")
	}
}

// The last-administrator guard counts from the same list, so an administrator
// the list left out did not count: with another administrator present, taking
// the role from the first was refused as leaving the organization with none.
func TestAnAdministratorPastTheFirstThousandStillCounts(t *testing.T) {
	w := newAccountWorld(t)
	w.fillOrganization(t, w.orgA, largeOrganization)
	// Joins after everybody else, so it is the last membership read.
	w.seedAccount(t, "admin-late", []string{entities.RoleAdmin}, w.orgA)

	err := w.svc.UpdateUser(actingAs(t, w.adminA, w.orgA), entities.User{
		ID: w.adminA.ID, Username: "admin-a", Roles: []string{entities.RoleDesigner},
	})
	if err != nil {
		t.Fatalf("with another administrator in the organization, the first may stop being one: %v", err)
	}
}
