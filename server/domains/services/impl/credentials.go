package impl

import (
	"context"
	"errors"

	"github.com/google/uuid"

	"github.com/gsoultan/metis/server/domains/entities"
)

// Credentials holds every account's password and authenticator app when a
// program embedding the engine has taken signing in over (engine.WithIdentity).
// The engine then writes neither a password hash nor a second factor of its
// own: it calls these instead, wherever it makes or manages an account.
type Credentials interface {
	SetPassword(ctx context.Context, userID uuid.UUID, plain string) error
	RemoveSecondFactor(ctx context.Context, userID uuid.UUID) error
}

// ErrSignInIsElsewhere answers a person's own credential call — signing in,
// a code, changing their own password — on an engine whose embedding program
// serves those itself.
var ErrSignInIsElsewhere = errors.New("signing in is served by the program in front of this engine")

// unusablePasswordHash is what an account made under an Identity keeps in the
// engine's own password column: no bcrypt hash, so nothing ever matches it.
// The engine does not read the column while an Identity is set; this is for
// the day it is taken away, when such an account signs in only once somebody
// sets it a password.
const unusablePasswordHash = "!identity"

// WithCredentials hands passwords and second factors to the embedding
// program's Identity.
func WithCredentials(credentials Credentials) UserServiceOption {
	return func(s *userService) { s.credentials = credentials }
}

// AccountForPrincipal reads the account an Identity proved a request to be,
// through the same few-second cache a session's account is read through, so
// a role taken away applies within seconds either way.
func (s *userService) AccountForPrincipal(ctx context.Context, userID uuid.UUID) (entities.User, error) {
	principal, err := s.resolvePrincipal(ctx, userID)
	if err != nil {
		return entities.User{}, err
	}
	return principal.user, nil
}
