package contracts

import (
	"context"
	"encoding/json"
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

	// SendRelease sends a release cut or imported here to a server this one
	// gives releases to. That server collects it the next time it checks in,
	// within a minute, so nothing outside needs to reach it.
	SendRelease(ctx context.Context, projectID uuid.UUID, release string, connectionID uuid.UUID) (entities.ReleaseDelivery, error)
	// ListReleaseDeliveries reads where a project's releases were sent and
	// how far each got there, as of each server's last check-in.
	ListReleaseDeliveries(ctx context.Context, projectID uuid.UUID) ([]entities.ReleaseDelivery, error)
	// AnswerReleaseCollection is what a server this one gives releases to is
	// handed when it checks in: the releases sent to it that it has not
	// settled. It is called with no account: the connection's token admits it.
	AnswerReleaseCollection(ctx context.Context, token string) (ReleaseCollection, error)
	// AnswerReleaseReport keeps what that server says about those releases.
	// The token admits it, as above.
	AnswerReleaseReport(ctx context.Context, token string, report ReleaseReport) error
	// CollectReleases checks in now with a server this one takes releases
	// from, takes what was sent, staged, and says how far each got here.
	// It is what this server does by itself every minute.
	CollectReleases(ctx context.Context, connectionID uuid.UUID) (CollectedReleases, error)
	// StartReleaseCollection checks in with every accepted server this one
	// takes releases from, every minute, until ctx ends.
	StartReleaseCollection(ctx context.Context)
}

// ReleaseCollectionFormat names the shape of a check-in and its report.
const ReleaseCollectionFormat = "metis.release-collection/v1"

// ReleaseCollection is what a server is handed when it checks in.
type ReleaseCollection struct {
	Format   string             `json:"format"`
	Releases []CollectedRelease `json:"releases"`
	// More is true when releases were held back to keep the answer small:
	// the server checks in again at once for them.
	More bool `json:"more,omitzero"`
}

// CollectedRelease is one release sent to the server checking in. File is
// the signed release file while the server has not said it has the release;
// LiveNote is the sending server's signed note that it went live there, once
// it has.
type CollectedRelease struct {
	ID           uuid.UUID       `json:"id"`
	Organization string          `json:"organization"`
	Project      string          `json:"project"`
	Release      string          `json:"release"`
	File         json.RawMessage `json:"file,omitempty"`
	LiveNote     json.RawMessage `json:"live_note,omitempty"`
}

// ReleaseReport is what a server says about the releases it was handed.
type ReleaseReport struct {
	Format  string           `json:"format"`
	Reports []DeliveryReport `json:"reports"`
}

// DeliveryReport is how far one release got on the server that took it.
type DeliveryReport struct {
	ID     uuid.UUID              `json:"id"`
	State  entities.DeliveryState `json:"state"`
	Detail string                 `json:"detail,omitzero"`
}

// CollectedReleases is what one check-in with a server took, and how far
// each release got here.
type CollectedReleases struct {
	Connection entities.ServerConnection `json:"connection"`
	Releases   []CollectedReleaseResult  `json:"releases"`
}

// CollectedReleaseResult is one release a check-in handled here.
type CollectedReleaseResult struct {
	Organization string                 `json:"organization"`
	Project      string                 `json:"project"`
	Release      string                 `json:"release"`
	State        entities.DeliveryState `json:"state"`
	Detail       string                 `json:"detail,omitzero"`
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
