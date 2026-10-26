package peopletoken

import (
	"errors"
	"strings"
	"testing"
	"time"

	"aidanwoods.dev/go-paseto"
	"github.com/google/uuid"
)

const (
	secretA = "people-key-one-7c1f0e9a4b2d8f63e5a0c9b1d7e3f2a4"
	secretB = "people-key-two-3e8d1b5c9f0a7e2d4c6b8a1f3e5d7c9b"
)

func keysAt(t *testing.T, current, previous string, now time.Time) *Keys {
	t.Helper()
	k, err := NewKeys(current, previous)
	if err != nil {
		t.Fatalf("NewKeys: %v", err)
	}
	k.now = func() time.Time { return now }
	return k
}

func TestIssueThenReadNamesThePersonAndTheirProject(t *testing.T) {
	now := time.Date(2026, 10, 25, 9, 0, 0, 0, time.UTC)
	k := keysAt(t, secretA, "", now)
	participant, project := uuid.New(), uuid.New()

	token, expires := k.Issue(participant, project)
	if !strings.HasPrefix(token, Prefix) {
		t.Fatalf("token %q does not start %q", token, Prefix)
	}
	if !expires.Equal(now.Add(Lifetime)) {
		t.Fatalf("expires %v, want %v", expires, now.Add(Lifetime))
	}
	claims, err := k.Read(token)
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	if claims.Participant != participant || claims.Project != project || !claims.IssuedAt.Equal(now) {
		t.Fatalf("claims = %+v", claims)
	}
}

func TestReadRefuses(t *testing.T) {
	now := time.Date(2026, 10, 25, 9, 0, 0, 0, time.UTC)
	k := keysAt(t, secretA, "", now)
	good, _ := k.Issue(uuid.New(), uuid.New())

	other := keysAt(t, secretB, "", now)
	foreign, _ := other.Issue(uuid.New(), uuid.New())

	later := keysAt(t, secretA, "", now.Add(Lifetime+time.Second))

	wrongAudience := paseto.NewToken()
	wrongAudience.SetIssuedAt(now)
	wrongAudience.SetNotBefore(now)
	wrongAudience.SetExpiration(now.Add(time.Hour))
	wrongAudience.SetAudience("platform")
	wrongAudience.SetSubject(uuid.NewString())
	wrongAudience.SetString(projectClaim, uuid.NewString())

	otherPurpose := paseto.NewToken()
	otherPurpose.SetIssuedAt(now)
	otherPurpose.SetNotBefore(now)
	otherPurpose.SetExpiration(now.Add(time.Hour))
	otherPurpose.SetAudience(Audience)
	otherPurpose.SetSubject(uuid.NewString())
	otherPurpose.SetString(projectClaim, uuid.NewString())

	tampered := []byte(good)
	tampered[len(tampered)-5] ^= 0x01

	cases := []struct {
		name  string
		keys  *Keys
		token string
	}{
		{"a platform JWT", k, "eyJhbGciOiJIUzI1NiJ9.eyJzdWIiOiJ4In0.c2ln"},
		{"empty", k, ""},
		{"sealed with another key", k, foreign},
		{"expired", later, good},
		{"another audience", k, wrongAudience.V4Encrypt(k.current, implicit)},
		{"sealed for another purpose", k, otherPurpose.V4Encrypt(k.current, []byte("metis:task-link:v1"))},
		{"tampered", k, string(tampered)},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := tc.keys.Read(tc.token); !errors.Is(err, ErrInvalid) {
				t.Fatalf("Read = %v, want ErrInvalid", err)
			}
		})
	}
}

func TestATokenSealedWithThePreviousKeyIsReadDuringARotation(t *testing.T) {
	now := time.Date(2026, 10, 25, 9, 0, 0, 0, time.UTC)
	before := keysAt(t, secretA, "", now)
	token, _ := before.Issue(uuid.New(), uuid.New())

	rotated := keysAt(t, secretB, secretA, now)
	if _, err := rotated.Read(token); err != nil {
		t.Fatalf("Read during rotation: %v", err)
	}
	finished := keysAt(t, secretB, "", now)
	if _, err := finished.Read(token); !errors.Is(err, ErrInvalid) {
		t.Fatalf("Read after rotation = %v, want ErrInvalid", err)
	}
}

func TestAWeakSecretIsRefused(t *testing.T) {
	if _, err := NewKeys("short", ""); err == nil {
		t.Fatal("NewKeys accepted a short secret")
	}
	if _, err := NewKeys(secretA, "changeme"); err == nil {
		t.Fatal("NewKeys accepted a placeholder as the previous secret")
	}
}
