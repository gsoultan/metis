package entities

import (
	"time"

	"github.com/google/uuid"
)

// DeliveryState is how far a release sent to another server has got there,
// as that server last said.
type DeliveryState string

const (
	// DeliveryWaiting is a release sent and not yet taken: the other server
	// collects it the next time it checks in.
	DeliveryWaiting DeliveryState = "waiting"
	// DeliveryStaged is a release the other server took and holds, not live.
	DeliveryStaged DeliveryState = "staged"
	// DeliveryApproved is a staged release someone there agreed may go live.
	DeliveryApproved DeliveryState = "approved"
	// DeliveryLive is a release that went live there.
	DeliveryLive DeliveryState = "live"
	// DeliverySuperseded is a release that was live there and was replaced.
	DeliverySuperseded DeliveryState = "superseded"
	// DeliveryRefused is a release the other server would not take, with why.
	DeliveryRefused DeliveryState = "refused"
)

// Valid says whether s is a state a server may report.
func (s DeliveryState) Valid() bool {
	switch s {
	case DeliveryWaiting, DeliveryStaged, DeliveryApproved, DeliveryLive, DeliverySuperseded, DeliveryRefused:
		return true
	}
	return false
}

// Settled says the other server has nothing more to say about the release:
// it went live, was replaced, or was refused. A settled delivery is no longer
// offered when the other server checks in.
func (s DeliveryState) Settled() bool {
	return s == DeliveryLive || s == DeliverySuperseded || s == DeliveryRefused
}

// ReleaseDelivery is a release this server sent to one it gives releases to,
// and what that server last said about it.
type ReleaseDelivery struct {
	ID           uuid.UUID `json:"id"`
	ConnectionID uuid.UUID `json:"connection_id"`
	// ServerName and ServerPurpose are the receiving server's, when read
	// with the connection.
	ServerName    string        `json:"server_name,omitzero"`
	ServerPurpose ServerPurpose `json:"server_purpose,omitzero"`
	ProjectID     uuid.UUID     `json:"project_id"`
	Release       string        `json:"release"`
	State         DeliveryState `json:"state"`
	// Detail is why the other server refused the release, in its words.
	Detail string    `json:"detail,omitzero"`
	SentBy string    `json:"sent_by"`
	SentAt time.Time `json:"sent_at"`
	// CollectedAt is when the other server first took the release, and
	// ReportedAt when it last said how far it got. Both are "as of the last
	// check-in": a server that stopped checking in is not guessed about.
	CollectedAt *time.Time `json:"collected_at,omitzero"`
	ReportedAt  *time.Time `json:"reported_at,omitzero"`
	// LastSeenAt is when the receiving server last checked in at all.
	LastSeenAt *time.Time `json:"last_seen_at,omitzero"`
}
