package auth_test

import (
	"slices"
	"sync"
	"testing"

	"github.com/google/uuid"
	"github.com/gsoultan/metis/server/domains/entities"
	"github.com/gsoultan/metis/server/repositories/db"
)

// An administrator deleting a linked account ends that account. The person's
// next sign-in — the provider still vouching for them — is given a new one,
// under a new username: usernames are never reissued, so nothing of the old
// account's history is inherited.
func TestADeletedAccountsNextSignInIsGivenANewOne(t *testing.T) {
	h := newOIDCHarness(t, organizationClaim)
	acme := h.organization("Acme")
	claims := map[string]any{"preferred_username": "barbara", organizationClaim: acme.String()}

	before := h.me(h.provider.token(t, "subject-barbara", claims))
	if err := h.svc.DeleteUser(inOrganization(t.Context(), acme), uuid.MustParse(before.ID)); err != nil {
		t.Fatalf("delete Barbara's account: %v", err)
	}

	after := h.me(h.provider.token(t, "subject-barbara", claims))
	if after.ID == before.ID {
		t.Fatal("the deleted account was signed in to again")
	}
	if after.Username == before.Username {
		t.Fatalf("the new account reuses the deleted one's username %q", before.Username)
	}
	if got := h.members(acme); !slices.Equal(got, []string{after.Username}) {
		t.Fatalf("Acme's members are %v, want the new account alone", got)
	}
}

// Several requests carrying one person's first token arrive at once — a
// browser opening its tabs. They race to create the account, and the unique
// index on the identity decides the race: one account, which every request
// signs in as.
func TestConcurrentFirstSignInsMakeOneAccount(t *testing.T) {
	h := newOIDCHarness(t, organizationClaim)
	acme := h.organization("Acme")
	claims := identityClaims(h, "subject-katherine", "katherine", acme)

	const requests = 8
	ids := make([]uuid.UUID, requests)
	errs := make([]error, requests)
	var wg sync.WaitGroup
	for i := range requests {
		wg.Go(func() {
			account, err := h.svc.SignInThroughIdentityProvider(t.Context(), claims)
			ids[i], errs[i] = account.ID, err
		})
	}
	wg.Wait()

	for i, err := range errs {
		if err != nil {
			t.Fatalf("sign-in %d: %v", i, err)
		}
		if ids[i] != ids[0] {
			t.Fatalf("sign-ins reached different accounts: %v", ids)
		}
	}
	if got := h.members(acme); len(got) != 1 {
		t.Fatalf("Acme holds %v, want one account", got)
	}
}

// A request that arrives on an environment's port is bound to that
// environment's database, and accounts do not live there: the sign-in writes
// the account and its memberships to the main database, in one transaction.
// The environment here has no database at all, so anything written through
// its binding fails.
func TestASignInOnAnEnvironmentsPortWritesToTheMainDatabase(t *testing.T) {
	h := newOIDCHarness(t, organizationClaim)
	acme := h.organization("Acme")

	onStaging := db.Bind(t.Context(), uuid.New())
	account, err := h.svc.SignInThroughIdentityProvider(onStaging,
		identityClaims(h, "subject-margaret", "margaret", acme))
	if err != nil {
		t.Fatalf("sign in on an environment's port: %v", err)
	}
	if got := h.members(acme); !slices.Equal(got, []string{account.Username}) {
		t.Fatalf("Acme's members are %v, want %s", got, account.Username)
	}
}

// identityClaims is what the validator makes of a token naming org in the
// organization claim.
func identityClaims(h *apiHarness, subject, username string, org uuid.UUID) entities.IdentityClaims {
	return entities.IdentityClaims{
		Issuer:               h.provider.issuer(),
		Subject:              subject,
		Username:             username,
		OrganizationClaim:    organizationClaim,
		HasOrganizationClaim: true,
		Organizations:        []string{org.String()},
	}
}
