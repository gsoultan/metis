package auth

import (
	"encoding/base64"
	"testing"

	"github.com/golang-jwt/jwt/v5"
)

const testIssuer = "https://id.example.com/realms/acme"

// compact assembles a token from a header and a payload, as written, with a
// signature nobody checks here: the kind of a token is read before anything
// about it is verified.
func compact(header, payload string) string {
	encode := base64.RawURLEncoding.EncodeToString
	return encode([]byte(header)) + "." + encode([]byte(payload)) + "." + encode([]byte("signature"))
}

// signedHS256 is a token as /api/v1/login mints one, with claims on top.
func signedHS256(t *testing.T, method jwt.SigningMethod, claims jwt.MapClaims) string {
	t.Helper()
	token, err := jwt.NewWithClaims(method, claims).SignedString([]byte("a-secret-for-the-test"))
	if err != nil {
		t.Fatalf("sign: %v", err)
	}
	return token
}

// A token is sent to the rules of the kind it says it is — by the algorithm
// its header names and whether its payload names an issuer — and to no other.
// What does not say it is either kind is refused before either set of rules
// is consulted.
func TestATokenIsReadAsTheKindItSaysItIs(t *testing.T) {
	loginClaims := jwt.MapClaims{"sub": "0199a4c2-5e1b-7c3d-8f00-1a2b3c4d5e6f", "username": "hopper", "exp": 4102444800, "iat": 1700000000}
	namingIssuer := func(issuer any) jwt.MapClaims {
		claims := jwt.MapClaims{"iss": issuer}
		for k, v := range loginClaims {
			claims[k] = v
		}
		return claims
	}
	idToken := `{"iss":"` + testIssuer + `","aud":"metis","sub":"subject-ada","exp":4102444800,"iat":1700000000}`

	cases := []struct {
		name  string
		token string
		want  tokenKind
	}{
		{"a token /api/v1/login mints: HS256, naming no issuer", signedHS256(t, jwt.SigningMethodHS256, loginClaims), tokenLocal},
		{"HS384, naming no issuer", signedHS256(t, jwt.SigningMethodHS384, loginClaims), tokenLocal},
		{"HS512, naming no issuer", signedHS256(t, jwt.SigningMethodHS512, loginClaims), tokenLocal},
		{"HS256 naming the provider as its issuer", signedHS256(t, jwt.SigningMethodHS256, namingIssuer(testIssuer)), tokenUnrecognised},
		{"HS256 naming another issuer", signedHS256(t, jwt.SigningMethodHS256, namingIssuer("https://elsewhere.example")), tokenUnrecognised},
		{"HS256 naming an empty issuer", signedHS256(t, jwt.SigningMethodHS256, namingIssuer("")), tokenUnrecognised},
		{"HS256 naming a null issuer", signedHS256(t, jwt.SigningMethodHS256, namingIssuer(nil)), tokenUnrecognised},
		{"HS256 naming its issuer in capitals", compact(`{"alg":"HS256","typ":"JWT"}`, `{"ISS":"`+testIssuer+`","sub":"x"}`), tokenUnrecognised},
		{"an RS256 ID token", compact(`{"alg":"RS256","kid":"k"}`, idToken), tokenIdentityProvider},
		{"an RS512 ID token", compact(`{"alg":"RS512","kid":"k"}`, idToken), tokenIdentityProvider},
		{"a PS256 ID token", compact(`{"alg":"PS256","kid":"k"}`, idToken), tokenIdentityProvider},
		{"an ES256 ID token", compact(`{"alg":"ES256","kid":"k"}`, idToken), tokenIdentityProvider},
		{"an EdDSA ID token", compact(`{"alg":"EdDSA","kid":"k"}`, idToken), tokenIdentityProvider},
		// The provider's rules refuse it, for naming no issuer: they are the
		// ones a public-key signature is checked by.
		{"RS256 naming no issuer", compact(`{"alg":"RS256"}`, `{"sub":"x"}`), tokenIdentityProvider},
		{"alg none", compact(`{"alg":"none"}`, `{"sub":"x"}`), tokenUnrecognised},
		{"alg None, in another case", compact(`{"alg":"None"}`, `{"sub":"x"}`), tokenUnrecognised},
		{"no alg at all", compact(`{"typ":"JWT"}`, `{"sub":"x"}`), tokenUnrecognised},
		{"an alg nobody implements", compact(`{"alg":"HS1024"}`, `{"sub":"x"}`), tokenUnrecognised},
		{"an alg that is not a string", compact(`{"alg":256}`, `{"sub":"x"}`), tokenUnrecognised},
		{"a payload that is not JSON", compact(`{"alg":"HS256"}`, `not json`), tokenUnrecognised},
		{"a payload that is a JSON list", compact(`{"alg":"HS256"}`, `["iss"]`), tokenUnrecognised},
		{"a header that is not JSON", compact(`HS256`, `{"sub":"x"}`), tokenUnrecognised},
		{"a header that is not base64url", "@@@." + base64.RawURLEncoding.EncodeToString([]byte(`{"sub":"x"}`)) + ".sig", tokenUnrecognised},
		{"two parts", "eyJhbGciOiJIUzI1NiJ9.e30", tokenUnrecognised},
		{"four parts", signedHS256(t, jwt.SigningMethodHS256, loginClaims) + ".more", tokenUnrecognised},
		{"no parts", "opaque-token-value", tokenUnrecognised},
		{"nothing", "", tokenUnrecognised},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := kindOf(tc.token); got != tc.want {
				t.Fatalf("read as %s, want %s", tokenKindNames[got], tokenKindNames[tc.want])
			}
		})
	}
}

var tokenKindNames = map[tokenKind]string{
	tokenUnrecognised:     "neither kind",
	tokenLocal:            "a local token",
	tokenIdentityProvider: "an identity provider's ID token",
}

// Reading the kind is on every request while OIDC is on, so what it costs is
// measured rather than assumed.
func BenchmarkKindOf(b *testing.B) {
	token, err := jwt.NewWithClaims(jwt.SigningMethodHS256, jwt.MapClaims{
		"sub": "0199a4c2-5e1b-7c3d-8f00-1a2b3c4d5e6f", "username": "hopper",
		"roles": []string{"ADMIN"}, "exp": 4102444800, "iat": 1700000000,
	}).SignedString([]byte("a-secret-for-the-benchmark"))
	if err != nil {
		b.Fatalf("sign: %v", err)
	}
	b.ReportAllocs()
	for b.Loop() {
		if kindOf(token) != tokenLocal {
			b.Fatal("misread")
		}
	}
}
