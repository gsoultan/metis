package auth_test

import (
	"crypto/rand"
	"crypto/rsa"
	"encoding/json"
	"maps"
	"net/http"
	"slices"
	"testing"
	"time"

	"github.com/coreos/go-oidc/v3/oidc"
	"github.com/coreos/go-oidc/v3/oidc/oidctest"
	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
	"github.com/gsoultan/metis/server/domains/entities"
)

// While OIDC is on, a request may carry either kind of bearer token: the one
// /api/v1/login mints for a local account, or an ID token from the identity
// provider. Each is checked by its own rules and nothing else — a token that
// says it is the provider's is judged by the provider's rules alone, and is
// not taken as a local one when those refuse it.
//
// Before, the API took the provider's tokens only, so an installation that
// turned OIDC on lost its break-glass administrator whenever the provider was
// unreachable: a local account's token was a 401.
func TestEachKindOfTokenIsCheckedByItsOwnRules(t *testing.T) {
	cases := []struct {
		name string
		mint func(s tokenScene) string
		// The status a request carrying it is answered with, with OIDC
		// configured and without. A 200 must also have reached Acme's
		// projects and nobody else's.
		withOIDC, withoutOIDC int
	}{
		{
			name:     "a local account's token, from signing in with its password",
			mint:     func(s tokenScene) string { return s.h.login(s.local.Username) },
			withOIDC: http.StatusOK, withoutOIDC: http.StatusOK,
		},
		{
			name: "an ID token from the identity provider",
			mint: func(s tokenScene) string {
				return s.provider.token(s.t, "subject-ada", map[string]any{
					"preferred_username": "ada",
					organizationClaim:    []string{s.acme.String()},
				})
			},
			withOIDC: http.StatusOK, withoutOIDC: http.StatusUnauthorized,
		},
		{
			// The control for the next case: signed with JWT_SECRET, for a
			// real account, naming no issuer — what /api/v1/login mints. So the
			// next case's refusal is its issuer's doing, and nothing else's.
			name:     "a token signed with JWT_SECRET, naming no issuer",
			mint:     func(s tokenScene) string { return s.signLocally(s.localClaims(nil)) },
			withOIDC: http.StatusOK, withoutOIDC: http.StatusOK,
		},
		{
			// It says it is the provider's, so it is judged by the provider's
			// rules, which have no shared secret in them — and not retried as a
			// local token when they refuse it, which the local rules alone
			// would take. Without OIDC there is no provider to impersonate, and
			// the local rules, which have never read an issuer, apply as they
			// always have: only a holder of JWT_SECRET can make this token, and
			// they can make any local token.
			name: "a token signed with JWT_SECRET that names the provider as its issuer",
			mint: func(s tokenScene) string {
				return s.signLocally(s.localClaims(jwt.MapClaims{"iss": s.provider.issuer(), "aud": oidcClientID}))
			},
			withOIDC: http.StatusUnauthorized, withoutOIDC: http.StatusOK,
		},
		{
			name: "an ID token signed by a key the provider does not publish",
			mint: func(s tokenScene) string {
				return s.signAsImpostor(map[string]any{
					"preferred_username": "ada",
					organizationClaim:    []string{s.acme.String()},
				})
			},
			withOIDC: http.StatusUnauthorized, withoutOIDC: http.StatusUnauthorized,
		},
		{
			name: "a local token that has expired",
			mint: func(s tokenScene) string {
				issued := time.Now().Add(-25 * time.Hour)
				return s.signLocally(s.localClaims(jwt.MapClaims{
					"iat": issued.Unix(),
					"exp": issued.Add(24 * time.Hour).Unix(),
				}))
			},
			withOIDC: http.StatusUnauthorized, withoutOIDC: http.StatusUnauthorized,
		},
		{
			name: "an unsigned token (alg none) for the local account",
			mint: func(s tokenScene) string {
				token, err := jwt.NewWithClaims(jwt.SigningMethodNone, s.localClaims(nil)).
					SignedString(jwt.UnsafeAllowNoneSignatureType)
				if err != nil {
					s.t.Fatalf("make an unsigned token: %v", err)
				}
				return token
			},
			withOIDC: http.StatusUnauthorized, withoutOIDC: http.StatusUnauthorized,
		},
	}

	for _, mode := range []struct {
		name string
		oidc bool
	}{{"with OIDC configured", true}, {"without OIDC", false}} {
		t.Run(mode.name, func(t *testing.T) {
			s := newTokenScene(t, mode.oidc)
			// Not subtests: the harness reports through the test it was built for.
			for _, tc := range cases {
				want := tc.withoutOIDC
				if mode.oidc {
					want = tc.withOIDC
				}
				token := tc.mint(s)
				status, body := s.h.get("/api/v1/projects", token, "")
				if status != want {
					t.Errorf("%s: status %d (%s), want %d", tc.name, status, body, want)
					continue
				}
				if want == http.StatusOK && !slices.Equal(s.h.projects(token, ""), []string{"Acme Project"}) {
					t.Errorf("%s: reached %v, want Acme's projects alone", tc.name, s.h.projects(token, ""))
				}
			}
		})
	}
}

