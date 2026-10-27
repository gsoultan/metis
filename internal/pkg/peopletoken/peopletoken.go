// Package peopletoken mints and reads the tokens People sign in with.
//
// People are the population processes give work to; Users run the platform.
// The two must never be able to stand in for each other, so People do not get
// the platform's JWT. Their tokens are PASETO v4.local: encrypted and
// authenticated with a symmetric key only this server holds. There is no
// algorithm header to negotiate, the claims cannot be read by whoever holds the
// token, and a token begins "v4.local." — a shape the platform's rules refuse
// on sight and these rules are the only ones to accept.
package peopletoken

import (
	"crypto/hkdf"
	"crypto/sha256"
	"errors"
	"fmt"
	"strings"
	"time"

	"aidanwoods.dev/go-paseto"
	"github.com/google/uuid"
	"github.com/gsoultan/metis/internal/pkg/secrets"
)

// EnvKey is the secret People tokens are sealed with. Unset, nobody can sign
// in as a person: there is no key to fall back to, and borrowing the
// platform's would make one leaked secret open both doors.
const EnvKey = "METIS_PEOPLE_TOKEN_KEY"

// EnvPreviousKey is the secret being rotated out. Tokens it sealed are still
// read until they expire; nothing new is sealed with it.
const EnvPreviousKey = "METIS_PEOPLE_TOKEN_KEY_PREVIOUS"

// Prefix is how every People token starts. Anything else is not one.
const Prefix = "v4.local."

// APIKeyPrefix starts a person's API key, which their own systems present on
// the People routes in place of a sign-in token. A key is not a PASETO token,
// so it is told apart, and refused on the platform's routes, by this.
const APIKeyPrefix = "mpk_"

// IsPeopleCredential reports whether token is one the People routes take: a
// sign-in token or an API key.
func IsPeopleCredential(token string) bool {
	return strings.HasPrefix(token, Prefix) || strings.HasPrefix(token, APIKeyPrefix)
}

// Audience is the one audience a People token names.
const Audience = "people"

// Lifetime is how long a People token is good for.
const Lifetime = 12 * time.Hour

// projectClaim carries the project the person belongs to. A username is unique
// only within a project, so a token that did not name it would not name a
// person.
const projectClaim = "prj"

// implicit binds every token to this purpose, so a v4.local token this server
// seals for anything else in future cannot be replayed as a sign-in.
var implicit = []byte("metis:people-sign-in:v1")

// keyInfo separates the key derived here from anything else derived from the
// same secret.
const keyInfo = "metis people token v4.local"

// ErrInvalid is any token that is not one this server sealed for a person and
// that is still good.
var ErrInvalid = errors.New("not a valid People token")

// Claims is what a People token says.
type Claims struct {
	Participant uuid.UUID
	Project     uuid.UUID
	IssuedAt    time.Time
}

// Keys seal and open People tokens.
type Keys struct {
	current  paseto.V4SymmetricKey
	previous *paseto.V4SymmetricKey
	now      func() time.Time
}

// NewKeys derives the keys from the operator's secrets. previous may be empty.
//
// The secret is held to the same rule as JWT_SECRET, then stretched to the 32
// bytes v4.local needs with HKDF, so an operator writes a secret the way they
// write every other one rather than producing raw key bytes.
func NewKeys(current, previous string) (*Keys, error) {
	key, err := deriveKey(EnvKey, current)
	if err != nil {
		return nil, err
	}
	keys := &Keys{current: key, now: time.Now}
	if strings.TrimSpace(previous) != "" {
		old, err := deriveKey(EnvPreviousKey, previous)
		if err != nil {
			return nil, err
		}
		keys.previous = &old
	}
	return keys, nil
}

func deriveKey(name, secret string) (paseto.V4SymmetricKey, error) {
	if err := secrets.Validate(name, secret); err != nil {
		return paseto.V4SymmetricKey{}, err
	}
	raw, err := hkdf.Key(sha256.New, []byte(strings.TrimSpace(secret)), nil, keyInfo, 32)
	if err != nil {
		return paseto.V4SymmetricKey{}, fmt.Errorf("derive the key from %s: %w", name, err)
	}
	return paseto.V4SymmetricKeyFromBytes(raw)
}

// Issue seals a token for a person in a project.
func (k *Keys) Issue(participant, project uuid.UUID) (string, time.Time) {
	now := k.now()
	expires := now.Add(Lifetime)
	token := paseto.NewToken()
	token.SetIssuedAt(now)
	token.SetNotBefore(now)
	token.SetExpiration(expires)
	token.SetAudience(Audience)
	token.SetSubject(participant.String())
	token.SetString(projectClaim, project.String())
	return token.V4Encrypt(k.current, implicit), expires
}

// Read opens a token and returns what it says, or ErrInvalid.
//
// Tried against the current key, then the previous one: during a rotation a
// token sealed an hour ago is still somebody's session.
func (k *Keys) Read(token string) (Claims, error) {
	if !strings.HasPrefix(token, Prefix) {
		return Claims{}, ErrInvalid
	}
	parser := paseto.MakeParser([]paseto.Rule{paseto.ValidAt(k.now()), paseto.ForAudience(Audience)})
	parsed, err := parser.ParseV4Local(k.current, token, implicit)
	if err != nil && k.previous != nil {
		parsed, err = parser.ParseV4Local(*k.previous, token, implicit)
	}
	if err != nil {
		return Claims{}, fmt.Errorf("%w: %w", ErrInvalid, err)
	}
	return claimsOf(parsed)
}

func claimsOf(token *paseto.Token) (Claims, error) {
	subject, err := token.GetSubject()
	if err != nil {
		return Claims{}, fmt.Errorf("%w: no subject", ErrInvalid)
	}
	participant, err := uuid.Parse(subject)
	if err != nil {
		return Claims{}, fmt.Errorf("%w: the subject is not a participant id", ErrInvalid)
	}
	projectText, err := token.GetString(projectClaim)
	if err != nil {
		return Claims{}, fmt.Errorf("%w: no project", ErrInvalid)
	}
	project, err := uuid.Parse(projectText)
	if err != nil {
		return Claims{}, fmt.Errorf("%w: the project is not an id", ErrInvalid)
	}
	issued, err := token.GetIssuedAt()
	if err != nil {
		return Claims{}, fmt.Errorf("%w: no issue time", ErrInvalid)
	}
	return Claims{Participant: participant, Project: project, IssuedAt: issued}, nil
}
