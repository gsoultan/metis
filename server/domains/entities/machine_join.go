package entities

import (
	"crypto/sha256"
	"strings"
	"time"

	"github.com/google/uuid"
)

// MachineJoin is a machine being added to this installation: a one-time code
// an administrator made on the Servers page, the machine that used it, and,
// once an administrator accepted it, this installation's settings sealed to
// that machine's key alone.
type MachineJoin struct {
	ID uuid.UUID `json:"id"`
	// CodeHash is the SHA-256 of the code. The code itself is shown once and
	// kept nowhere.
	CodeHash string `json:"-"`
	// Role is what the new machine will run: all, web or worker.
	Role      string    `json:"role"`
	CreatedBy string    `json:"created_by"`
	CreatedAt time.Time `json:"created_at"`
	// ExpiresAt is when the join ends, whatever step it reached.
	ExpiresAt time.Time `json:"expires_at"`
	// Name is the host name the machine gave when it used the code.
	Name string `json:"name,omitzero"`
	// Key is the machine's X25519 public key, base64: what the settings are
	// sealed to.
	Key string `json:"-"`
	// Words are read from the code and the machine's key, and shown on the
	// machine and on the Servers page. A machine in the middle that swapped
	// the key would change them.
	Words       string     `json:"words,omitzero"`
	RequestedAt *time.Time `json:"requested_at,omitzero"`
	AcceptedBy  string     `json:"accepted_by,omitzero"`
	AcceptedAt  *time.Time `json:"accepted_at,omitzero"`
	// Sealed is the settings sealed to Key, until the machine collects them.
	Sealed      string     `json:"-"`
	CollectedAt *time.Time `json:"collected_at,omitzero"`
}

// MachineJoinWords are the four words a joining machine and the Servers page
// both show, read from the code and the machine's key.
func MachineJoinWords(code, key string) string {
	sum := sha256.Sum256([]byte("metis.machine-join/v1\n" + code + "\n" + key))
	words := make([]string, 4)
	for i := range words {
		words[i] = connectionWords[sum[i]]
	}
	return strings.Join(words, " ")
}
