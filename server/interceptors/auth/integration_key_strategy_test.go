package auth

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"
	"github.com/gsoultan/metis/server/domains/entities"
)

type recordingStrategy struct{ seen string }

func (s *recordingStrategy) Authenticate(_ context.Context, token string) (any, error) {
	s.seen = token
	return entities.User{}, nil
}

// A key goes to the key's rules alone, and anything else to the rules it
// wraps alone.
func TestKeysAndSignInTokensAreCheckedByTheirOwnRules(t *testing.T) {
	refused := errors.New("refused")
	next := &recordingStrategy{}
	keysSaw := ""
	s := NewIntegrationKeyStrategy(func(_ context.Context, token string) (entities.User, error) {
		keysSaw = token
		return entities.User{}, refused
	}, next)

	if _, err := s.Authenticate(t.Context(), "mik_abc"); !errors.Is(err, refused) {
		t.Errorf("a key: err = %v, want the key rules' refusal as it is", err)
	}
	if keysSaw != "mik_abc" || next.seen != "" {
		t.Errorf("a key reached the key rules with %q and the others with %q", keysSaw, next.seen)
	}
	keysSaw = ""
	if _, err := s.Authenticate(t.Context(), "eyJhbGciOiJIUzI1NiJ9.e30.sig"); err != nil {
		t.Fatalf("a sign-in token: %v", err)
	}
	if keysSaw != "" || next.seen == "" {
		t.Errorf("a sign-in token reached the key rules (%q) or not the others (%q)", keysSaw, next.seen)
	}
}

func TestAKeyIsRefusedOnSessionOnlyPaths(t *testing.T) {
	key := entities.User{IntegrationKey: uuid.Must(uuid.NewV7())}
	account := entities.User{ID: uuid.Must(uuid.NewV7())}
	for _, tc := range []struct {
		principal any
		path      string
		want      bool
	}{
		{key, "/api/v1/users", true},
		{key, "/api/v1/users/me/two-factor", true},
		{key, "/api/v1/integration-keys/123", true},
		{key, "/api/v1/settings", true},
		{key, "/api/v1/projects", false},
		{key, "/api/v1/usersettings", false},
		{account, "/api/v1/users/me", false},
		{"not a user", "/api/v1/users", false},
	} {
		if got := refusedToIntegrationKey(tc.principal, tc.path); got != tc.want {
			t.Errorf("%T on %s: refused = %v, want %v", tc.principal, tc.path, got, tc.want)
		}
	}
}
