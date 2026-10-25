package pg

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/gsoultan/metis/internal/pkg/apierr"
	"github.com/gsoultan/metis/server/domains/entities"
	"github.com/gsoultan/metis/server/repositories/contracts"
	"github.com/gsoultan/metis/server/repositories/db"
	"github.com/gsoultan/metis/server/repositories/store/personapikey"
	"github.com/gsoultan/storm/runtime"
)

type personAPIKeyRepository struct {
	conn *db.Conn
}

// NewPersonAPIKeyRepository keeps People's keys in the main database, beside
// the directory, whatever port a request arrives on.
//
// Not scoped by tenant: a key is presented by a system that is not signed in,
// so there is no tenant to scope it by. Every read is by the key's hash or by
// the participant the caller signed in as.
func NewPersonAPIKeyRepository(conn *db.Conn) contracts.PersonAPIKeyRepository {
	return &personAPIKeyRepository{conn: conn}
}

func (r *personAPIKeyRepository) Create(ctx context.Context, key entities.PersonAPIKey, tokenHash string) error {
	ex, err := r.conn.MainExecutor(ctx)
	if err != nil {
		return err
	}
	ins := personapikey.Create()
	ins.SetID(key.ID)
	ins.SetParticipantID(key.ParticipantID)
	ins.SetName(key.Name)
	ins.SetPrefix(key.Prefix)
	ins.SetTokenHash(tokenHash)
	if _, err := ins.Insert(ctx, ex); err != nil {
		return fmt.Errorf("could not record the key: %w", err)
	}
	return nil
}

func (r *personAPIKeyRepository) ListByParticipant(ctx context.Context, participantID uuid.UUID) ([]entities.PersonAPIKey, error) {
	ex, err := r.conn.MainExecutor(ctx)
	if err != nil {
		return nil, err
	}
	rows, err := everyRow[personapikey.Row](ctx, ex, personapikey.New().
		Where(personapikey.ParticipantID.Eq(participantID)).
		Order(personapikey.CreatedAt.Desc(), personapikey.ID.Desc()))
	if err != nil {
		return nil, fmt.Errorf("could not list the keys: %w", err)
	}
	out := make([]entities.PersonAPIKey, 0, len(rows))
	for _, row := range rows {
		out = append(out, personAPIKeyFrom(row))
	}
	return out, nil
}

func (r *personAPIKeyRepository) Revoke(ctx context.Context, participantID, id uuid.UUID, at time.Time) error {
	ex, err := r.conn.MainExecutor(ctx)
	if err != nil {
		return err
	}
	row, found, err := personapikey.New().
		Where(personapikey.ID.Eq(id), personapikey.ParticipantID.Eq(participantID), personapikey.RevokedAt.IsNull()).
		One(ctx, ex)
	if err != nil {
		return fmt.Errorf("could not read the key: %w", err)
	}
	if !found {
		return fmt.Errorf("%w: no such key", apierr.ErrNotFound)
	}
	mut := personapikey.Mutate(row)
	mut.SetRevokedAt(at.UTC())
	if err := mut.Update(ctx, ex); err != nil {
		return fmt.Errorf("could not revoke the key: %w", err)
	}
	return nil
}

func (r *personAPIKeyRepository) GetLiveByHash(ctx context.Context, tokenHash string) (entities.PersonAPIKey, error) {
	ex, err := r.conn.MainExecutor(ctx)
	if err != nil {
		return entities.PersonAPIKey{}, err
	}
	row, found, err := personapikey.New().
		Where(personapikey.TokenHash.Eq(tokenHash), personapikey.RevokedAt.IsNull()).
		One(ctx, ex)
	if err != nil {
		return entities.PersonAPIKey{}, fmt.Errorf("could not read the key: %w", err)
	}
	if !found {
		return entities.PersonAPIKey{}, fmt.Errorf("%w: no such key", apierr.ErrNotFound)
	}
	return personAPIKeyFrom(row), nil
}

func (r *personAPIKeyRepository) MarkUsed(ctx context.Context, id uuid.UUID, at time.Time) error {
	ex, err := r.conn.MainExecutor(ctx)
	if err != nil {
		return err
	}
	// Only the one column, keyed by id: a key revoked at the same moment
	// stays revoked.
	mut := personapikey.MutateKey(id)
	mut.SetLastUsedAt(at.UTC())
	if err := mut.Update(ctx, ex); err != nil && !errors.Is(err, runtime.ErrNoRow) {
		return fmt.Errorf("could not record the key's use: %w", err)
	}
	return nil
}

func personAPIKeyFrom(row personapikey.Row) entities.PersonAPIKey {
	key := entities.PersonAPIKey{
		ID:            row.ID,
		ParticipantID: row.ParticipantID,
		Name:          row.Name,
		Prefix:        row.Prefix,
		CreatedAt:     row.CreatedAt,
	}
	if at, ok := row.LastUsedAt.Get(); ok {
		key.LastUsedAt = &at
	}
	if at, ok := row.RevokedAt.Get(); ok {
		key.RevokedAt = &at
	}
	return key
}
