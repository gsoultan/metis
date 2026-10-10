package impl

import (
	"context"
	"testing"

	"github.com/google/uuid"

	"github.com/gsoultan/metis/server/domains/entities"
	"github.com/gsoultan/metis/server/repositories"
	"github.com/gsoultan/metis/server/repositories/db"
	"github.com/gsoultan/metis/tests/testutils"
)

// failingSnapshots fails the way a real write does on PostgreSQL: with a
// statement the server refuses, inside the caller's transaction.
type failingSnapshots struct{ conn *db.Conn }

func (f failingSnapshots) CaptureSnapshot(ctx context.Context, _ entities.VariableSnapshot) error {
	ex, err := f.conn.Executor(ctx)
	if err != nil {
		return err
	}
	_, err = ex.Exec(ctx, "SELECT 1/0", nil)
	return err
}

// TestAFailedSnapshotDoesNotFailTheTransaction: a snapshot is history, and its
// failure is logged rather than returned so that it cannot fail the business
// transaction around it. On PostgreSQL that was not true — the failed INSERT
// aborted the transaction, and the step's next statement failed with it.
func TestAFailedSnapshotDoesNotFailTheTransaction(t *testing.T) {
	gormDB := testutils.SetupTestDB(t)
	conn := testutils.StormConn(gormDB)
	repo := repositories.NewRepository(conn)
	e := &Engine{repo: repo, varHistory: failingSnapshots{conn: conn}}
	ctx := entities.WithSystemContext(t.Context())

	err := repo.UnitOfWork().Do(ctx, func(txCtx context.Context) error {
		e.captureVariableSnapshot(txCtx, entities.ProcessInstance{ID: uuid.New()})
		ex, err := conn.Executor(txCtx)
		if err != nil {
			return err
		}
		_, err = ex.Exec(txCtx, "SELECT 1", nil)
		return err
	})
	if err != nil {
		t.Fatalf("the transaction failed after a snapshot did: %v", err)
	}
}
