package task_test

import (
	"testing"

	"github.com/google/uuid"
	"github.com/gsoultan/metis/server/repositories/db"
	taskstore "github.com/gsoultan/metis/server/repositories/store/task"
	"github.com/gsoultan/metis/tests/testutils"
)

// rewriteTask changes a task's row behind the server's back: the way the
// engine writes a status alone, or a pod on an older release leaves a row.
func rewriteTask(t *testing.T, conn *db.Conn, id uuid.UUID, change func(*taskstore.Mut)) {
	t.Helper()
	mut := taskstore.MutateKey(id)
	change(&mut)
	if err := mut.Update(t.Context(), testutils.Executor(conn)); err != nil {
		t.Fatalf("rewrite task %s: %v", id, err)
	}
}

// rewrite is rewriteTask on a task the harness opened.
func (h *taskHarness) rewrite(t *testing.T, id string, change func(*taskstore.Mut)) {
	t.Helper()
	rewriteTask(t, h.db, uuid.MustParse(id), change)
}

// withdraw sets a task's status alone, as the engine withdraws one.
func (h *taskHarness) withdraw(t *testing.T, id string) {
	t.Helper()
	h.rewrite(t, id, func(m *taskstore.Mut) { m.SetStatus("canceled") })
}

// markStale leaves a task the way a pod on the release before delegations had
// owners leaves one it claims or releases: an owner and a pending mark, under
// whatever status it has.
func (h *taskHarness) markStale(t *testing.T, id string) {
	t.Helper()
	h.rewrite(t, id, func(m *taskstore.Mut) {
		m.SetOwner("mallory")
		m.SetDelegationState("pending")
	})
}
