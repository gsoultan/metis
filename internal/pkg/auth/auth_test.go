package auth

import (
	"crypto/rand"
	"crypto/rsa"
	"encoding/json"
	"net/http/httptest"
	"slices"
	"testing"
	"time"

	"github.com/coreos/go-oidc/v3/oidc"
	"github.com/coreos/go-oidc/v3/oidc/oidctest"
)

const testClientID = "metis"

// signer is a fake issuer: go-oidc's test server and the key it advertises.
type signer struct {
	issuer string
	key    *rsa.PrivateKey
}

func newSigner(t *testing.T) signer {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("generate a key: %v", err)
	}
	fake := &oidctest.Server{PublicKeys: []oidctest.PublicKey{
		{PublicKey: key.Public(), KeyID: "k", Algorithm: oidc.RS256},
	}}
	server := httptest.NewServer(fake)
	t.Cleanup(server.Close)
	fake.SetIssuer(server.URL)
	return signer{issuer: server.URL, key: key}
}

func (s signer) sign(t *testing.T, claims map[string]any) string {
	t.Helper()
	body := map[string]any{
		"iss": s.issuer, "aud": testClientID, "sub": "subject-1",
		"iat": time.Now().Unix(), "exp": time.Now().Add(time.Hour).Unix(),
	}
	for k, v := range claims {
		body[k] = v
	}
	raw, err := json.Marshal(body)
	if err != nil {
		t.Fatalf("encode claims: %v", err)
	}
	return oidctest.SignIDToken(s.key, "k", oidc.RS256, string(raw))
}

// The organization claim may be a string or a list of strings, under whatever
// name the operator configures — a provider's custom claim is often a URL, and
// its dots are part of the name, not a path.
func TestTheOrganizationClaimIsReadAsTheOperatorNamedIt(t *testing.T) {
	const urlClaim = "https://metis.example.com/organizations"
	cases := []struct {
		name        string
		claim       string
		value       any
		omit        bool
		wantPresent bool
		want        []string
	}{
		{name: "a string", claim: "orgs", value: "org-1", wantPresent: true, want: []string{"org-1"}},
		{name: "a list", claim: "orgs", value: []string{"org-1", " org-2 "}, wantPresent: true, want: []string{"org-1", "org-2"}},
		{name: "a list with things that are not strings", claim: "orgs",
			value: []any{"org-1", 7, nil, map[string]any{"id": "org-2"}, ""}, wantPresent: true, want: []string{"org-1"}},
		{name: "a number", claim: "orgs", value: 42, wantPresent: true},
		{name: "null", claim: "orgs", value: nil, wantPresent: true},
		{name: "missing", claim: "orgs", omit: true},
		{name: "a URL for a name", claim: urlClaim, value: []string{"org-1"}, wantPresent: true, want: []string{"org-1"}},
	}
	issuer := newSigner(t)
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv(EnvOrganizationClaim, tc.claim)
			validator, err := NewTokenValidator(t.Context(), issuer.issuer, testClientID)
			if err != nil {
				t.Fatalf("validator: %v", err)
			}
			extra := map[string]any{"preferred_username": "ada"}
			if !tc.omit {
				extra[tc.claim] = tc.value
			}
			claims, err := validator.ValidateToken(t.Context(), issuer.sign(t, extra))
			if err != nil {
				t.Fatalf("validate: %v", err)
			}
			if claims.Issuer != issuer.issuer || claims.Subject != "subject-1" || claims.Username != "ada" {
				t.Fatalf("identity = %q/%q/%q", claims.Issuer, claims.Subject, claims.Username)
			}
			if claims.OrganizationClaim != tc.claim || claims.HasOrganizationClaim != tc.wantPresent {
				t.Fatalf("claim %q present=%v, want %q present=%v",
					claims.OrganizationClaim, claims.HasOrganizationClaim, tc.claim, tc.wantPresent)
			}
			if !slices.Equal(claims.Organizations, tc.want) && len(claims.Organizations)+len(tc.want) > 0 {
				t.Fatalf("organizations = %q, want %q", claims.Organizations, tc.want)
			}
		})
	}
}

// With the setting unset nothing is read as organizations, whatever the token
// carries: the sign-in then refuses, naming the setting.
func TestWithNoOrganizationClaimConfiguredNoneIsRead(t *testing.T) {
	t.Setenv(EnvOrganizationClaim, "")
	issuer := newSigner(t)
	validator, err := NewTokenValidator(t.Context(), issuer.issuer, testClientID)
	if err != nil {
		t.Fatalf("validator: %v", err)
	}
	claims, err := validator.ValidateToken(t.Context(), issuer.sign(t, map[string]any{"orgs": "org-1"}))
	if err != nil {
		t.Fatalf("validate: %v", err)
	}
	if claims.OrganizationClaim != "" || claims.HasOrganizationClaim || len(claims.Organizations) != 0 {
		t.Fatalf("read organizations with no claim configured: %+v", claims)
	}
}

// A token the provider did not sign is not evidence of anything.
func TestATokenSignedByAnotherKeyIsRefused(t *testing.T) {
	t.Setenv(EnvOrganizationClaim, "orgs")
	issuer := newSigner(t)
	impostor := newSigner(t)
	impostor.issuer = issuer.issuer
	validator, err := NewTokenValidator(t.Context(), issuer.issuer, testClientID)
	if err != nil {
		t.Fatalf("validator: %v", err)
	}
	if _, err := validator.ValidateToken(t.Context(), impostor.sign(t, map[string]any{"orgs": "org-1"})); err == nil {
		t.Fatal("a token signed with a key the issuer does not publish was accepted")
	}
}