// tokenScene is an API, an organization with a local account in it, and an
// identity provider: the one the API trusts when OIDC is configured, and one
// it has never heard of when it is not.
type tokenScene struct {
	t        *testing.T
	h        *apiHarness
	provider *identityProvider
	acme     uuid.UUID
	local    entities.User
}

func newTokenScene(t *testing.T, withOIDC bool) tokenScene {
	t.Helper()
	s := tokenScene{t: t}
	if withOIDC {
		s.h = newOIDCHarness(t, organizationClaim)
		s.provider = s.h.provider
	} else {
		s.h = newLocalHarness(t)
		s.provider = newIdentityProvider(t)
	}
	s.acme = s.h.organization("Acme")
	s.h.organization("Globex")
	s.local = s.h.localAccount("hopper", "hopper@acme.example", s.acme)
	return s
}

// localClaims are the claims /api/v1/login puts in the local account's token,
// with overrides on top.
func (s tokenScene) localClaims(overrides jwt.MapClaims) jwt.MapClaims {
	now := time.Now()
	claims := jwt.MapClaims{
		"sub":      s.local.ID.String(),
		"username": s.local.Username,
		"roles":    s.local.Roles,
		"iat":      now.Unix(),
		"exp":      now.Add(time.Hour).Unix(),
	}
	maps.Copy(claims, overrides)
	return claims
}

// signLocally signs claims as /api/v1/login does: HS256, with JWT_SECRET.
func (s tokenScene) signLocally(claims jwt.MapClaims) string {
	s.t.Helper()
	token, err := jwt.NewWithClaims(jwt.SigningMethodHS256, claims).SignedString([]byte(localTokenSecret))
	if err != nil {
		s.t.Fatalf("sign a local token: %v", err)
	}
	return token
}

// signAsImpostor makes an ID token the provider did not sign: its issuer, its
// audience and the key id it publishes, signed with a key of somebody else's.
func (s tokenScene) signAsImpostor(claims map[string]any) string {
	s.t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		s.t.Fatalf("generate the impostor's key: %v", err)
	}
	now := time.Now()
	body := map[string]any{
		"iss": s.provider.issuer(),
		"aud": oidcClientID,
		"sub": "subject-impostor",
		"iat": now.Unix(),
		"exp": now.Add(time.Hour).Unix(),
	}
	maps.Copy(body, claims)
	raw, err := json.Marshal(body)
	if err != nil {
		s.t.Fatalf("encode the impostor's claims: %v", err)
	}
	return oidctest.SignIDToken(key, oidcKeyID, oidc.RS256, string(raw))
}
