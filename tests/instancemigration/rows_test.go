package instancemigration

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/gsoultan/metis/server/repositories/db"
	"github.com/gsoultan/metis/server/repositories/models"
	"github.com/gsoultan/metis/server/repositories/store/deviationrequest"
	"github.com/gsoultan/metis/server/repositories/store/incident"
	jobstore "github.com/gsoultan/metis/server/repositories/store/job"
	"github.com/gsoultan/metis/server/repositories/store/processinstance"
	"github.com/gsoultan/metis/server/repositories/store/subscription"
	taskstore "github.com/gsoultan/metis/server/repositories/store/task"
	"github.com/gsoultan/metis/tests/testutils"
	"github.com/gsoultan/storm/pgstat"
	"github.com/gsoultan/storm/runtime"
	"github.com/gsoultan/storm/runtime/pgxdrv"
)

// The rows as the tables hold them, read and written past the code under
// test: what a test checks it against, and the states nothing in the product
// writes.

func (f *fixture) ex() runtime.Executor { return testutils.Executor(f.db) }

// bringDue makes every timer of the instance due a minute ago: the time it
// waits for, passed.
func (f *fixture) bringDue(t *testing.T, instanceID uuid.UUID) {
	t.Helper()
	var due jobstore.Mut
	due.SetNextRunAt(time.Now().Add(-time.Minute))
	if _, err := jobstore.New().InstanceIDEq(instanceID).UpdateAll(f.ctx, f.ex(), due); err != nil {
		t.Fatalf("bring the timer due: %v", err)
	}
}

// rewriteRequest changes a request's row by hand.
func (f *fixture) rewriteRequest(t *testing.T, id uuid.UUID, change func(*deviationrequest.Mut)) {
	t.Helper()
	mut := deviationrequest.MutateKey(id)
	change(&mut)
	if err := mut.Update(f.ctx, f.ex()); err != nil {
		t.Fatalf("rewrite the request %s: %v", id, err)
	}
}

// rewriteInstance changes an instance's row by hand.
func (f *fixture) rewriteTasks(t *testing.T, q taskstore.Query, change func(*taskstore.Mut)) int64 {
	t.Helper()
	var mut taskstore.Mut
	change(&mut)
	n, err := q.UpdateAll(f.ctx, f.ex(), mut)
	if err != nil {
		t.Fatalf("rewrite the tasks: %v", err)
	}
	return n
}

// heldTask is a change to a task somebody has written and not yet committed.
type heldTask struct {
	session int32
	commit  func() error
}

// holdTask writes change to one task in a transaction it leaves open, so the
// row is the transaction's until commit; it is rolled back at the end of the
// test otherwise.
func (f *fixture) holdTask(t *testing.T, id uuid.UUID, change func(*taskstore.Mut)) heldTask {
	t.Helper()
	tx, err := pgxdrv.Pool{P: f.db.Main()}.StartTx(t.Context())
	if err != nil {
		t.Fatalf("open the transaction that holds the task: %v", err)
	}
	settled := false
	t.Cleanup(func() {
		if !settled {
			_ = tx.Rollback(context.Background())
		}
	})
	session, _, err := pgstat.Backend(t.Context(), tx)
	if err != nil {
		t.Fatalf("read the holder's session: %v", err)
	}
	var mut taskstore.Mut
	change(&mut)
	if n, err := taskstore.New().IDEq(id).UpdateAll(t.Context(), tx, mut); err != nil || n != 1 {
		t.Fatalf("write the task: %d row(s), %v", n, err)
	}
	return heldTask{session: session, commit: func() error { settled = true; return tx.Commit(context.Background()) }}
}

