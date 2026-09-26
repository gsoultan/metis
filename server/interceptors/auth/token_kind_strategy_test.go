package auth

import (
	"context"
	"errors"
	"testing"

	"github.com/golang-jwt/jwt/v5"
	"github.com/gsoultan/metis/internal/pkg/apierr"
	pkgauth "github.com/gsoultan/metis/internal/pkg/auth"
)

// spyStrategy answers as it is told and counts the tokens it was shown.
type spyStrategy struct {
	principal any
	err       error
	shown     int
}

func (s *spyStrategy) Authenticate(context.Context, string) (any, error) {
	s.shown++
	return s.principal, s.err
}

// Each token is shown to the rules of the kind it says it is, and to no
// other: not before, and not after those refuse it. A token either set of
// rules would accept on its own is still refused when it says it is the other
// kind.
func TestATokenIsCheckedOnlyByTheRulesOfItsKind(t *testing.T) {
	local := signedHS256(t, jwt.SigningMethodHS256, jwt.MapClaims{"sub": "a-local-account"})
	localNamingIssuer := signedHS256(t, jwt.SigningMethodHS256, jwt.MapClaims{"sub": "a-local-account", "iss": testIssuer})
	idToken := compact(`{"alg":"RS256","kid":"k"}`, `{"iss":"`+testIssuer+`","aud":"metis","sub":"subject-ada"}`)
	refused := errors.New("refused by these rules")
	notPlaced := apierr.Forbiddenf("the claim places them in no organization here")

	cases := []struct {
		name                            string
		token                           string
		localErr, providerErr           error
		wantPrincipal                   any
		wantErr                         error
		wantShownLocally, wantShownToIP int
	}{
		{name: "a local token", token: local,
			wantPrincipal: "local account", wantShownLocally: 1},
		{name: "a local token its rules refuse is not shown to the provider's", token: local,
			localErr: refused, wantErr: refused, wantShownLocally: 1},
		{name: "an ID token", token: idToken,
			wantPrincipal: "linked account", wantShownToIP: 1},
		{name: "an ID token the provider's rules refuse is not retried as local", token: idToken,
			providerErr: refused, wantErr: refused, wantShownToIP: 1},
		{name: "an ID token whose claim places nobody keeps its 403", token: idToken,
			providerErr: notPlaced, wantErr: apierr.ErrForbidden, wantShownToIP: 1},
		{name: "an HMAC token naming an issuer is shown to neither", token: localNamingIssuer,
			wantErr: pkgauth.ErrUnauthorized},
		{name: "an unsigned token is shown to neither", token: compact(`{"alg":"none"}`, `{"sub":"a-local-account"}`),
			wantErr: pkgauth.ErrUnauthorized},
		{name: "something that is not a token is shown to neither", token: "opaque-token-value",
			wantErr: pkgauth.ErrUnauthorized},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			localRules := &spyStrategy{principal: "local account", err: tc.localErr}
			providerRules := &spyStrategy{principal: "linked account", err: tc.providerErr}
			if tc.localErr != nil {
				localRules.principal = nil
			}
			if tc.providerErr != nil {
				providerRules.principal = nil
			}

			principal, err := NewTokenKindStrategy(localRules, providerRules).Authenticate(t.Context(), tc.token)

			if localRules.shown != tc.wantShownLocally || providerRules.shown != tc.wantShownToIP {
				t.Fatalf("shown to the local rules %d time(s) and the provider's %d, want %d and %d",
					localRules.shown, providerRules.shown, tc.wantShownLocally, tc.wantShownToIP)
			}
			if tc.wantErr != nil {
				if !errors.Is(err, tc.wantErr) || principal != nil {
					t.Fatalf("got %v, %v; want no principal and %v", principal, err, tc.wantErr)
				}
				return
			}
			if err != nil || principal != tc.wantPrincipal {
				t.Fatalf("got %v, %v; want %v", principal, err, tc.wantPrincipal)
			}
		})
	}
}
