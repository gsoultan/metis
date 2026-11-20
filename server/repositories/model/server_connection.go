package model

import (
	"time"

	"github.com/gsoultan/storm"
)

// ServerConnection is another installation this one exchanges releases with:
// one it takes releases from ("upstream") or gives them to ("downstream").
//
// An upstream one is trusted only once AcceptedAt is set, by an administrator
// other than RequestedBy, and only while Trust opens: it is the row's id and
// Key sealed with ENCRYPTION_KEY when it was accepted, so that writing a row
// into this table is not enough to make this server trust a key. Token is
// sealed the same way: it is what this server presents to the upstream one.
// TokenHash is the SHA-256 of what a downstream one presents here.
type ServerConnection struct {
	storm.Model

	Name        string
	Purpose     string
	Side        string
	URL         string
	Key         string
	Words       string
	Token       string
	TokenHash   string
	Trust       string
	RequestedBy string
	AcceptedBy  string
	AcceptedAt  *time.Time
	LastSeenAt  *time.Time
}

func (c *ServerConnection) Schema(t *storm.Table) {
	t.Name("server_connections")
	t.Col(&c.Name).Size(255)
	t.Col(&c.Purpose).Size(16)
	t.Col(&c.Side).Size(16)
	t.Col(&c.URL).Size(2048)
	t.Col(&c.Key).Size(64)
	t.Col(&c.Words).Size(64)
	t.Col(&c.Token).Size(512)
	t.Col(&c.TokenHash).Size(64)
	t.Col(&c.Trust).Size(512)
	t.Col(&c.RequestedBy).Size(255)
	t.Col(&c.AcceptedBy).Size(255)
	// One connection per server and direction: a key is one installation.
	t.Index(&c.Key, &c.Side).Unique().Named("ux_server_connections_key_side")
}

// ConnectionCode is a one-time code made for another server to connect with.
// Only its SHA-256 is kept, and it is good once, until ExpiresAt.
type ConnectionCode struct {
	storm.Model

	Hash      string
	CreatedBy string
	ExpiresAt time.Time
	UsedAt    *time.Time
}

func (c *ConnectionCode) Schema(t *storm.Table) {
	t.Name("connection_codes")
	t.Col(&c.Hash).Size(64)
	t.Col(&c.CreatedBy).Size(255)
	t.Index(&c.Hash).Unique().Named("ux_connection_codes_hash")
}

// ServerSigningKey is the Ed25519 seed this installation signs its releases
// and live notes with, made the first time one is needed. Seed is sealed with
// ENCRYPTION_KEY. METIS_RELEASE_SIGNING_KEY, when set, is used instead.
type ServerSigningKey struct {
	storm.Model

	Seed string
}

func (k *ServerSigningKey) Schema(t *storm.Table) {
	t.Name("server_signing_keys")
	t.Col(&k.Seed).Size(512)
}

// ReleaseDelivery is a release sent to a server this one gives releases to,
// and what that server last said about it. The release itself is not copied:
// it is signed and read from the releases table each time it is collected.
type ReleaseDelivery struct {
	storm.Model

	ConnectionID string
	ProjectID    string
	Release      string
	State        string
	Detail       string
	SentBy       string
	SentAt       time.Time
	CollectedAt  *time.Time
	ReportedAt   *time.Time
}

func (d *ReleaseDelivery) Schema(t *storm.Table) {
	t.Name("release_deliveries")
	t.Col(&d.ConnectionID).Size(36)
	t.Col(&d.ProjectID).Size(36)
	t.Col(&d.Release).Size(255)
	t.Col(&d.State).Size(16)
	t.Col(&d.Detail).Size(1024)
	t.Col(&d.SentBy).Size(255)
	// A release is sent to a server once; sending it again resends the same.
	t.Index(&d.ConnectionID, &d.ProjectID, &d.Release).Unique().Named("ux_release_deliveries_connection_project_release")
	t.Index(&d.ProjectID).Named("ix_release_deliveries_project")
}
