package contracts

import (
	"context"
	"time"

	"github.com/google/uuid"
	"github.com/gsoultan/metis/server/domains/entities"
)

// ServerConnectionService connects this installation to the servers it takes
// releases from and gives them to, by a one-time code, and keeps the key it
// signs its releases with.
type ServerConnectionService interface {
	// ServerConnectionIdentity is what this server calls itself, is for, signs
	// with and is reached at.
	ServerConnectionIdentity(ctx context.Context) (ServerIdentity, error)
	// MakeServerConnectionCode makes a one-time code for a server after this
	// one to connect with. It is shown once.
	MakeServerConnectionCode(ctx context.Context) (IssuedConnectionCode, error)
	// CreateServerConnection connects to the server before this one, at url,
	// with a code made there. It waits for a second administrator here.
	CreateServerConnection(ctx context.Context, url, code string) (entities.ServerConnection, error)
	// AnswerServerHandshake is the other half of CreateServerConnection, on
	// the server the code was made on. It is called with no account: the
	// code is what admits it.
	AnswerServerHandshake(ctx context.Context, hello ServerHandshake) (ServerHandshakeReply, error)
	// ListServerConnections reads every connection.
	ListServerConnections(ctx context.Context) ([]entities.ServerConnection, error)
	// AcceptServerConnection trusts a server this one takes releases from.
	// Only an administrator other than the one who connected it may.
	AcceptServerConnection(ctx context.Context, id uuid.UUID) (entities.ServerConnection, error)
	// RemoveServerConnection stops exchanging releases with a server.
	RemoveServerConnection(ctx context.Context, id uuid.UUID) error
}

// ServerIdentity is this server as the others it connects to see it.
type ServerIdentity struct {
	Name    string                 `json:"name"`
	Purpose entities.ServerPurpose `json:"purpose"`
	Key     string                 `json:"key"`
	URL     string                 `json:"url,omitzero"`
	// TrustLocked is true when the operator set the keys this server trusts
	// in its environment: it then takes no connection from the pages.
	TrustLocked bool `json:"trust_locked,omitzero"`
}

// IssuedConnectionCode is a one-time code, shown once.
type IssuedConnectionCode struct {
	Code      string    `json:"code"`
	ExpiresAt time.Time `json:"expires_at"`
}

// ServerHandshakeFormat names the shape of the handshake.
const ServerHandshakeFormat = "metis.connection/v1"

// ServerHandshake is what a server sends the one whose code it was given.
type ServerHandshake struct {
	Format  string                 `json:"format"`
	Code    string                 `json:"code"`
	Name    string                 `json:"name"`
	Purpose entities.ServerPurpose `json:"purpose"`
	Key     string                 `json:"key"`
	URL     string                 `json:"url,omitzero"`
}

// ServerHandshakeReply is what the server whose code it was answers. Token is
// what the asking server presents when it collects its releases.
type ServerHandshakeReply struct {
	Format  string                 `json:"format"`
	Name    string                 `json:"name"`
	Purpose entities.ServerPurpose `json:"purpose"`
	Key     string                 `json:"key"`
	Token   string                 `json:"token"`
}
