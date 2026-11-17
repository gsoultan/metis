package impl

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/rs/zerolog/log"

	"github.com/gsoultan/metis/internal/pkg/apierr"
	"github.com/gsoultan/metis/internal/pkg/auth"
	"github.com/gsoultan/metis/internal/pkg/integrationkey"
	"github.com/gsoultan/metis/server/domains/entities"
	servicecontracts "github.com/gsoultan/metis/server/domains/services/contracts"
	repocontracts "github.com/gsoultan/metis/server/repositories/contracts"
)

const (
	// maxIntegrationKeys bounds the live keys one organization holds: one per
	// system it connects, and fifty is past any real count.
	maxIntegrationKeys = 50
	// maxIntegrationKeyName is the longest name a key takes.
	maxIntegrationKeyName = 100
	// integrationKeyPrefixLen is how much of a key is kept to tell it apart
	// in a list: the marker and eight characters, far too few to guess the
	// rest.
	integrationKeyPrefixLen = len(integrationkey.Prefix) + 8
)

// integrationKeyRoles are the roles a key may be given. Not the
// administrator's: a key is a password nobody types, kept in another system's
// configuration, and needs no second factor. Whatever it can do, somebody who
// copies it can do, so it must not be able to change who may do what, make
// more keys, or reconfigure the organization.
var integrationKeyRoles = []string{
	entities.RoleUser,
	entities.RoleOperator,
	entities.RoleDesigner,
	entities.RoleReleaser,
	entities.RoleQueryAuthor,
}

var (
	errKeyIsNotAnAdministrator = apierr.Forbiddenf("an integration API key cannot make, list or revoke keys; an administrator signs in to do that")
	errKeysNeedAnOrganization  = apierr.Invalidf("choose the organization the key is for")
	errKeysNeedAnAdministrator = apierr.Forbiddenf("only an administrator of the organization manages its integration API keys")
)

type integrationKeyService struct {
	keys repocontracts.IntegrationAPIKeyRepository
	now  func() time.Time
}

// NewIntegrationKeyService manages organizations' integration API keys.
func NewIntegrationKeyService(keys repocontracts.IntegrationAPIKeyRepository) servicecontracts.IntegrationKeyService {
	return &integrationKeyService{keys: keys, now: time.Now}
}

func hashIntegrationKey(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}

// administering returns the administrator managing keys and the organization
// the request is for, or why they may not.
func administering(ctx context.Context) (*entities.User, uuid.UUID, error) {
	caller := signedIn(ctx)
	if caller == nil {
		return nil, uuid.Nil, fmt.Errorf("%w: sign in to manage integration API keys", auth.ErrUnauthorized)
	}
	if caller.ViaIntegrationKey() {
		return nil, uuid.Nil, errKeyIsNotAnAdministrator
	}
	organization := entities.ActingOrganization(ctx)
	if organization == uuid.Nil {
		return nil, uuid.Nil, errKeysNeedAnOrganization
	}
	if !caller.HoldsRoleIn(organization, entities.RoleAdmin) {
		return nil, uuid.Nil, errKeysNeedAnAdministrator
	}
	return caller, organization, nil
}

// keyRoles checks and tidies the roles a key is asked to act with.
func keyRoles(asked []string) ([]string, error) {
	roles := make([]string, 0, len(asked))
	for _, role := range asked {
		role = strings.ToUpper(strings.TrimSpace(role))
		if role == "" || slices.Contains(roles, role) {
			continue
		}
		if !slices.Contains(integrationKeyRoles, role) {
			return nil, apierr.Invalidf("a key cannot act as %s; it may be given %s",
				role, strings.Join(integrationKeyRoles, ", "))
		}
		roles = append(roles, role)
	}
	if len(roles) == 0 {
		return nil, apierr.Invalidf("give the key at least one role: %s", strings.Join(integrationKeyRoles, ", "))
	}
	slices.Sort(roles)
	return roles, nil
}

