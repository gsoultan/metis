package pg

import (
	"context"
	"encoding/json"
	"fmt"
	"slices"
	"time"

	"github.com/google/uuid"
	"github.com/gsoultan/metis/internal/pkg/apierr"
	"github.com/gsoultan/metis/server/domains/entities"
	"github.com/gsoultan/metis/server/repositories/contracts"
	"github.com/gsoultan/metis/server/repositories/db"
	"github.com/gsoultan/metis/server/repositories/store/usersecondfactor"
)

type userSecondFactorRepository struct {
	conn *db.Conn
}

// NewUserSecondFactorRepository keeps staff authenticators in the main
// database, whatever port a request arrives on, as the accounts are.
//
// Every change that depends on what is stored — a step taken, a recovery code
// spent, an enrolment confirmed — reads the row locked in the transaction
// that writes it, so two sign-ins racing with one code cannot both succeed.
func NewUserSecondFactorRepository(conn *db.Conn) contracts.UserSecondFactorRepository {
	return &userSecondFactorRepository{conn: conn}
}

func (r *userSecondFactorRepository) Get(ctx context.Context, userID uuid.UUID) (entities.UserSecondFactor, bool, error) {
	row, found, err := r.read(ctx, userID, false)
	if err != nil || !found {
		return entities.UserSecondFactor{}, found, err
	}
	factor, err := secondFactorFrom(row)
	return factor, true, err
}

func (r *userSecondFactorRepository) Begin(ctx context.Context, userID uuid.UUID, secret string) error {
	return r.conn.TransactMain(ctx, func(txCtx context.Context) error {
		row, found, err := r.read(txCtx, userID, true)
		if err != nil {
			return err
		}
		if found && row.ConfirmedAt.Valid {
			return apierr.Forbiddenf("this account already signs in with an authenticator app; an administrator has to remove it first")
		}
		ex, err := r.conn.MainExecutor(txCtx)
		if err != nil {
			return err
		}
		if found {
			mut := usersecondfactor.Mutate(row)
			mut.SetSecret(secret)
			mut.SetLastStep(0)
			mut.SetUpdatedAtNow()
			return wrapWrite("begin the enrolment", mut.Update(txCtx, ex))
		}
		ins := usersecondfactor.Create()
		ins.SetID(uuid.Must(uuid.NewV7()))
		ins.SetUserID(userID)
		ins.SetSecret(secret)
		ins.SetLastStep(0)
		_, err = ins.Insert(txCtx, ex)
		return wrapWrite("begin the enrolment", err)
	})
}

func (r *userSecondFactorRepository) Confirm(ctx context.Context, userID uuid.UUID, secret string, step int64, recoveryHashes []string, at time.Time) (bool, error) {
	codes, err := json.Marshal(recoveryHashes)
	if err != nil {
		return false, fmt.Errorf("could not encode the recovery codes: %w", err)
	}
	confirmed := false
	err = r.conn.TransactMain(ctx, func(txCtx context.Context) error {
		row, found, err := r.read(txCtx, userID, true)
		if err != nil || !found {
			return err
		}
		if row.ConfirmedAt.Valid || row.Secret != secret || step <= row.LastStep {
			return nil
		}
		mut := usersecondfactor.Mutate(row)
		mut.SetConfirmedAt(at.UTC())
		mut.SetLastStep(step)
		mut.SetRecoveryCodes(codes)
		mut.SetUpdatedAtNow()
		if err := r.update(txCtx, &mut, "confirm the enrolment"); err != nil {
			return err
		}
		confirmed = true
		return nil
	})
	return confirmed, err
}

func (r *userSecondFactorRepository) Delete(ctx context.Context, userID uuid.UUID) (bool, error) {
	ex, err := r.conn.MainExecutor(ctx)
	if err != nil {
		return false, err
	}
	n, err := usersecondfactor.New().Where(usersecondfactor.UserID.Eq(userID)).DeleteAll(ctx, ex)
	if err != nil {
		return false, fmt.Errorf("could not remove the authenticator: %w", err)
	}
	return n > 0, nil
}

