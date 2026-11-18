package app

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/jackc/pgx/v5/stdlib"
	"github.com/rs/zerolog/log"

	"github.com/gsoultan/metis/internal/pkg/apierr"
	"github.com/gsoultan/metis/internal/pkg/crypto"
	"github.com/gsoultan/metis/internal/pkg/identity"
	"github.com/gsoultan/metis/server/domains/adapters"
	"github.com/gsoultan/metis/server/domains/entities"
	repocontracts "github.com/gsoultan/metis/server/repositories/contracts"
)

// identityPoolSize bounds the Identity's own pool. It answers one lookup per
// request, beside the engine's pool, not instead of it.
const identityPoolSize = 8

// UseIdentity hands signing in to the program that embeds the engine. See
// engine.WithIdentity.
func (a *App) UseIdentity(build identity.Factory) {
	a.identityFactory = build
}

// buildIdentity calls the embedding program's factory, once the database is
// open and migrated. Every path that starts the engine runs it, the
// maintenance commands among them, so a password they set reaches the
// Identity and not the engine's own column.
func (a *App) buildIdentity(ctx context.Context) error {
	if a.identityFactory == nil {
		return nil
	}
	db, err := openIdentityDB(a.storm.Main())
	if err != nil {
		return err
	}
	env := identity.Env{
		DB:       db,
		Accounts: identityAccounts{users: a.repo.User()},
		Seal:     crypto.Encrypt,
		Open:     crypto.Decrypt,
	}
	built, err := a.identityFactory(ctx, env)
	if err != nil {
		return errors.Join(fmt.Errorf("the embedding program's sign-in could not start: %w", err), db.Close())
	}
	if built == nil {
		return errors.Join(errors.New("the embedding program's sign-in returned no Identity"), db.Close())
	}
	a.identity, a.identityDB = built, db
	log.Info().Msg("Signing in is served by the program embedding the engine")
	return nil
}

// closeIdentity closes the Identity's pool, after the servers have stopped.
func (a *App) closeIdentity() {
	if a.identityDB != nil {
		if err := a.identityDB.Close(); err != nil {
			log.Debug().Err(err).Msg("Could not close the identity's database pool")
		}
	}
}

// openIdentityDB opens a database/sql pool of its own on the main database,
// with the connection settings the engine's pool was opened with.
func openIdentityDB(main *pgxpool.Pool) (*sql.DB, error) {
	if main == nil {
		return nil, errors.New("the database is not open")
	}
	db := stdlib.OpenDB(*main.Config().ConnConfig.Copy())
	db.SetMaxOpenConns(identityPoolSize)
	db.SetMaxIdleConns(identityPoolSize / 2)
	return db, nil
}

// identityAccounts reads the engine's accounts for the Identity.
type identityAccounts struct {
	users repocontracts.UserRepository
}

func (r identityAccounts) ByUsername(ctx context.Context, username string) (identity.Account, bool, error) {
	m, err := r.users.GetByUsername(entities.WithSystemContext(ctx), username)
	if err != nil {
		return notFoundOr(err)
	}
	return accountOf(adapters.UserEntityAdapter{Model: m}.ToEntity()), true, nil
}

func (r identityAccounts) ByID(ctx context.Context, userID string) (identity.Account, bool, error) {
	// An id that is not a uuid names no account; it is not a failure.
	id, valid := accountID(userID)
	if !valid {
		return identity.Account{}, false, nil
	}
	m, err := r.users.Get(entities.WithSystemContext(ctx), id)
	if err != nil {
		return notFoundOr(err)
	}
	return accountOf(adapters.UserEntityAdapter{Model: m}.ToEntity()), true, nil
}

func accountID(userID string) (uuid.UUID, bool) {
	id, err := uuid.Parse(userID)
	return id, err == nil
}

func notFoundOr(err error) (identity.Account, bool, error) {
	if errors.Is(err, apierr.ErrNotFound) {
		return identity.Account{}, false, nil
	}
	return identity.Account{}, false, err
}

func accountOf(u entities.User) identity.Account {
	roles := make(map[string][]string, len(u.Organizations))
	for _, organization := range u.Organizations {
		if organization != nil {
			roles[organization.ID.String()] = u.RolesIn(organization.ID)
		}
	}
	return identity.Account{UserID: u.ID.String(), Username: u.Username, Roles: roles}
}

// identityCredentials is the Identity as the services hold credentials.
type identityCredentials struct {
	id identity.Identity
}

func (c identityCredentials) SetPassword(ctx context.Context, userID uuid.UUID, plain string) error {
	return c.id.SetPassword(ctx, userID.String(), plain)
}

func (c identityCredentials) RemoveSecondFactor(ctx context.Context, userID uuid.UUID) error {
	return c.id.RemoveSecondFactor(ctx, userID.String())
}