// queuedFor is the sessions that wait for a row of table, each with whom it
// waits behind.
func (f *fixture) queuedFor(t *testing.T, table string) []pgstat.Session {
	t.Helper()
	sessions, err := pgstat.Sessions(t.Context(), f.ex())
	if err != nil {
		t.Fatalf("look for what waits for %s: %v", table, err)
	}
	schema := db.Namespace(f.db.Main())
	var queued []pgstat.Session
	for _, s := range sessions {
		// A session queued for a row holds the row's tuple lock while it
		// waits for the transaction ahead of it.
		if len(s.BlockedBy) > 0 && s.LocksOn("tuple", schema, table) {
			queued = append(queued, s)
		}
	}
	return queued
}

// told is a value as a row tells it: "<null>" for none.
func told[T any](v runtime.Null[T]) string {
	if !v.Valid {
		return "<null>"
	}
	return fmt.Sprint(v.V)
}

// stamp is an instant as a row tells it.
func stamp(at time.Time) string { return at.UTC().Format(time.RFC3339Nano) }

func digest(s string) string {
	sum := sha256.Sum256([]byte(s))
	return hex.EncodeToString(sum[:])
}

// jobRows is every live timer and queued call of the instance, oldest first.
func (f *fixture) jobRows(t *testing.T, instanceID uuid.UUID) []string {
	t.Helper()
	rows, err := jobstore.New().InstanceIDEq(instanceID).Order(jobstore.CreatedAt.Asc(), jobstore.ID.Asc()).All(f.ctx, f.ex(), nil)
	if err != nil {
		t.Fatalf("read the jobs: %v", err)
	}
	out := make([]string, len(rows))
	for i, r := range rows {
		out[i] = fmt.Sprintf("definition=%s node=%s type=%s status=%s", uuid.UUID(r.DefinitionID), r.NodeID, r.Type, r.Status)
	}
	return out
}

// waitingEventRows is the messages and signals an instance waits for.
func (f *fixture) waitingEventRows(t *testing.T, instanceID uuid.UUID) []string {
	t.Helper()
	rows, err := subscription.New().InstanceIDEq(instanceID).Order(subscription.NodeID.Asc(), subscription.EventName.Asc()).All(f.ctx, f.ex(), nil)
	if err != nil {
		t.Fatalf("read the waiting events: %v", err)
	}
	out := make([]string, len(rows))
	for i, r := range rows {
		out[i] = fmt.Sprintf("node=%s type=%s event=%s", r.NodeID, r.Type, r.EventName)
	}
	return out
}

// finishedTasks is every completed or canceled task of the instance, each
// told in full as the database holds it: the columns that say what happened.
// updated_at is told with them, and for a task it shows only that nothing
// stamped the row: a task write does not move it. That a finished task is not
// written rests on the code; what is compared here is every column's value.
func (f *fixture) finishedTasks(t *testing.T, instanceID uuid.UUID) map[string]string {
	t.Helper()
	rows, err := taskstore.New().InstanceIDEq(instanceID).
		StatusIn(string(models.TaskCompleted), string(models.TaskCanceled)).Unordered().All(f.ctx, f.ex(), nil)
	if err != nil {
		t.Fatalf("read the finished tasks: %v", err)
	}
	byID := make(map[string]string, len(rows))
	for _, r := range rows {
		due := "<null>"
		if r.DueDate.Valid {
			due = stamp(r.DueDate.V)
		}
		byID[uuid.UUID(r.ID).String()] = fmt.Sprintf(
			"node=%s name=%s description=%s type=%s status=%s assignee=%s owner=%s delegation=%s candidates=%s/%s "+
				"priority=%d due=%s form=%s/%s variables=%s written=%s",
			r.NodeID, r.Name, told(r.Description), r.Type, r.Status, told(r.Assignee), told(r.Owner), told(r.DelegationState),
			r.CandidateUsers, r.CandidateGroups, r.Priority, due, told(r.FormKey), digest(r.FormDefinition.V),
			digest(r.Variables), stamp(r.UpdatedAt))
	}
	return byID
}

