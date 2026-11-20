package contracts

import (
	"context"
	"time"

	"github.com/google/uuid"
	"github.com/gsoultan/metis/server/domains/entities"
)

// ServerConnectionRepository keeps the servers this installation exchanges
// releases with, the one-time codes they connect with and the key it signs
// with. Installation-wide: who may read or change them is decided before
// this is reached, by the platform administrator gate.
type ServerConnectionRepository interface {
	// CreateCode keeps a one-time code by its hash.
	CreateCode(ctx context.Context, code entities.ConnectionCode) error
	// UseCode marks the code with this hash used, once, if it has not
	// expired by now. apierr.ErrNotFound for any code that cannot be used,
	// so a caller learns nothing about which.
	UseCode(ctx context.Context, hash string, now time.Time) (entities.ConnectionCode, error)

	// Create keeps a connection. A server already connected on that side is
	// refused as invalid.
	Create(ctx context.Context, c entities.ServerConnection) (entities.ServerConnection, error)
	// Get reads one connection, with its token opened.
	Get(ctx context.Context, id uuid.UUID) (entities.ServerConnection, error)
	// List reads every connection, oldest first, without tokens.
	List(ctx context.Context) ([]entities.ServerConnection, error)
	// Accept records who accepted an upstream connection that nobody had.
	// apierr.ErrNotFound when there is none waiting with that id.
	Accept(ctx context.Context, id uuid.UUID, by string, at time.Time) (entities.ServerConnection, error)
	// Delete removes a connection.
	Delete(ctx context.Context, id uuid.UUID) error

	// SigningSeed is this installation's signing seed, or false when none has
	// been made.
	SigningSeed(ctx context.Context) ([]byte, bool, error)
	// SaveSigningSeed keeps a new signing seed, sealed, and returns the one
	// that is kept: another replica's, if it made one first.
	SaveSigningSeed(ctx context.Context, seed []byte) ([]byte, error)
}
