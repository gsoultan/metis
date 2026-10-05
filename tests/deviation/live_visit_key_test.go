package deviation_test

import (
	"database/sql"
	"testing"

	"github.com/google/uuid"
	"github.com/gsoultan/metis/server/domains/entities"
)

// A visit is held by its live row: one that is applied, or waiting for
// approval. A second live row for it is refused by the database whatever its
// id — the last line against acting twice — and a row that was decided against
// holds nothing, so the same thing can be asked for again (migration 34).
func TestALiveRowHoldsItsVisitAndADecidedOneDoesNot(t *testing.T) {
	h := newDeviationHarness(t)
	instanceID := h.startOneStep(t, entities.Node{Name: "Approve", Type: entities.UserTask, Assignee: "alice"})
	row := func(status entities.DeviationStatus, visit string) entities.Deviation {
		d := h.sample(instanceID) // a new id each time
		d.Status, d.VisitKey = status, visit
		return d
	}
	liveKeyOf := func(id uuid.UUID) string {
		t.Helper()
		var live sql.NullString
		if err := h.db.Raw(`SELECT live_visit_key FROM instance_deviations WHERE id = ?`, id).Row().Scan(&live); err != nil {
			t.Fatalf("read the live key: %v", err)
		}
		return live.String
	}

	for _, status := range []entities.DeviationStatus{entities.DeviationApplied, entities.DeviationPendingApproval} {
		visit := "dv1-held-" + string(status)
		first, err := h.write(h.tenantContext(), row(status, visit))
		if err != nil {
			t.Fatalf("write the %s row: %v", status, err)
		}
		if live := liveKeyOf(first.ID); live != visit {
			t.Errorf("a %s row holds its visit with the key %q; want %q", status, live, visit)
		}
		for _, second := range []entities.DeviationStatus{entities.DeviationApplied, entities.DeviationPendingApproval} {
			if _, err := h.write(h.tenantContext(), row(second, visit)); err == nil {
				t.Errorf("a second live row (%s) was written for a visit a %s row holds", second, status)
			}
		}
	}

	for _, status := range []entities.DeviationStatus{entities.DeviationRejected, entities.DeviationExpired, entities.DeviationStale} {
		decided, err := h.write(h.tenantContext(), row(status, "dv1-asked-again"))
		if err != nil {
			t.Fatalf("write the %s row: %v", status, err)
		}
		if live := liveKeyOf(decided.ID); live != "" {
			t.Errorf("a %s row holds its visit with the key %q; it would refuse every later request for it", status, live)
		}
	}
	if _, err := h.write(h.tenantContext(), row(entities.DeviationPendingApproval, "dv1-asked-again")); err != nil {
		t.Fatalf("a visit three decided rows name could not be asked for again: %v", err)
	}

	// A row of no visit — a hand-over, an edit — holds nothing, however many there are.
	for range 2 {
		written, err := h.write(h.tenantContext(), row(entities.DeviationApplied, ""))
		if err != nil {
			t.Fatalf("write a row of no visit: %v", err)
		}
		if live := liveKeyOf(written.ID); live != "" {
			t.Errorf("a row of no visit has the live key %q", live)
		}
	}
}
