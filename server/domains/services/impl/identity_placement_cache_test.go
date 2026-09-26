package impl

import (
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/gsoultan/metis/server/domains/entities"
)

func signInClaims(organizations ...string) entities.IdentityClaims {
	return entities.IdentityClaims{
		Issuer:               "https://id.example.com",
		Subject:              "subject-1",
		OrganizationClaim:    "orgs",
		HasOrganizationClaim: true,
		Organizations:        organizations,
	}
}

// A placement is reused for the account cache's lifetime and no longer: the
// lifetime is how long a change nobody told this process about goes unseen.
func TestAPlacementIsReusedUntilItLapses(t *testing.T) {
	now := time.Now()
	c := newPlacementCache()
	c.now = fixedClock(&now)
	key := placementKeyOf(signInClaims("org-1"))
	placed := identityPlacement{account: uuid.New(), organizations: []uuid.UUID{uuid.New()}}
	c.put(key, placed, nil)

	now = now.Add(defaultPrincipalCacheTTL - time.Millisecond)
	got, ok := c.get(key)
	if !ok || got.placement.account != placed.account {
		t.Fatalf("the placement was not reused within its lifetime: %+v %v", got, ok)
	}
	now = now.Add(2 * time.Millisecond)
	if _, ok := c.get(key); ok {
		t.Fatal("the placement outlived its lifetime")
	}
}

// A token whose claim changed is placed afresh at once, not when the old
// placement lapses — that is what keeps an organization the provider stopped
// naming from being reached for a few seconds more.
func TestAChangedClaimIsADifferentPlacement(t *testing.T) {
	base := placementKeyOf(signInClaims("org-1", "org-2"))
	for name, claims := range map[string]entities.IdentityClaims{
		"an organization dropped":         signInClaims("org-1"),
		"the same organizations reversed": signInClaims("org-2", "org-1"),
		"values that run together":        signInClaims("org-1org-2"),
		"another subject":                 func() entities.IdentityClaims { c := signInClaims("org-1", "org-2"); c.Subject = "subject-2"; return c }(),
		"the claim gone":                  func() entities.IdentityClaims { c := signInClaims(); c.HasOrganizationClaim = false; return c }(),
	} {
		if placementKeyOf(claims) == base {
			t.Errorf("%s digests to the same placement", name)
		}
	}
	if placementKeyOf(signInClaims("org-1", "org-2")) != base {
		t.Error("the same claims digest differently")
	}
}

// A refusal is remembered as a refusal, so somebody retrying is neither looked
// up again nor logged again on every request.
func TestARefusalIsRemembered(t *testing.T) {
	c := newPlacementCache()
	key := placementKeyOf(signInClaims())
	refusal := errors.New("refused")
	c.put(key, identityPlacement{}, refusal)

	got, ok := c.get(key)
	if !ok || !errors.Is(got.refusal, refusal) {
		t.Fatalf("the refusal was not remembered: %+v %v", got, ok)
	}
}

func TestAZeroLifetimeRemembersNothing(t *testing.T) {
	t.Setenv(envPrincipalCacheTTL, "0s")
	c := newPlacementCache()
	key := placementKeyOf(signInClaims("org-1"))
	c.put(key, identityPlacement{account: uuid.New()}, nil)
	if _, ok := c.get(key); ok {
		t.Fatal("a zero lifetime still cached a placement")
	}
}
