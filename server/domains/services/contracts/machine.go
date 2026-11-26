package contracts

import (
	"context"
	"time"

	"github.com/google/uuid"
	"github.com/gsoultan/metis/server/domains/entities"
)

// MachineService is the Servers page: the processes running this
// installation, which share its database, as each last said it was.
type MachineService interface {
	// ListMachines is every process that said it was running, the most
	// recently heard first. A platform administrator's.
	ListMachines(ctx context.Context) ([]entities.Machine, error)
	// ForgetMachine removes a process that stopped saying it was running.
	// One heard from in the last two minutes is refused: it is running, and
	// would only come back. A platform administrator's.
	ForgetMachine(ctx context.Context, id uuid.UUID) error
	// StartMachineHeartbeat says, every few seconds until ctx ends, that this
	// process is running and what it is. Every process does, whatever its
	// role.
	StartMachineHeartbeat(ctx context.Context, self entities.Machine)

	// MakeMachineJoin makes a one-time code for a new machine to join this
	// installation with, running role. A platform administrator's.
	MakeMachineJoin(ctx context.Context, role string) (IssuedMachineJoin, error)
	// ListMachineJoins is every machine being added that has not finished:
	// codes not used yet, and machines waiting to be accepted. A platform
	// administrator's.
	ListMachineJoins(ctx context.Context) ([]entities.MachineJoin, error)
	// AcceptMachineJoin seals this installation's settings to the key of the
	// machine waiting on join id, for it to collect. A platform
	// administrator's, who has checked that the machine shows the same words.
	AcceptMachineJoin(ctx context.Context, id uuid.UUID) (entities.MachineJoin, error)
	// RemoveMachineJoin takes back a code, or refuses the machine that used
	// it. A platform administrator's.
	RemoveMachineJoin(ctx context.Context, id uuid.UUID) error
	// RequestMachineJoin is a new machine using a code: it says its name
	// and key and is told the words to show. Public: the code admits it,
	// once.
	RequestMachineJoin(ctx context.Context, req MachineJoinRequest) (MachineJoinReply, error)
	// CollectMachineJoin is the machine asking whether it was accepted, and
	// once it was, collecting the settings sealed to its key, once. Public:
	// the code admits it.
	CollectMachineJoin(ctx context.Context, code string) (MachineJoinCollection, error)
}

// IssuedMachineJoin is a code just made: shown once, and kept nowhere.
type IssuedMachineJoin struct {
	ID        uuid.UUID `json:"id"`
	Code      string    `json:"code"`
	Role      string    `json:"role"`
	ExpiresAt time.Time `json:"expires_at"`
}

// MachineJoinRequest is what a new machine sends with its code.
type MachineJoinRequest struct {
	// Format names this request's shape, so a machine on an older or newer
	// release is told so rather than misread.
	Format string `json:"format"`
	Code   string `json:"code"`
	// Name is the machine's host name, as the Servers page will show it.
	Name string `json:"name"`
	// Key is the machine's X25519 public key, base64: the settings are
	// sealed to it, and only the machine holding its private half opens
	// them.
	Key string `json:"key"`
}

// MachineJoinReply is what the machine is told: the words to show, which the
// Servers page shows too, and what it will run.
type MachineJoinReply struct {
	Format    string    `json:"format"`
	Words     string    `json:"words"`
	Role      string    `json:"role"`
	ExpiresAt time.Time `json:"expires_at"`
}

// MachineJoinCollection is the answer to a machine waiting: still waiting,
// or the settings sealed to its key.
type MachineJoinCollection struct {
	Format string `json:"format"`
	// Accepted is false while no administrator has accepted the machine.
	Accepted bool `json:"accepted"`
	// Sealed is this installation's config.yaml sealed to the machine's
	// key, base64, once it was accepted. Handed out once.
	Sealed string `json:"sealed,omitzero"`
}

// MachineJoinFormat names the shape of a join request and its answers.
const MachineJoinFormat = "metis.machine-join/v1"
