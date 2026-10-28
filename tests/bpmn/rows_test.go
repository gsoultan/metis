package bpmn_test

import (
	"context"
	"encoding/json"
	"slices"
	"testing"
	"time"

	taskstore "github.com/gsoultan/metis/server/repositories/store/task"

	"github.com/google/uuid"
	"github.com/gsoultan/metis/server/repositories/store/deviationrequest"
	"github.com/gsoultan/metis/server/repositories/store/instancedeviation"
	"github.com/gsoultan/metis/server/repositories/store/processdefinition"
	"github.com/gsoultan/metis/server/repositories/store/processinstance"
	"github.com/gsoultan/metis/tests/testutils"
	"github.com/gsoultan/storm/pgstat"
	"github.com/gsoultan/storm/runtime"
)

// The rows as the tables hold them, read and written past the code under
// test: what a test checks it against, and the states nothing in the product
// writes.

func (h engineHarness) ex() runtime.Executor { return testutils.Executor(h.db) }

// some is a value a nullable column holds.
func some[T any](v T) runtime.Null[T] { return runtime.Null[T]{V: v, Valid: true} }

// taken is how many rows a locking read took.
func taken[R any](rows []R, err error) (int, error) { return len(rows), err }

// rewriteRequest writes change to one request, whatever its state.
func (h engineHarness) rewriteRequest(t *testing.T, requestID uuid.UUID, change func(*deviationrequest.Mut)) {
	t.Helper()
	var mut deviationrequest.Mut
	change(&mut)
	if n, err := deviationrequest.New().IDEq(requestID).UpdateAll(h.Ctx(), h.ex(), mut); err != nil || n != 1 {
		t.Fatalf("rewrite request %s: %d row(s), %v", requestID, n, err)
	}
}

// requestRow is a request as its table holds it, whatever the clock says.
func (h engineHarness) requestRow(t *testing.T, requestID uuid.UUID) deviationrequest.Row {
	t.Helper()
	row, found, err := deviationrequest.New().IDEq(requestID).One(h.Ctx(), h.ex())
	if err != nil || !found {
		t.Fatalf("read request %s: %v (found=%v)", requestID, err, found)
	}
	return row
}

// pastDue moves a request's deadline a minute into the past.
func (h engineHarness) pastDue(t *testing.T, requestID uuid.UUID) {
	t.Helper()
	h.rewriteRequest(t, requestID, func(m *deviationrequest.Mut) { m.SetExpiresAt(time.Now().Add(-time.Minute)) })
}

// breakSealed is a sealed JSON string with its end overwritten: ciphertext
// that no longer opens.
func breakSealed(t *testing.T, stored runtime.JSON) runtime.JSON {
	t.Helper()
	var sealed string
	if err := json.Unmarshal(stored, &sealed); err != nil {
		t.Fatalf("read the sealed column: %v", err)
	}
	broken, err := json.Marshal(sealed[:len(sealed)-8] + "AAAAAAAA")
	if err != nil {
		t.Fatalf("break the sealed column: %v", err)
	}
	return broken
}

// seedDefinitions records versions of key, numbered first to first+n-1,
// created at.
func (h engineHarness) seedDefinitions(t *testing.T, key, name string, n int, at time.Time) {
	t.Helper()
	rows := make([]processdefinition.Row, n)
	for i := range rows {
		rows[i] = processdefinition.Row{
			ID: uuid.New(), CreatedAt: at, UpdatedAt: at, ProjectID: h.projID,
			Key: key, Name: name, Version: int64(i + 1), Nodes: runtime.JSON(`[]`), Flows: runtime.JSON(`[]`),
		}
	}
	if _, err := processdefinition.InsertAll(h.Ctx(), h.ex(), rows); err != nil {
		t.Fatalf("seed %d versions of %s: %v", n, key, err)
	}
}

// seedInstancesLike records n instances of like's version in status, created a
// minute from now; parent names the instance that called them, if any.
func (h engineHarness) seedInstancesLike(ctx context.Context, t *testing.T, like uuid.UUID, parent *uuid.UUID, status string, n int) {
	t.Helper()
	of, found, err := processinstance.New().IDEq(like).One(ctx, h.ex())
	if err != nil || !found {
		t.Fatalf("read the instance to seed like: %v (found=%v)", err, found)
	}
	at := time.Now().Add(time.Minute)
	rows := make([]processinstance.Row, n)
	for i := range rows {
		rows[i] = processinstance.Row{
			ID: uuid.New(), CreatedAt: at, UpdatedAt: at, ProjectID: h.projID, DefinitionID: of.DefinitionID,
			Status: status, Tokens: runtime.JSON(`[]`), CompletedNodes: runtime.JSON(`[]`),
			CompensatedNodes: runtime.JSON(`[]`), MultiInstance: runtime.JSON(`{}`), Joins: runtime.JSON(`{}`),
		}
		if parent != nil {
			rows[i].ParentInstanceID = some[[16]byte](*parent)
		}
	}
	if _, err := processinstance.InsertAll(ctx, h.ex(), rows); err != nil {
		t.Fatalf("seed %d instances: %v", n, err)
	}
}

// forgetIterations puts the instance's tasks back the way a release before
// migration 31 wrote them: no iteration recorded.
func (h engineHarness) forgetIterations(t *testing.T, instanceID uuid.UUID) {
	t.Helper()
	var forgotten taskstore.Mut
	forgotten.SetIterationIDNull()
	if _, err := taskstore.New().InstanceIDEq(instanceID).UpdateAll(h.Ctx(), h.ex(), forgotten); err != nil {
		t.Fatalf("forget the iterations: %v", err)
	}
}

// breakLedgerRows leaves the ledger rows of requests rejected and holding
// nothing, while the requests still wait: a state the product does not make.
func (h engineHarness) breakLedgerRows(t *testing.T, requests ...uuid.UUID) {
	t.Helper()
	var broken instancedeviation.Mut
	broken.SetStatus("rejected")
	broken.SetLiveVisitKeyNull()
	if _, err := instancedeviation.New().RequestIDIn(ids(requests)...).UpdateAll(h.Ctx(), h.ex(), broken); err != nil {
		t.Fatalf("break the ledger rows of %d request(s): %v", len(requests), err)
	}
}

// waitingBehind counts the sessions waiting for a lock session holds: for it,
// or in a line behind one that is.
//
// A CREATE INDEX CONCURRENTLY anywhere in the database, such as another
// package's migration test, waits for every transaction already open, the
// holder's among them, on its virtual transaction id. It is waiting for no row
// the holder holds, so it is not counted.
func (h engineHarness) waitingBehind(t *testing.T, session int32) int {
	t.Helper()
	sessions, err := pgstat.Sessions(h.Ctx(), h.ex())
	if err != nil {
		t.Fatalf("look for what is waiting for the held rows: %v", err)
	}
	behind := map[int32]bool{session: true}
	for grew := true; grew; {
		grew = false
		for _, s := range sessions {
			if behind[s.PID] || s.WaitEventType != "Lock" || s.WaitEvent == "virtualxid" {
				continue
			}
			if slices.ContainsFunc(s.BlockedBy, func(pid int32) bool { return behind[pid] }) {
				behind[s.PID], grew = true, true
			}
		}
	}
	return len(behind) - 1
}

// ids is uuids as the stores take them.
func ids(of []uuid.UUID) [][16]byte {
	keys := make([][16]byte, len(of))
	for i, id := range of {
		keys[i] = id
	}
	return keys
}
