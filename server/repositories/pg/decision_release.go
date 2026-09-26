package pg

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/gsoultan/metis/server/repositories/store/decisionrelease"
	"github.com/gsoultan/storm/runtime"
)

// MakeLive records that, from now on, an evaluation of key that names no
// version reads version.
//
// One more entry on the key's timeline rather than an overwrite, so what was
// live before, and when that changed, stays readable. Upserted on (key,
// project, moment) as the process timeline is: two writes naming the same
// instant are one change of mind, not two cutovers.
func (r *decisionRepository) MakeLive(ctx context.Context, projectID uuid.UUID, key string, version int) error {
	if err := r.requireProjectInTenant(ctx, projectID); err != nil {
		return err
	}
	ex, err := r.conn.conn.Executor(ctx)
	if err != nil {
		return err
	}
	ins := decisionrelease.Create()
	ins.SetProjectID(projectID)
	ins.SetDecisionKey(key)
	ins.SetVersion(int64(version))
	ins.SetActivateAt(time.Now().UTC())
	ins.SetDeletedAtNull()
	ins.OnConflictDecisionKeyProjectIDActivateAt()
	if _, err := ins.Insert(ctx, ex); err != nil && !errors.Is(err, runtime.ErrConflict) {
		return fmt.Errorf("could not make version %d of %s live: %w", version, key, err)
	}
	return nil
}
