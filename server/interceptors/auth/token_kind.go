package auth

import (
	"encoding/base64"
	"encoding/json"
	"strings"

	"github.com/golang-jwt/jwt/v5"
)

// tokenKind is what a bearer token says it is, read before anything about it
// is verified.
type tokenKind int

const (
	// tokenUnrecognised is neither kind, and is refused without either set of
	// rules being consulted.
	tokenUnrecognised tokenKind = iota
	// tokenLocal is what /api/v1/login mints: signed with JWT_SECRET, an HMAC,
	// and naming no issuer.
	tokenLocal
	// tokenIdentityProvider is an ID token: signed with a key pair, whose
	// public half the identity provider publishes.
	tokenIdentityProvider
)

// kindOf reads which kind of token this says it is: from the algorithm its
// header names and from whether its payload names an issuer.
//
// Nothing read here is trusted. It chooses whose rules check the token, and
// those rules then read the same fields their own way and verify them: the
// local rules take an HMAC and nothing else, and the identity provider's take
// the provider's own algorithms and issuer, and its keys, and nothing else.
// So a token that lies about what it is reaches only rules that refuse it:
// what it says cannot make either set of rules accept it.
//
// A local token names no issuer — /api/v1/login never writes one — so an HMAC
// token that names one is not ours, whoever it names: if it names the
// identity provider it is claiming to be the provider's, which never signs
// with our secret. It is refused here, and in particular is never checked
// against JWT_SECRET. Case is ignored in the issuer's name, as the provider's
// rules ignore it.
func kindOf(token string) tokenKind {
	encodedHeader, rest, _ := strings.Cut(token, ".")
	encodedPayload, signature, found := strings.Cut(rest, ".")
	if !found || strings.Contains(signature, ".") {
		return tokenUnrecognised
	}
	// The one member of each that is read: how the token is signed, and who
	// issued it — raw, so an empty or null issuer is told apart from none.
	var header struct {
		Algorithm string `json:"alg"`
	}
	if !decodeSegment(encodedHeader, &header) {
		return tokenUnrecognised
	}
	var payload struct {
		Issuer json.RawMessage `json:"iss"`
	}
	if !decodeSegment(encodedPayload, &payload) {
		return tokenUnrecognised
	}
	kind := kindOfAlgorithm(header.Algorithm)
	if kind == tokenLocal && payload.Issuer != nil {
		return tokenUnrecognised
	}
	return kind
}

// kindOfAlgorithm names the kind of token an algorithm belongs to: an HMAC
// is a local token's, a public-key signature an identity provider's. The
// families are the JWT library's own, so the local family is exactly the one
// the local rules accept. "none", and any algorithm that is neither, belongs
// to no kind.
func kindOfAlgorithm(algorithm string) tokenKind {
	switch jwt.GetSigningMethod(algorithm).(type) {
	case *jwt.SigningMethodHMAC:
		return tokenLocal
	case *jwt.SigningMethodRSA, *jwt.SigningMethodRSAPSS, *jwt.SigningMethodECDSA, *jwt.SigningMethodEd25519:
		return tokenIdentityProvider
	default:
		return tokenUnrecognised
	}
}

// decodeSegment reads one base64url segment of a token as a JSON object.
func decodeSegment(segment string, into any) bool {
	raw, err := base64.RawURLEncoding.DecodeString(segment)
	if err != nil {
		return false
	}
	return json.Unmarshal(raw, into) == nil
}
