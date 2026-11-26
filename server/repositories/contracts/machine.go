package contracts

import (
	"context"
	"time"

	"github.com/google/uuid"
	"github.com/gsoultan/metis/server/domains/entities"
)

// MachineRepository keeps the processes of this installation, as each last
// said it was. Installation-wide: who may read it is decided before this is
// reached, by the platform administrator gate.
type MachineRepository interface {
	// Heartbeat writes what a process says it is, now: its row, by its id,
	// made the first time.
	Heartbeat(ctx context.Context, m entities.Machine) error
	// List is every process that said anything, the most recently heard
	// first.
	List(ctx context.Context) ([]entities.Machine, error)
	// Forget removes a process last heard from before quietSince.
	// apierr.ErrNotFound when there is no such process, or it was heard from
	// since: a running one is not forgotten.
	Forget(ctx context.Context, id uuid.UUID, quietSince time.Time) error
	// ForgetQuiet removes every process last heard from before then, and says
	// how many.
	ForgetQuiet(ctx context.Context, before time.Time) (int64, error)

	// CreateJoin keeps a join made with a one-time code: its role, who made
	// it, when it ends and the code's SHA-256.
	CreateJoin(ctx context.Context, join entities.MachineJoin) (entities.MachineJoin, error)
	// RequestJoin records the machine that used the code with codeHash: its
	// name, key and words. One UPDATE, so two machines racing with one code
	// cannot both have it. Only a join that ends after endsAfter (and after
	// now) and was not used before is taken: a code is good for less time
	// than the join it starts. apierr.ErrNotFound otherwise.
	RequestJoin(ctx context.Context, codeHash string, endsAfter, now time.Time, name, key, words string) (entities.MachineJoin, error)
	// Joins is every join that has not ended at now and was not collected,
	// the newest first.
	Joins(ctx context.Context, now time.Time) ([]entities.MachineJoin, error)
	// AcceptJoin keeps the sealed settings of a join a machine asked for and
	// nobody accepted yet, as accepted by by. apierr.ErrNotFound when there
	// is no such join, or it ended at now.
	AcceptJoin(ctx context.Context, id uuid.UUID, by, sealed string, now time.Time) error
	// JoinByCode is the join made with the code whose SHA-256 is codeHash,
	// or apierr.ErrNotFound.
	JoinByCode(ctx context.Context, codeHash string) (entities.MachineJoin, error)
	// CollectJoin marks an accepted join collected and empties its sealed
	// settings, so they are handed out once. apierr.ErrNotFound when it was
	// collected already, or is not accepted.
	CollectJoin(ctx context.Context, id uuid.UUID, now time.Time) error
	// RemoveJoin removes a join, at whatever step: an administrator refusing
	// the machine, or taking back a code. apierr.ErrNotFound when there is
	// none.
	RemoveJoin(ctx context.Context, id uuid.UUID) error
	// ForgetEndedJoins removes the joins that ended before then, and says how
	// many.
	ForgetEndedJoins(ctx context.Context, before time.Time) (int64, error)
}
