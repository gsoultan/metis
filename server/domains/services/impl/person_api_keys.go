package impl

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/rs/zerolog/log"

	"github.com/gsoultan/metis/internal/pkg/apierr"
	"github.com/gsoultan/metis/internal/pkg/auth"
	"github.com/gsoultan/metis/internal/pkg/peopletoken"
	"github.com/gsoultan/metis/server/domains/entities"
	servicecontracts "github.com/gsoultan/metis/server/domains/services/contracts"
)

// PersonAPIKeyPrefix starts every personal API key, so one is told from a
// People sign-in token, and found by a secret scanner, at sight.
const PersonAPIKeyPrefix = peopletoken.APIKeyPrefix

const (
	// maxPersonAPIKeys bounds the live keys one person holds: one per system
	// they hand work to, and twenty is past any real count.
	maxPersonAPIKeys = 20
	// maxPersonAPIKeyName is the longest name a key takes.
	maxPersonAPIKeyName = 100
	// personAPIKeyPrefixLen is how much of a key is kept to tell it apart in
	// a list: the marker and eight characters, far too few to guess the rest.
	personAPIKeyPrefixLen = len(PersonAPIKeyPrefix) + 8
	// keyUseGranularity is how often a key's last use is written: once a
	// minute is enough to say whether a system still uses it, and a key
	// calling every second must not write every second.
	keyUseGranularity = time.Minute
)

var (
	errAPIKeysUnavailable = apierr.Invalidf("personal API keys are not available on this server")
	errKeyMakesNoKeys     = apierr.Forbiddenf("an API key works tasks; sign in to make or revoke keys")
)

func hashPersonAPIKey(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}

// authenticateKey signs in the person a key belongs to, as the directory has
// them now: a key of somebody removed or deactivated signs in nobody.
func (s *peopleSignInService) authenticateKey(ctx context.Context, token string) (entities.Person, error) {
	if s.apiKeys == nil {
		return entities.Person{}, fmt.Errorf("%w: personal API keys are not accepted here", auth.ErrUnauthorized)
	}
	key, err := s.apiKeys.GetLiveByHash(ctx, hashPersonAPIKey(token))
	if errors.Is(err, apierr.ErrNotFound) {
		return entities.Person{}, fmt.Errorf("%w: no such key, or it was revoked", auth.ErrUnauthorized)
	}
	if err != nil {
		return entities.Person{}, fmt.Errorf("authenticate a key: %w", err)
	}
	participant, _, err := s.directory.GetSession(ctx, key.ParticipantID)
	if errors.Is(err, apierr.ErrNotFound) {
		return entities.Person{}, fmt.Errorf("%w: this person is no longer in the directory", auth.ErrUnauthorized)
	}
	if err != nil {
		return entities.Person{}, fmt.Errorf("authenticate a key: %w", err)
	}
	if !participant.Active {
		return entities.Person{}, fmt.Errorf("%w: this person is deactivated", auth.ErrUnauthorized)
	}
	now := s.now()
	if key.LastUsedAt == nil || now.Sub(*key.LastUsedAt) >= keyUseGranularity {
		if err := s.apiKeys.MarkUsed(ctx, key.ID, now); err != nil {
			// The request is the person's either way; the last-used time is
			// only a hint for whoever tidies their keys.
			log.Warn().Err(err).Str("key_id", key.ID.String()).Msg("could not record a personal API key's use")
		}
	}
	person := entities.PersonFrom(participant)
	person.ViaAPIKey = true
	return person, nil
}

// CreatePersonAPIKey makes a key for one of the person's systems and returns
// it, the only time it is ever shown.
func (s *peopleSignInService) CreatePersonAPIKey(ctx context.Context, person entities.Person, name string) (entities.PersonAPIKey, string, error) {
	if s.apiKeys == nil {
		return entities.PersonAPIKey{}, "", errAPIKeysUnavailable
	}
	if person.ViaAPIKey {
		return entities.PersonAPIKey{}, "", errKeyMakesNoKeys
	}
	name = strings.TrimSpace(name)
	if name == "" || utf8.RuneCountInString(name) > maxPersonAPIKeyName {
		return entities.PersonAPIKey{}, "", apierr.Invalidf("a key needs a name of 1 to %d characters, saying which system uses it", maxPersonAPIKeyName)
	}
	held, err := s.apiKeys.ListByParticipant(ctx, person.ID)
	if err != nil {
		return entities.PersonAPIKey{}, "", err
	}
	live := 0
	for _, k := range held {
		if k.RevokedAt != nil {
			continue
		}
		live++
		if strings.EqualFold(k.Name, name) {
			return entities.PersonAPIKey{}, "", apierr.Invalidf("you already have a key called %q; revoke it or choose another name", name)
		}
	}
	if live >= maxPersonAPIKeys {
		return entities.PersonAPIKey{}, "", apierr.Invalidf("you have %d keys, the most one person may hold; revoke one first", maxPersonAPIKeys)
	}

	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		return entities.PersonAPIKey{}, "", fmt.Errorf("could not make a key: %w", err)
	}
	token := PersonAPIKeyPrefix + base64.RawURLEncoding.EncodeToString(raw)
	key := entities.PersonAPIKey{
		ID:            uuid.Must(uuid.NewV7()),
		ParticipantID: person.ID,
		Name:          name,
		Prefix:        token[:personAPIKeyPrefixLen],
		CreatedAt:     s.now().UTC(),
	}
	if err := s.apiKeys.Create(ctx, key, hashPersonAPIKey(token)); err != nil {
		return entities.PersonAPIKey{}, "", err
	}
	return key, token, nil
}

// ListPersonAPIKeys returns the person's keys, never the keys themselves.
func (s *peopleSignInService) ListPersonAPIKeys(ctx context.Context, person entities.Person) ([]entities.PersonAPIKey, error) {
	if s.apiKeys == nil {
		return nil, errAPIKeysUnavailable
	}
	if person.ViaAPIKey {
		return nil, errKeyMakesNoKeys
	}
	return s.apiKeys.ListByParticipant(ctx, person.ID)
}

// RevokePersonAPIKey stops one of the person's keys at once.
func (s *peopleSignInService) RevokePersonAPIKey(ctx context.Context, person entities.Person, id uuid.UUID) error {
	if s.apiKeys == nil {
		return errAPIKeysUnavailable
	}
	if person.ViaAPIKey {
		return errKeyMakesNoKeys
	}
	return s.apiKeys.Revoke(ctx, person.ID, id, s.now())
}

// PersonAPIKeysOf is the People sign-in's key half, or a service that says
// keys are unavailable when the sign-in is not the one that keeps them.
func PersonAPIKeysOf(signIn servicecontracts.PeopleSignInService) servicecontracts.PersonAPIKeyService {
	if svc, ok := signIn.(*peopleSignInService); ok {
		return svc
	}
	return unavailablePersonAPIKeys{}
}

type unavailablePersonAPIKeys struct{}

func (unavailablePersonAPIKeys) CreatePersonAPIKey(context.Context, entities.Person, string) (entities.PersonAPIKey, string, error) {
	return entities.PersonAPIKey{}, "", errAPIKeysUnavailable
}

func (unavailablePersonAPIKeys) ListPersonAPIKeys(context.Context, entities.Person) ([]entities.PersonAPIKey, error) {
	return nil, errAPIKeysUnavailable
}

func (unavailablePersonAPIKeys) RevokePersonAPIKey(context.Context, entities.Person, uuid.UUID) error {
	return errAPIKeysUnavailable
}
