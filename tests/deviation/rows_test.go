package deviation_test

import (
	"testing"

	"github.com/google/uuid"
	"github.com/gsoultan/metis/server/repositories/store/deviationrequest"
	"github.com/gsoultan/metis/server/repositories/store/instancedeviation"
	"github.com/gsoultan/metis/server/repositories/store/processinstance"
	"github.com/gsoultan/metis/tests/testutils"
	"github.com/gsoultan/storm/runtime"
)

// The rows as the tables hold them, read past the repositories: what a test
// checks the repositories against.

func (h *deviationHarness) ex() runtime.Executor { return testutils.Executor(h.db) }

// ledgerRow is one ledger row as stored.
func (h *deviationHarness) ledgerRow(t *testing.T, id uuid.UUID) instancedeviation.Row {
	t.Helper()
	row, found, err := instancedeviation.New().IDEq(id).One(t.Context(), h.ex())
	if err != nil || !found {
		t.Fatalf("read the ledger row %s: %v (found=%v)", id, err, found)
	}
	return row
}

// requestRow is one request as stored.
func (h *deviationHarness) requestRow(t *testing.T, id uuid.UUID) deviationrequest.Row {
	t.Helper()
	row, found, err := deviationrequest.New().IDEq(id).One(t.Context(), h.ex())
	if err != nil || !found {
		t.Fatalf("read the request %s: %v (found=%v)", id, err, found)
	}
	return row
}

// instanceRow is one instance as stored.
func (h *deviationHarness) instanceRow(t *testing.T, id uuid.UUID) processinstance.Row {
	t.Helper()
	row, found, err := processinstance.New().IDEq(id).One(t.Context(), h.ex())
	if err != nil || !found {
		t.Fatalf("read the instance %s: %v (found=%v)", id, err, found)
	}
	return row
}

// rewriteRequest changes a request's row by hand: a state nothing in the
// product writes, or time passing.
func (h *deviationHarness) rewriteRequest(t *testing.T, id uuid.UUID, change func(*deviationrequest.Mut)) {
	t.Helper()
	mut := deviationrequest.MutateKey(id)
	change(&mut)
	if err := mut.Update(t.Context(), h.ex()); err != nil {
		t.Fatalf("rewrite the request %s: %v", id, err)
	}
}

// text is a JSON column as text, "" for none.
func text(v runtime.JSON) string { return string(v) }
