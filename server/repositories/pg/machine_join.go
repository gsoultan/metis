package pg

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"

	"github.com/gsoultan/metis/internal/pkg/apierr"
	"github.com/gsoultan/metis/server/domains/entities"
	"github.com/gsoultan/metis/server/repositories/store/machinejoin"
)

// maxMachineJoins bounds the joins read: each lasts an hour, and an
// administrator makes them one at a time.
const maxMachineJoins = 100

func (r *machineRepository) CreateJoin(ctx context.Context, join entities.MachineJoin) (entities.MachineJoin, error) {
	ex, err := r.conn.conn.MainExecutor(ctx)
	if err != nil {
		return entities.MachineJoin{}, err
	}
	ins := machinejoin.Create()
	ins.SetID(uuid.Must(uuid.NewV7()))
	ins.SetCodeHash(join.CodeHash)
	ins.SetRole(join.Role)
	ins.SetCreatedBy(join.CreatedBy)
	ins.SetExpiresAt(join.ExpiresAt.UTC())
	ins.SetName("")
	ins.SetMachineKey("")
	ins.SetWords("")
	ins.SetAcceptedBy("")
	ins.SetSealed("")
	row, err := ins.Insert(ctx, ex)
	if err != nil {
		return entities.MachineJoin{}, fmt.Errorf("could not keep the code: %w", err)
	}
	return machineJoinOf(row), nil
}

func (r *machineRepository) RequestJoin(ctx context.Context, codeHash string, endsAfter, now time.Time, name, key, words string) (entities.MachineJoin, error) {
	ex, err := r.conn.conn.MainExecutor(ctx)
	if err != nil {
		return entities.MachineJoin{}, err
	}
	var asked machinejoin.Mut
	asked.SetName(name)
	asked.SetMachineKey(key)
	asked.SetWords(words)
	asked.SetRequestedAt(now.UTC())
	asked.SetUpdatedAt(now.UTC())
	n, err := machinejoin.New().
		Where(machinejoin.CodeHash.Eq(codeHash), machinejoin.RequestedAt.IsNull(),
			machinejoin.ExpiresAt.Gt(endsAfter.UTC()), machinejoin.ExpiresAt.Gt(now.UTC())).
		UpdateAll(ctx, ex, asked)
	if err != nil {
		return entities.MachineJoin{}, fmt.Errorf("could not use the code: %w", err)
	}
	if n == 0 {
		return entities.MachineJoin{}, fmt.Errorf("%w: no such code", apierr.ErrNotFound)
	}
	return r.JoinByCode(ctx, codeHash)
}

func (r *machineRepository) Joins(ctx context.Context, now time.Time) ([]entities.MachineJoin, error) {
	ex, err := r.conn.conn.MainExecutor(ctx)
	if err != nil {
		return nil, err
	}
	rows, err := machinejoin.New().
		Where(machinejoin.ExpiresAt.Gt(now.UTC()), machinejoin.CollectedAt.IsNull()).
		Order(machinejoin.CreatedAt.Desc()).Limit(maxMachineJoins).All(ctx, ex, nil)
	if err != nil {
		return nil, fmt.Errorf("could not read the machines being added: %w", err)
	}
	out := make([]entities.MachineJoin, 0, len(rows))
	for _, row := range rows {
		out = append(out, machineJoinOf(row))
	}
	return out, nil
}

func (r *machineRepository) AcceptJoin(ctx context.Context, id uuid.UUID, by, sealed string, now time.Time) error {
	ex, err := r.conn.conn.MainExecutor(ctx)
	if err != nil {
		return err
	}
	var accepted machinejoin.Mut
	accepted.SetAcceptedBy(by)
	accepted.SetAcceptedAt(now.UTC())
	accepted.SetSealed(sealed)
	accepted.SetUpdatedAt(now.UTC())
	n, err := machinejoin.New().
		Where(machinejoin.ID.Eq(id), machinejoin.RequestedAt.IsNotNull(), machinejoin.AcceptedAt.IsNull(),
			machinejoin.ExpiresAt.Gt(now.UTC())).
		UpdateAll(ctx, ex, accepted)
	if err != nil {
		return fmt.Errorf("could not accept the machine: %w", err)
	}
	if n == 0 {
		return fmt.Errorf("%w: no machine waiting with that id", apierr.ErrNotFound)
	}
	return nil
}

