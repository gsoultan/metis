package pg

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"

	"github.com/gsoultan/metis/internal/pkg/apierr"
	"github.com/gsoultan/metis/server/domains/entities"
	"github.com/gsoultan/metis/server/repositories/contracts"
	"github.com/gsoultan/metis/server/repositories/db"
	"github.com/gsoultan/metis/server/repositories/store/machine"
	"github.com/gsoultan/storm/runtime"
)

type machineRepository struct{ conn }

// NewMachineRepository returns the store of this installation's processes.
// Installation-wide, so no tenant scope applies.
func NewMachineRepository(c *db.Conn) contracts.MachineRepository {
	return &machineRepository{conn{conn: c}}
}

// maxMachines bounds the processes read: an installation has a few web and
// worker machines, and the quiet ones are forgotten after a day.
const maxMachines = 200

func (r *machineRepository) Heartbeat(ctx context.Context, m entities.Machine) error {
	ex, err := r.conn.conn.MainExecutor(ctx)
	if err != nil {
		return err
	}
	var ins machine.Ins
	ins.SetID(m.ID)
	ins.SetName(m.Name)
	ins.SetAddress(m.Address)
	ins.SetRole(m.Role)
	ins.SetVersion(m.Version)
	ins.SetStartedAt(m.StartedAt.UTC())
	ins.SetLastSeenAt(m.LastSeenAt.UTC())
	ins.SetUpdatedAt(m.LastSeenAt.UTC())
	ins.OnConflictID()
	if _, err := ins.Insert(ctx, ex); err != nil && !errors.Is(err, runtime.ErrConflict) {
		return fmt.Errorf("could not record that this machine is running: %w", err)
	}
	return nil
}

func (r *machineRepository) List(ctx context.Context) ([]entities.Machine, error) {
	ex, err := r.conn.conn.MainExecutor(ctx)
	if err != nil {
		return nil, err
	}
	rows, err := machine.New().Order(machine.LastSeenAt.Desc()).Limit(maxMachines).All(ctx, ex, nil)
	if err != nil {
		return nil, fmt.Errorf("could not read the machines: %w", err)
	}
	out := make([]entities.Machine, 0, len(rows))
	for _, row := range rows {
		out = append(out, entities.Machine{
			ID:         uuid.UUID(row.ID),
			Name:       row.Name,
			Address:    row.Address,
			Role:       row.Role,
			Version:    row.Version,
			StartedAt:  row.StartedAt.UTC(),
			LastSeenAt: row.LastSeenAt.UTC(),
		})
	}
	return out, nil
}

func (r *machineRepository) Forget(ctx context.Context, id uuid.UUID, quietSince time.Time) error {
	ex, err := r.conn.conn.MainExecutor(ctx)
	if err != nil {
		return err
	}
	n, err := machine.New().Where(machine.ID.Eq(id), machine.LastSeenAt.Lt(quietSince.UTC())).DeleteAll(ctx, ex)
	if err != nil {
		return fmt.Errorf("could not forget the machine: %w", err)
	}
	if n == 0 {
		return fmt.Errorf("%w: no quiet machine with that id", apierr.ErrNotFound)
	}
	return nil
}

func (r *machineRepository) ForgetQuiet(ctx context.Context, before time.Time) (int64, error) {
	ex, err := r.conn.conn.MainExecutor(ctx)
	if err != nil {
		return 0, err
	}
	n, err := machine.New().Where(machine.LastSeenAt.Lt(before.UTC())).DeleteAll(ctx, ex)
	if err != nil {
		return 0, fmt.Errorf("could not forget the quiet machines: %w", err)
	}
	return n, nil
}
