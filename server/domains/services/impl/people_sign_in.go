package impl

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/gsoultan/metis/internal/pkg/apierr"
	"github.com/gsoultan/metis/internal/pkg/auth"
	"github.com/gsoultan/metis/internal/pkg/loginthrottle"
	"github.com/gsoultan/metis/internal/pkg/peopletoken"
	"github.com/gsoultan/metis/server/domains/entities"
	servicecontracts "github.com/gsoultan/metis/server/domains/services/contracts"
	repocontracts "github.com/gsoultan/metis/server/repositories/contracts"
	"golang.org/x/crypto/bcrypt"
)

type peopleSignInService struct {
	directory repocontracts.PersonSignInRepository
	keys      *peopletoken.Keys
	throttle  *loginthrottle.Throttle
	now       func() time.Time
	// apiKeys keeps the keys People give their systems. Nil, keys are not
	// offered and none is accepted.
	apiKeys repocontracts.PersonAPIKeyRepository
}

// PeopleSignInOption adjusts the People sign-in.
type PeopleSignInOption func(*peopleSignInService)

// WithPersonAPIKeys lets People make keys for their own systems, kept in
// keys, and accepts those keys on the People routes.
func WithPersonAPIKeys(keys repocontracts.PersonAPIKeyRepository) PeopleSignInOption {
	return func(s *peopleSignInService) { s.apiKeys = keys }
}

// NewPeopleSignInService signs People in against a project's directory, with
// tokens sealed by keys.
func NewPeopleSignInService(directory repocontracts.PersonSignInRepository, keys *peopletoken.Keys, options ...PeopleSignInOption) servicecontracts.PeopleSignInService {
	s := &peopleSignInService{
		directory: directory,
		keys:      keys,
		throttle:  loginthrottle.New(),
		now:       time.Now,
	}
	for _, option := range options {
		option(s)
	}
	return s
}

// errPersonCredentials is every refusal a sign-in gives that could otherwise
// tell an unknown name from a wrong password: nobody of that name, no password
// set, deactivated, or the wrong password.
var errPersonCredentials = fmt.Errorf("%w: invalid credentials", auth.ErrAuthenticationFailed)

// SignInPerson checks a person's password the way the platform sign-in checks
// an account's: a throttle per name before anything is read, and a bcrypt
// comparison on every path, so a missing person costs what a wrong password
// does.
func (s *peopleSignInService) SignInPerson(ctx context.Context, projectID uuid.UUID, username, password string) (entities.Person, string, time.Time, error) {
	if projectID == uuid.Nil || username == "" || password == "" {
		return entities.Person{}, "", time.Time{}, errPersonCredentials
	}
	// Keyed by project as well as name: "ada" in two projects is two people,
	// and one being guessed at must not lock the other out.
	key := projectID.String() + "/" + username
	if retry, wait := s.throttle.RetryAfter(key, s.now()); wait {
		return entities.Person{}, "", time.Time{}, fmt.Errorf("%w: too many failed attempts, try again in %s",
			auth.ErrAuthenticationFailed, retry.Round(time.Second))
	}

	participant, hash, err := s.directory.GetForSignIn(ctx, projectID, username)
	if err != nil && !errors.Is(err, apierr.ErrNotFound) {
		return entities.Person{}, "", time.Time{}, fmt.Errorf("sign a person in: %w", err)
	}
	if err != nil || hash == "" || !participant.Active {
		// The result is deliberately unused: the compare only makes this path
		// cost what a wrong password costs.
		//nolint:errcheck // deliberate: equalises timing, result is meaningless
		// #nosec G104 -- same reason.
		bcrypt.CompareHashAndPassword(dummyHash, []byte(password))
		s.throttle.Failed(key, s.now())
		return entities.Person{}, "", time.Time{}, errPersonCredentials
	}
	if err := bcrypt.CompareHashAndPassword([]byte(hash), []byte(password)); err != nil {
		s.throttle.Failed(key, s.now())
		return entities.Person{}, "", time.Time{}, errPersonCredentials
	}
	s.throttle.Succeeded(key)

	person := entities.PersonFrom(participant)
	token, expires := s.keys.Issue(person.ID, person.ProjectID)
	return person, token, expires, nil
}

// AuthenticatePerson reads a token and checks it against the directory as it
// is now, on every request: a person removed, deactivated or given a new
// password since the token was sealed is refused, without waiting for it to
// expire.
func (s *peopleSignInService) AuthenticatePerson(ctx context.Context, token string) (entities.Person, error) {
	if strings.HasPrefix(token, PersonAPIKeyPrefix) {
		return s.authenticateKey(ctx, token)
	}
	claims, err := s.keys.Read(token)
	if err != nil {
		return entities.Person{}, fmt.Errorf("%w: %w", auth.ErrUnauthorized, err)
	}
	participant, validFrom, err := s.directory.GetSession(ctx, claims.Participant)
	if errors.Is(err, apierr.ErrNotFound) {
		return entities.Person{}, fmt.Errorf("%w: this person is no longer in the directory", auth.ErrUnauthorized)
	}
	if err != nil {
		return entities.Person{}, fmt.Errorf("authenticate a person: %w", err)
	}
	if !participant.Active {
		return entities.Person{}, fmt.Errorf("%w: this person is deactivated", auth.ErrUnauthorized)
	}
	person := entities.PersonFrom(participant)
	// The token says which project; the directory has the last word. A row
	// that moved, or a token whose claims disagree with it, signs in nobody.
	if person.ProjectID != claims.Project {
		return entities.Person{}, fmt.Errorf("%w: the token names another project", auth.ErrUnauthorized)
	}
	// Strictly before, by the second, as the platform sign-in compares: a
	// token sealed in the same second as a password change is the one the
	// person is about to be handed.
	if !validFrom.IsZero() && claims.IssuedAt.Unix() < validFrom.Unix() {
		return entities.Person{}, fmt.Errorf("%w: issued before the password was changed", auth.ErrUnauthorized)
	}
	return person, nil
}

type unavailablePeopleSignInService struct {
	reason string
}

// NewUnavailablePeopleSignInService refuses every People sign-in with reason:
// when no key is set, or the installation has no People directory.
func NewUnavailablePeopleSignInService(reason string) servicecontracts.PeopleSignInService {
	return unavailablePeopleSignInService{reason: reason}
}

func (u unavailablePeopleSignInService) SignInPerson(context.Context, uuid.UUID, string, string) (entities.Person, string, time.Time, error) {
	return entities.Person{}, "", time.Time{}, errors.New(u.reason)
}

func (u unavailablePeopleSignInService) AuthenticatePerson(context.Context, string) (entities.Person, error) {
	return entities.Person{}, fmt.Errorf("%w: %s", auth.ErrUnauthorized, u.reason)
}
