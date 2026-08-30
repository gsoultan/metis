package impl

import (
	"context"
	"errors"
	"fmt"

	"github.com/gsoultan/metis/server/repositories/contracts"
	"gorm.io/gorm"
)

// Deploying a process or a decision allocates a version number by reading the
// highest one already used and adding one. Two deployments of the same key that
// read at the same moment propose the same number, and before the unique index
// on (project_id, key, version) existed both writes succeeded: two rows claimed
// to be version 4, `GetByKeyAndVersion` returned whichever the engine happened
// to sort first, and a caller asking to start "version 4" got a coin flip
// between two different processes.
//
// The database now refuses the second write. This file turns that refusal into
// the thing the caller wanted — the next free number — instead of an error.

// versionAttempts bounds the retry.
//
// Every retry is caused by a competing deployment of the same key winning the
// insert, and each competitor deploys once, so the loop is bounded by how many
// deployments of one key are genuinely in flight rather than by chance: with N
// racing, the unluckiest caller succeeds on attempt N. Sixteen is far past any
// real pipeline, and past it, refusing is more honest than looping.
const versionAttempts = 16

// allocateVersion assigns the next free version and stores the record.
//
// propose reads the highest version in use; store writes the record carrying the
// proposed one. They are separate because only store belongs in a transaction:
// the read is a proposal that the unique index arbitrates, so widening the
// transaction to cover it would buy nothing and hold a snapshot open across the
// retry.
func allocateVersion(
	ctx context.Context,
	uow contracts.UnitOfWork,
	subject string,
	propose func(ctx context.Context) (int, error),
	store func(ctx context.Context, version int) error,
) error {
	var (
		lastProposed int
		lastErr      error
	)

	for attempt := range versionAttempts {
		version, err := propose(ctx)
		if err != nil {
			return err
		}

		// Nothing moved since the refused write, so that write was refused by
		// some other constraint — a caller-supplied ID that already exists, say.
		// Retrying would loop on the same error and report it as contention.
		if attempt > 0 && version == lastProposed {
			return lastErr
		}
		lastProposed = version

		err = uow.Attempt(ctx, func(txCtx context.Context) error {
			return store(txCtx, version)
		})
		if err == nil {
			return nil
		}
		if !errors.Is(err, gorm.ErrDuplicatedKey) {
			return err
		}
		lastErr = err
	}

	return fmt.Errorf("could not allocate a version for %s after %d attempts: too many deployments of the same key at once: %w",
		subject, versionAttempts, lastErr)
}
