package entities

import (
	"time"

	"github.com/google/uuid"
)

// Device platforms a person's app reports when it registers for push.
const (
	DevicePlatformAndroid = "android"
	DevicePlatformIOS     = "ios"
	DevicePlatformWeb     = "web"
)

// PersonDevice is a phone or browser a person registered to receive push
// notifications on, as the Firebase SDK in their app identified it.
//
// The token is not a credential: it lets the holder of the project's Firebase
// key reach that one device, nothing more. It is still never sent back to
// anybody but the server, because a list of them is a list of where people are
// reachable.
type PersonDevice struct {
	ID            uuid.UUID `json:"id"`
	ProjectID     uuid.UUID `json:"-"`
	ParticipantID uuid.UUID `json:"-"`
	Token         string    `json:"-"`
	// TokenTail is the token's last characters, so a list can tell two
	// devices of the same name apart.
	TokenTail  string     `json:"token_tail"`
	Platform   string     `json:"platform"`
	Label      string     `json:"label,omitzero"`
	CreatedAt  time.Time  `json:"created_at"`
	LastSentAt *time.Time `json:"last_sent_at,omitzero"`
}