func (s *integrationKeyService) CreateIntegrationKey(ctx context.Context, name string, roles []string, expiresAt *time.Time) (entities.IntegrationAPIKey, string, error) {
	caller, organization, err := administering(ctx)
	if err != nil {
		return entities.IntegrationAPIKey{}, "", err
	}
	name = strings.TrimSpace(name)
	if name == "" || utf8.RuneCountInString(name) > maxIntegrationKeyName {
		return entities.IntegrationAPIKey{}, "", apierr.Invalidf("a key needs a name of 1 to %d characters, saying which system uses it", maxIntegrationKeyName)
	}
	roles, err = keyRoles(roles)
	if err != nil {
		return entities.IntegrationAPIKey{}, "", err
	}
	now := s.now().UTC()
	if expiresAt != nil && !expiresAt.After(now) {
		return entities.IntegrationAPIKey{}, "", apierr.Invalidf("a key's expiry must be in the future")
	}

	held, err := s.keys.ListByOrganization(ctx, organization)
	if err != nil {
		return entities.IntegrationAPIKey{}, "", err
	}
	live := 0
	for _, k := range held {
		if !k.Live(now) {
			continue
		}
		live++
		if strings.EqualFold(k.Name, name) {
			return entities.IntegrationAPIKey{}, "", apierr.Invalidf("the organization already has a key called %q; revoke it or choose another name", name)
		}
	}
	if live >= maxIntegrationKeys {
		return entities.IntegrationAPIKey{}, "", apierr.Invalidf("the organization has %d keys, the most it may hold; revoke one first", maxIntegrationKeys)
	}

	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		return entities.IntegrationAPIKey{}, "", fmt.Errorf("could not make a key: %w", err)
	}
	token := integrationkey.Prefix + base64.RawURLEncoding.EncodeToString(raw)
	key := entities.IntegrationAPIKey{
		ID:             uuid.Must(uuid.NewV7()),
		OrganizationID: organization,
		Name:           name,
		Prefix:         token[:integrationKeyPrefixLen],
		Roles:          roles,
		CreatedBy:      caller.Username,
		CreatedAt:      now,
	}
	if expiresAt != nil {
		at := expiresAt.UTC()
		key.ExpiresAt = &at
	}
	if err := s.keys.Create(ctx, key, hashIntegrationKey(token)); err != nil {
		return entities.IntegrationAPIKey{}, "", err
	}
	traceIntegrationKey(caller, "An integration API key was made.", key)
	return key, token, nil
}

func (s *integrationKeyService) ListIntegrationKeys(ctx context.Context) ([]entities.IntegrationAPIKey, error) {
	_, organization, err := administering(ctx)
	if err != nil {
		return nil, err
	}
	return s.keys.ListByOrganization(ctx, organization)
}

func (s *integrationKeyService) RevokeIntegrationKey(ctx context.Context, id uuid.UUID) error {
	caller, organization, err := administering(ctx)
	if err != nil {
		return err
	}
	if err := s.keys.Revoke(ctx, organization, id, s.now()); err != nil {
		return err
	}
	traceIntegrationKey(caller, "An integration API key was revoked.",
		entities.IntegrationAPIKey{ID: id, OrganizationID: organization})
	return nil
}

// AuthenticateIntegrationKey signs in the organization a key belongs to, with
// the key's roles there and in no other organization.
func (s *integrationKeyService) AuthenticateIntegrationKey(ctx context.Context, token string) (entities.User, error) {
	if !integrationkey.Is(token) {
		return entities.User{}, fmt.Errorf("%w: not an integration API key", auth.ErrUnauthorized)
	}
	key, err := s.keys.GetUnrevokedByHash(ctx, hashIntegrationKey(token))
	if errors.Is(err, apierr.ErrNotFound) {
		return entities.User{}, fmt.Errorf("%w: no such key, or it was revoked", auth.ErrUnauthorized)
	}
	if err != nil {
		return entities.User{}, fmt.Errorf("authenticate a key: %w", err)
	}
	now := s.now()
	if !key.Live(now) {
		return entities.User{}, fmt.Errorf("%w: this key has expired", auth.ErrUnauthorized)
	}
	if key.LastUsedAt == nil || now.Sub(*key.LastUsedAt) >= keyUseGranularity {
		if err := s.keys.MarkUsed(ctx, key.ID, now); err != nil {
			// The request is the organization's either way; the last-used
			// time is only a hint for whoever tidies its keys.
			log.Warn().Err(err).Str("key_id", key.ID.String()).Msg("could not record an integration API key's use")
		}
	}
	return integrationPrincipal(key), nil
}

// integrationPrincipal is who a key acts as: a principal of its own, named for
// the key, member of its organization alone and holding its roles only
// there.
func integrationPrincipal(key entities.IntegrationAPIKey) entities.User {
	organization := &entities.Organization{ID: key.OrganizationID}
	return entities.User{
		ID:                  key.ID,
		Username:            "integration:" + key.Name,
		DisplayName:         key.Name,
		Organizations:       []*entities.Organization{organization},
		Organization:        organization,
		Roles:               []string{},
		RolesByOrganization: map[uuid.UUID][]string{key.OrganizationID: slices.Clone(key.Roles)},
		IntegrationKey:      key.ID,
	}
}

// traceIntegrationKey says in the server's log who made or revoked a key. It
// names the key by id and name and its roles, never the key itself.
func traceIntegrationKey(actor *entities.User, what string, key entities.IntegrationAPIKey) {
	line := log.Info().Str("actor_id", actor.ID.String()).Str("actor", actor.Username).
		Str("organization", key.OrganizationID.String()).
		Str("integration_key_id", key.ID.String())
	if key.Name != "" {
		line = line.Str("integration_key", key.Name).Strs("roles", key.Roles)
	}
	line.Msg(what)
}
