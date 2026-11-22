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
}