func (r *machineRepository) JoinByCode(ctx context.Context, codeHash string) (entities.MachineJoin, error) {
	ex, err := r.conn.conn.MainExecutor(ctx)
	if err != nil {
		return entities.MachineJoin{}, err
	}
	row, found, err := machinejoin.New().Where(machinejoin.CodeHash.Eq(codeHash)).One(ctx, ex)
	if err != nil {
		return entities.MachineJoin{}, fmt.Errorf("could not read the code: %w", err)
	}
	if !found {
		return entities.MachineJoin{}, fmt.Errorf("%w: no such code", apierr.ErrNotFound)
	}
	return machineJoinOf(row), nil
}

func (r *machineRepository) CollectJoin(ctx context.Context, id uuid.UUID, now time.Time) error {
	ex, err := r.conn.conn.MainExecutor(ctx)
	if err != nil {
		return err
	}
	var collected machinejoin.Mut
	collected.SetCollectedAt(now.UTC())
	collected.SetSealed("")
	collected.SetUpdatedAt(now.UTC())
	n, err := machinejoin.New().
		Where(machinejoin.ID.Eq(id), machinejoin.AcceptedAt.IsNotNull(), machinejoin.CollectedAt.IsNull()).
		UpdateAll(ctx, ex, collected)
	if err != nil {
		return fmt.Errorf("could not mark the settings collected: %w", err)
	}
	if n == 0 {
		return fmt.Errorf("%w: nothing to collect", apierr.ErrNotFound)
	}
	return nil
}

func (r *machineRepository) RemoveJoin(ctx context.Context, id uuid.UUID) error {
	ex, err := r.conn.conn.MainExecutor(ctx)
	if err != nil {
		return err
	}
	n, err := machinejoin.New().Where(machinejoin.ID.Eq(id)).DeleteAll(ctx, ex)
	if err != nil {
		return fmt.Errorf("could not remove the machine being added: %w", err)
	}
	if n == 0 {
		return fmt.Errorf("%w: no machine being added with that id", apierr.ErrNotFound)
	}
	return nil
}

func (r *machineRepository) ForgetEndedJoins(ctx context.Context, before time.Time) (int64, error) {
	ex, err := r.conn.conn.MainExecutor(ctx)
	if err != nil {
		return 0, err
	}
	n, err := machinejoin.New().Where(machinejoin.ExpiresAt.Lt(before.UTC())).DeleteAll(ctx, ex)
	if err != nil {
		return 0, fmt.Errorf("could not remove the ended joins: %w", err)
	}
	return n, nil
}

func machineJoinOf(row machinejoin.Row) entities.MachineJoin {
	join := entities.MachineJoin{
		ID:         uuid.UUID(row.ID),
		CodeHash:   row.CodeHash,
		Role:       row.Role,
		CreatedBy:  row.CreatedBy,
		CreatedAt:  row.CreatedAt.UTC(),
		ExpiresAt:  row.ExpiresAt.UTC(),
		Name:       row.Name,
		Key:        row.MachineKey,
		Words:      row.Words,
		AcceptedBy: row.AcceptedBy,
		Sealed:     row.Sealed,
	}
	if at, ok := row.RequestedAt.Get(); ok {
		at = at.UTC()
		join.RequestedAt = &at
	}
	if at, ok := row.AcceptedAt.Get(); ok {
		at = at.UTC()
		join.AcceptedAt = &at
	}
	if at, ok := row.CollectedAt.Get(); ok {
		at = at.UTC()
		join.CollectedAt = &at
	}
	return join
}