// jobsInFull is every timer and queued call of the instance, told in full.
func (f *fixture) jobsInFull(t *testing.T, instanceID uuid.UUID) map[string]string {
	t.Helper()
	rows, err := jobstore.New().InstanceIDEq(instanceID).Unordered().All(f.ctx, f.ex(), nil)
	if err != nil {
		t.Fatalf("read the jobs: %v", err)
	}
	byID := make(map[string]string, len(rows))
	for _, r := range rows {
		byID[uuid.UUID(r.ID).String()] = fmt.Sprintf("definition=%s node=%s type=%s status=%s retries=%d due=%s written=%s",
			uuid.UUID(r.DefinitionID), r.NodeID, r.Type, r.Status, r.Retries, stamp(r.NextRunAt), stamp(r.UpdatedAt))
	}
	return byID
}

// incidentsInFull is every incident of the instance, told in full.
func (f *fixture) incidentsInFull(t *testing.T, instanceID uuid.UUID) map[string]string {
	t.Helper()
	rows, err := incident.New().InstanceIDEq(instanceID).Unordered().All(f.ctx, f.ex(), nil)
	if err != nil {
		t.Fatalf("read the incidents: %v", err)
	}
	byID := make(map[string]string, len(rows))
	for _, r := range rows {
		definition, resolved := "<null>", "<null>"
		if r.DefinitionID.Valid {
			definition = uuid.UUID(r.DefinitionID.V).String()
		}
		if r.ResolvedAt.Valid {
			resolved = stamp(r.ResolvedAt.V)
		}
		byID[uuid.UUID(r.ID).String()] = fmt.Sprintf("definition=%s node=%s status=%s resolved=%s written=%s",
			definition, r.NodeID, r.Status, resolved, stamp(r.UpdatedAt))
	}
	return byID
}

// leaveTaskOn writes a task on nodeID the engine never opened: a copy of the
// instance's task on fromNode, claimed by assignee.
func (f *fixture) leaveTaskOn(t *testing.T, instanceID uuid.UUID, fromNode, nodeID, name, assignee string) {
	t.Helper()
	from, found, err := taskstore.New().InstanceIDEq(instanceID).NodeIDEq(fromNode).One(f.ctx, f.ex())
	if err != nil || !found {
		t.Fatalf("read the task on %s: %v (found=%v)", fromNode, err, found)
	}
	now := time.Now()
	left := taskstore.Row{
		ID: uuid.New(), CreatedAt: now, UpdatedAt: now, ProjectID: from.ProjectID, InstanceID: from.InstanceID,
		NodeID: nodeID, Name: name, Type: from.Type, Status: "claimed", Assignee: some(assignee),
		CandidateUsers: from.CandidateUsers, CandidateGroups: from.CandidateGroups, Priority: from.Priority,
		Variables: from.Variables,
	}
	if _, err := taskstore.InsertAll(f.ctx, f.ex(), []taskstore.Row{left}); err != nil {
		t.Fatalf("leave a task on %s: %v", nodeID, err)
	}
}

// leaveSubscriptionOn writes a wait for a message on nodeID the engine never
// opened.
func (f *fixture) leaveSubscriptionOn(t *testing.T, instanceID uuid.UUID, nodeID, eventName string) {
	t.Helper()
	instance, found, err := processinstance.New().IDEq(instanceID).One(f.ctx, f.ex())
	if err != nil || !found {
		t.Fatalf("read the instance: %v (found=%v)", err, found)
	}
	now := time.Now()
	wait := subscription.Row{
		ID: uuid.New(), CreatedAt: now, UpdatedAt: now, ProjectID: instance.ProjectID, InstanceID: instanceID,
		NodeID: nodeID, Type: "message", EventName: eventName, CorrelationKey: some(""),
	}
	if _, err := subscription.InsertAll(f.ctx, f.ex(), []subscription.Row{wait}); err != nil {
		t.Fatalf("leave a wait on %s: %v", nodeID, err)
	}
}

// some is a value a nullable column holds.
func some[T any](v T) runtime.Null[T] { return runtime.Null[T]{V: v, Valid: true} }
