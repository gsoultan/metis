package user_test

import (
	"testing"

	"github.com/gsoultan/metis/server/domains/entities"
)

// The platform accounts page lists every account with the roles it holds.
//
// It read the accounts, and every role grant in the installation, through
// queries the store caps at a thousand rows. Past a thousand accounts the ones
// after the window were missing; past a thousand grants the accounts that were
// listed showed no roles, an administrator among them.
func TestThePlatformAccountListShowsEveryAccountWithItsRoles(t *testing.T) {
	w := newPlatformWorld(t)
	w.seedDesigners(t, grantsAheadOfTheAdministrator)
	admin := w.createAdministrator(t, "the-administrator")

	accounts, err := w.svc.ListPlatformUsers(w.ctx)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(accounts) != grantsAheadOfTheAdministrator+1 {
		t.Errorf("the list shows %d of %d accounts", len(accounts), grantsAheadOfTheAdministrator+1)
	}
	for _, account := range accounts {
		if account.ID == admin {
			if !entities.HasRole(account.Roles, entities.RoleAdmin) {
				t.Fatalf("the administrator is listed with roles %v", account.Roles)
			}
			return
		}
	}
	t.Fatal("the administrator is not in the list")
}
