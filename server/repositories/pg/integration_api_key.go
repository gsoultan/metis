package pg

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/gsoultan/metis/internal/pkg/apierr"
	"github.com/gsoultan/metis/server/domains/entities"
	"github.com/gsoultan/metis/server/repositories/contracts"
	"github.com/gsoultan/metis/server/repositories/db"
	"github.com/gsoultan/metis/server/repositories/store/integrationapikey"
	"github.com/gsoultan/storm"
	"github.com/gsoultan/storm/runtime"
	"github.com/rs/zerolog/log"
)

type integrationAPIKeyRepository struct {
	conn *db.Conn
}

// NewIntegrationAPIKeyRepository keeps organizations' integration keys in the
// main database, beside the organizations, whatever port a request arrives on.
//
// Not scoped by tenant: a key is presented by a system that is not signed in,
// so there is no tenant to scope it by until the key names one. Every read is
// by the key's hash, or by the organization the caller administers.
func NewIntegrationAPIKeyRepository(conn *db.Conn) contracts.IntegrationAPIKeyRepository {
	return &integrationAPIKeyRepository{conn: conn}
}

func (r *integrationAPIKeyRepository) Create(ctx context.Context, key entities.IntegrationAPIKey, tokenHash string) error {
	ex, err := r.conn.MainExecutor(ctx)
	if err != nil {
		return err
	}
	roles, err := json.Marshal(key.Roles)
	if err != nil {
		return fmt.Errorf("could not encode the key's roles: %w", err)
	}
	ins := integrationapikey.Create()
	ins.SetID(key.ID)
	ins.SetOrganizationID(key.OrganizationID)
	ins.SetName(key.Name)
	ins.SetPrefix(key.Prefix)
	ins.SetTokenHash(tokenHash)
	ins.SetRoles(storm.JSON(roles))
	ins.SetCreatedBy(key.CreatedBy)
	if key.ExpiresAt != nil {
		ins.SetExpiresAt(key.ExpiresAt.UTC())
	}
	if _, err := ins.Insert(ctx, ex); err != nil {
		return fmt.Errorf("could not record the key: %w", err)
	}
	return nil
}

func (r *integrationAPIKeyRepository) ListByOrganization(ctx context.Context, organizationID uuid.UUID) ([]entities.IntegrationAPIKey, error) {
	ex, err := r.conn.MainExecutor(ctx)
	if err != nil {
		return nil, err
	}
	rows, err := everyRow[integrationapikey.Row](ctx, ex, integrationapikey.New().
		Where(integrationapikey.OrganizationID.Eq(organizationID)).
		Order(integrationapikey.CreatedAt.Desc(), integrationapikey.ID.Desc()))
	if err != nil {
		return nil, fmt.Errorf("could not list the keys: %w", err)
	}
	out := make([]entities.IntegrationAPIKey, 0, len(rows))
	for _, row := range rows {
		out = append(out, integrationAPIKeyFrom(row))
	}
	return out, nil
}

func (r *integrationAPIKeyRepository) Revoke(ctx context.Context, organizationID, id uuid.UUID, at time.Time) error {
	ex, err := r.conn.MainExecutor(ctx)
	if err != nil {
		return err
	}
	row, found, err := integrationapikey.New().
		Where(integrationapikey.ID.Eq(id), integrationapikey.OrganizationID.Eq(organizationID), integrationapikey.RevokedAt.IsNull()).
		One(ctx, ex)
	if err != nil {
		return fmt.Errorf("could not read the key: %w", err)
	}
	if !found {
		return fmt.Errorf("%w: no such key", apierr.ErrNotFound)
	}
	mut := integrationapikey.Mutate(row)
	mut.SetRevokedAt(at.UTC())
	if err := mut.Update(ctx, ex); err != nil {
		return fmt.Errorf("could not revoke the key: %w", err)
	}
	return nil
}

func (r *integrationAPIKeyRepository) GetUnrevokedByHash(ctx context.Context, tokenHash string) (entities.IntegrationAPIKey, error) {
	ex, err := r.conn.MainExecutor(ctx)
	if err != nil {
		return entities.IntegrationAPIKey{}, err
	}
	row, found, err := integrationapikey.New().
		Where(integrationapikey.TokenHash.Eq(tokenHash), integrationapikey.RevokedAt.IsNull()).
		One(ctx, ex)
	if err != nil {
		return entities.IntegrationAPIKey{}, fmt.Errorf("could not read the key: %w", err)
	}
	if !found {
		return entities.IntegrationAPIKey{}, fmt.Errorf("%w: no such key", apierr.ErrNotFound)
	}
	return integrationAPIKeyFrom(row), nil
}

func (r *integrationAPIKeyRepository) MarkUsed(ctx context.Context, id uuid.UUID, at time.Time) error {
	ex, err := r.conn.MainExecutor(ctx)
	if err != nil {
		return err
	}
	// Only the one column, keyed by id: a key revoked at the same moment
	// stays revoked.
	mut := integrationapikey.MutateKey(id)
	mut.SetLastUsedAt(at.UTC())
	if err := mut.Update(ctx, ex); err != nil && !errors.Is(err, runtime.ErrNoRow) {
		return fmt.Errorf("could not record the key's use: %w", err)
	}
	return nil
}

func integrationAPIKeyFrom(row integrationapikey.Row) entities.IntegrationAPIKey {
	key := entities.IntegrationAPIKey{
		ID:             row.ID,
		OrganizationID: row.OrganizationID,
		Name:           row.Name,
		Prefix:         row.Prefix,
		CreatedBy:      row.CreatedBy,
		CreatedAt:      row.CreatedAt,
		Roles:          []string{},
	}
	// A row whose roles cannot be read acts with none: refusing is the safe
	// way to be wrong about what a key may do.
	if err := json.Unmarshal(row.Roles, &key.Roles); err != nil {
		log.Warn().Err(err).Str("key_id", uuid.UUID(row.ID).String()).Msg("an integration API key's roles could not be read; it acts with none")
		key.Roles = []string{}
	}
	if at, ok := row.ExpiresAt.Get(); ok {
		key.ExpiresAt = &at
	}
	if at, ok := row.LastUsedAt.Get(); ok {
		key.LastUsedAt = &at
	}
	if at, ok := row.RevokedAt.Get(); ok {
		key.RevokedAt = &at
	}
	return key
}
