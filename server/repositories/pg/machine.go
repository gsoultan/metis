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
	ins.SetRoleFixed(m.RoleFixed)
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
			ID:            uuid.UUID(row.ID),
			Name:          row.Name,
			Address:       row.Address,
			Role:          row.Role,
			Version:       row.Version,
			StartedAt:     row.StartedAt.UTC(),
			LastSeenAt:    row.LastSeenAt.UTC(),
			RequestedRole: row.RequestedRole,
			RoleFixed:     row.RoleFixed,
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

func (r *machineRepository) RequestRole(ctx context.Context, id uuid.UUID, role string, liveSince time.Time) error {
	return r.conn.conn.TransactMain(ctx, func(txCtx context.Context) error {
		ex, err := r.conn.conn.MainExecutor(txCtx)
		if err != nil {
			return err
		}
		// Every running process, locked: a second ask waits here until the
		// first commits, then reads what it left, so two asks cannot each
		// count the other machine as the worker that remains.
		live, err := machine.New().Where(machine.LastSeenAt.Gt(liveSince.UTC())).
			Order(machine.ID.Asc()).Limit(maxMachines).ForUpdate().All(txCtx, ex, nil)
		if err != nil {
			return fmt.Errorf("could not read the running machines: %w", err)
		}
		var target *machine.Row
		working := false
		for i := range live {
			row := &live[i]
			next := row.Role
			if row.RequestedRole != "" {
				next = row.RequestedRole
			}
			if uuid.UUID(row.ID) == id {
				target = row
				next = role
			}
			if next != "web" {
				working = true
			}
		}
		switch {
		case target == nil:
			return fmt.Errorf("%w: no running machine with that id", apierr.ErrNotFound)
		case target.RoleFixed:
			return contracts.ErrMachineRoleFixed
		case !working:
			return contracts.ErrNoMachineLeftWorking
		}
		asked := role
		if role == target.Role {
			asked = ""
		}
		var change machine.Mut
		change.SetRequestedRole(asked)
		change.SetUpdatedAt(time.Now().UTC())
		if _, err := machine.New().Where(machine.ID.Eq(id)).UpdateAll(txCtx, ex, change); err != nil {
			return fmt.Errorf("could not ask the machine to change its role: %w", err)
		}
		return nil
	})
}

func (r *machineRepository) RequestedRole(ctx context.Context, id uuid.UUID) (string, error) {
	ex, err := r.conn.conn.MainExecutor(ctx)
	if err != nil {
		return "", err
	}
	row, found, err := machine.New().Where(machine.ID.Eq(id)).One(ctx, ex)
	if err != nil {
		return "", fmt.Errorf("could not read the role this machine was asked to take: %w", err)
	}
	if !found {
		return "", nil
	}
	return row.RequestedRole, nil
}