func (r *userSecondFactorRepository) TakeStep(ctx context.Context, userID uuid.UUID, step int64) (bool, error) {
	taken := false
	err := r.conn.TransactMain(ctx, func(txCtx context.Context) error {
		row, found, err := r.read(txCtx, userID, true)
		if err != nil || !found || !row.ConfirmedAt.Valid || step <= row.LastStep {
			return err
		}
		mut := usersecondfactor.Mutate(row)
		mut.SetLastStep(step)
		mut.SetUpdatedAtNow()
		if err := r.update(txCtx, &mut, "record the code's use"); err != nil {
			return err
		}
		taken = true
		return nil
	})
	return taken, err
}

func (r *userSecondFactorRepository) TakeRecoveryCode(ctx context.Context, userID uuid.UUID, hash string) (bool, error) {
	taken := false
	err := r.conn.TransactMain(ctx, func(txCtx context.Context) error {
		row, found, err := r.read(txCtx, userID, true)
		if err != nil || !found || !row.ConfirmedAt.Valid {
			return err
		}
		factor, err := secondFactorFrom(row)
		if err != nil {
			return err
		}
		at := slices.Index(factor.RecoveryCodeHashes, hash)
		if at < 0 {
			return nil
		}
		left, err := json.Marshal(slices.Delete(factor.RecoveryCodeHashes, at, at+1))
		if err != nil {
			return fmt.Errorf("could not encode the recovery codes: %w", err)
		}
		mut := usersecondfactor.Mutate(row)
		mut.SetRecoveryCodes(left)
		mut.SetUpdatedAtNow()
		if err := r.update(txCtx, &mut, "spend the recovery code"); err != nil {
			return err
		}
		taken = true
		return nil
	})
	return taken, err
}

func (r *userSecondFactorRepository) ReplaceRecoveryCodes(ctx context.Context, userID uuid.UUID, hashes []string) error {
	codes, err := json.Marshal(hashes)
	if err != nil {
		return fmt.Errorf("could not encode the recovery codes: %w", err)
	}
	return r.conn.TransactMain(ctx, func(txCtx context.Context) error {
		row, found, err := r.read(txCtx, userID, true)
		if err != nil {
			return err
		}
		if !found || !row.ConfirmedAt.Valid {
			return fmt.Errorf("%w: the account has no authenticator app", apierr.ErrNotFound)
		}
		mut := usersecondfactor.Mutate(row)
		mut.SetRecoveryCodes(codes)
		mut.SetUpdatedAtNow()
		return r.update(txCtx, &mut, "replace the recovery codes")
	})
}

// read returns the account's row, locked for the transaction when lock.
func (r *userSecondFactorRepository) read(ctx context.Context, userID uuid.UUID, lock bool) (usersecondfactor.Row, bool, error) {
	ex, err := r.conn.MainExecutor(ctx)
	if err != nil {
		return usersecondfactor.Row{}, false, err
	}
	q := usersecondfactor.New().Where(usersecondfactor.UserID.Eq(userID))
	if lock {
		q = q.ForNoKeyUpdate()
	}
	row, found, err := q.One(ctx, ex)
	if err != nil {
		return usersecondfactor.Row{}, false, fmt.Errorf("could not read the authenticator: %w", err)
	}
	return row, found, nil
}

func (r *userSecondFactorRepository) update(ctx context.Context, mut *usersecondfactor.Mut, what string) error {
	ex, err := r.conn.MainExecutor(ctx)
	if err != nil {
		return err
	}
	return wrapWrite(what, mut.Update(ctx, ex))
}

func wrapWrite(what string, err error) error {
	if err != nil {
		return fmt.Errorf("could not %s: %w", what, err)
	}
	return nil
}

func secondFactorFrom(row usersecondfactor.Row) (entities.UserSecondFactor, error) {
	factor := entities.UserSecondFactor{
		UserID:   row.UserID,
		Secret:   row.Secret,
		LastStep: row.LastStep,
	}
	if at, ok := row.ConfirmedAt.Get(); ok {
		factor.ConfirmedAt = &at
	}
	if len(row.RecoveryCodes) > 0 {
		if err := json.Unmarshal(row.RecoveryCodes, &factor.RecoveryCodeHashes); err != nil {
			return entities.UserSecondFactor{}, fmt.Errorf("could not decode the recovery codes: %w", err)
		}
	}
	return factor, nil
}
