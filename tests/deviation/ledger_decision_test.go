package deviation_test

import (
	"context"
	"database/sql"
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/gsoultan/metis/internal/pkg/apierr"
	"github.com/gsoultan/metis/server/domains/entities"
	repocontracts "github.com/gsoultan/metis/server/repositories/contracts"
)

// decideRow decides a ledger row inside a unit of work of its own.
func (h *deviationHarness) decideRow(ctx context.Context, id uuid.UUID, decision repocontracts.LedgerRowDecision) (entities.Deviation, error) {
	var out entities.Deviation
	err := h.repo.UnitOfWork().Do(ctx, func(tx context.Context) error {
		var err error
		out, err = h.repo.DeviationDecider().Decide(tx, id, decision)
		return err
	})
	return out, err
}

// pendingWaive is a waive of the step that waits for approval, for one visit.
func (h *deviationHarness) pendingWaive(instanceID uuid.UUID, visit string) entities.Deviation {
	row := h.sample(instanceID)
	row.Kind, row.Scope, row.Origin = entities.DeviationWaive, entities.DeviationScopeInstance, entities.DeviationOriginInPlace
	row.Status, row.VisitKey = entities.DeviationPendingApproval, visit
	return row
}

func (h *deviationHarness) mustWrite(t *testing.T, d entities.Deviation) entities.Deviation {
	t.Helper()
	written, err := h.write(h.tenantContext(), d)
	if err != nil {
		t.Fatalf("write the ledger row: %v", err)
	}
	return written
}

// liveVisitKeyOf is what a ledger row holds its visit with, if anything.
func (h *deviationHarness) liveVisitKeyOf(t *testing.T, id uuid.UUID) sql.NullString {
	t.Helper()
	var live sql.NullString
	if err := h.db.Raw(`SELECT live_visit_key FROM instance_deviations WHERE id = ?`, id).Row().Scan(&live); err != nil {
		t.Fatalf("read the live key: %v", err)
	}
	return live
}

// theRow reads one ledger row back through the ledger's own read.
func (h *deviationHarness) theRow(t *testing.T, instanceID, id uuid.UUID) entities.Deviation {
	t.Helper()
	rows, err := h.repo.Deviation().ListByInstance(h.tenantContext(), instanceID)
	if err != nil {
		t.Fatalf("read the ledger: %v", err)
	}
	for _, row := range rows {
		if row.ID == id {
			return row
		}
	}
	t.Fatalf("the ledger has no row %s", id)
	return entities.Deviation{}
}

// A ledger row moves once, from pending to a terminal status; leaving the
// live statuses frees its visit.
func TestALedgerRowIsDecidedOnceAndADecidedRowFreesItsVisit(t *testing.T) {
	h := newDeviationHarness(t)
	instanceID := h.startOneStep(t, entities.Node{Name: "Approve", Type: entities.UserTask, Assignee: "alice"})
	row := h.sample(instanceID)
	row.Kind, row.Origin, row.Status, row.VisitKey = entities.DeviationWaive, entities.DeviationOriginInPlace, entities.DeviationPendingApproval, "dv1-v"
	written, err := h.write(h.tenantContext(), row)
	if err != nil {
		t.Fatalf("write: %v", err)
	}
	decide := func(status entities.DeviationStatus) error {
		return h.repo.UnitOfWork().Do(h.tenantContext(), func(tx context.Context) error {
			_, err := h.repo.DeviationDecider().Decide(tx, written.ID, repocontracts.LedgerRowDecision{Status: status, DecidedAt: time.Now()})
			return err
		})
	}
	if err := decide(entities.DeviationRejected); err != nil {
		t.Fatalf("decide: %v", err)
	}
	if err := decide(entities.DeviationApplied); !errors.Is(err, repocontracts.ErrDeviationRowDecided) {
		t.Fatalf("a second decision: %v", err)
	}
	if _, ok, _ := h.repo.Deviation().FindLiveByVisit(h.tenantContext(), instanceID, "dv1-v"); ok {
		t.Fatal("a rejected row is still the visit's live row")
	}
	row.ID = uuid.Must(uuid.NewV7())
	if _, err := h.write(h.tenantContext(), row); err != nil {
		t.Fatalf("a new pending row for a freed visit: %v", err)
	}
}

// A row holds its visit for as long as it is live — waiting, or applied — and
// for no longer. Decided to applied it goes on holding it, so the step cannot
// be waived twice; decided against, it lets go, and the same thing can be
// asked for again. The live statuses are DeviationStatus.Live: applied and
// pending_approval.
func TestADecidedRowHoldsItsVisitOnlyWhileItIsLive(t *testing.T) {
	h := newDeviationHarness(t)
	instanceID := h.startOneStep(t, entities.Node{Name: "Approve", Type: entities.UserTask, Assignee: "alice"})
	for _, status := range []entities.DeviationStatus{
		entities.DeviationApplied, entities.DeviationRejected, entities.DeviationExpired, entities.DeviationStale,
	} {
		visit := "dv1-decided-" + string(status)
		pending := h.mustWrite(t, h.pendingWaive(instanceID, visit))
		if live := h.liveVisitKeyOf(t, pending.ID); live.String != visit {
			t.Fatalf("a waiting row holds its visit with %+v; want %q", live, visit)
		}
		decided, err := h.decideRow(h.tenantContext(), pending.ID, repocontracts.LedgerRowDecision{Status: status, DecidedAt: time.Now()})
		if err != nil {
			t.Fatalf("decide to %s: %v", status, err)
		}
		if decided.Status != status || decided.VisitKey != visit {
			t.Errorf("decided to %s the row answers status %s, visit %q", status, decided.Status, decided.VisitKey)
		}
		live := h.liveVisitKeyOf(t, pending.ID)
		_, found, err := h.repo.Deviation().FindLiveByVisit(h.tenantContext(), instanceID, visit)
		if err != nil {
			t.Fatalf("find the live row of %s: %v", visit, err)
		}
		_, again := h.write(h.tenantContext(), h.pendingWaive(instanceID, visit))
		if status.Live() {
			if live.String != visit || !found || again == nil {
				t.Errorf("a row decided to %s holds its visit with %+v, is found as live: %v, and a second row for the visit: %v; "+
					"want the visit held, the row found and the second refused", status, live, found, again)
			}
			continue
		}
		if live.Valid || found || again != nil {
			t.Errorf("a row decided to %s holds its visit with %+v, is found as live: %v, and asking again: %v; "+
				"want the visit let go and the new request written", status, live, found, again)
		}
	}
}

// A decision writes what it names — who, when, the entry beside it, the task
// that was withdrawn, and the values at the moment of the change, sealed —
// and leaves what it does not name as the request wrote it.
func TestADecisionWritesWhatItNamesAndLeavesTheRest(t *testing.T) {
	h := newDeviationHarness(t)
	instanceID := h.startOneStep(t, entities.Node{Name: "Approve", Type: entities.UserTask, Assignee: "alice"})
	request := h.mustCreateRequest(t, h.sampleRequest(instanceID, "dv1-named"))
	pending := h.pendingWaive(instanceID, "dv1-named")
	pending.RequestID, pending.ActorID = request.ID, request.RequestedByID
	pending.Before = map[string]any{}
	pending.After = map[string]any{"variables": map[string]any{"amount": "eighty-thousand"}}
	pending.Details = map[string]any{"open_work": float64(1), "decision_points": float64(0)}
	written := h.mustWrite(t, pending)

	decidedAt := time.Date(2026, 10, 5, 9, 30, 0, 0, time.UTC)
	decision := repocontracts.LedgerRowDecision{
		Status: entities.DeviationApplied, ApprovedBy: "budi", ApprovedByID: budi, DecidedAt: decidedAt,
		AuditEntryID: uuid.Must(uuid.NewV7()), Task: &entities.Task{ID: uuid.Must(uuid.NewV7())},
		Before:  map[string]any{"variables": map[string]any{"amount": "seventy-thousand-four-hundred"}},
		After:   map[string]any{"variables": map[string]any{"amount": "eighty-thousand"}, "tasks": map[string]any{"t1": "withdrawn"}},
		Details: map[string]any{"withdrawn": float64(1), "tasks_listed": float64(1)},
	}
	decided, err := h.decideRow(h.tenantContext(), written.ID, decision)
	if err != nil {
		t.Fatalf("decide: %v", err)
	}
	for what, got := range map[string]entities.Deviation{"the decision's answer": decided, "the ledger": h.theRow(t, instanceID, written.ID)} {
		if got.Status != entities.DeviationApplied || got.ApprovedBy != "budi" || got.ApprovedByID != budi ||
			got.DecidedAt == nil || !got.DecidedAt.Equal(decidedAt) || got.AuditEntryID != decision.AuditEntryID ||
			got.Task == nil || got.Task.ID != decision.Task.ID {
			t.Errorf("%s: the decision reads back as %+v", what, got)
		}
		if got.Before["variables"].(map[string]any)["amount"] != "seventy-thousand-four-hundred" ||
			got.After["tasks"].(map[string]any)["t1"] != "withdrawn" || got.Details["withdrawn"] != float64(1) ||
			len(got.Details) != 2 {
			t.Errorf("%s: before=%v after=%v details=%v; want what the decision gave, whole", what, got.Before, got.After, got.Details)
		}
		if got.RequestID != request.ID || got.Actor != written.Actor || got.ActorID != request.RequestedByID || got.Reason != written.Reason ||
			got.RunID != written.RunID || got.Kind != entities.DeviationWaive || got.VisitKey != "dv1-named" ||
			got.Node == nil || got.Node.ID != "step" || !got.CreatedAt.Equal(written.CreatedAt) {
			t.Errorf("%s: the decision changed what was asked: %+v", what, got)
		}
	}
	var before, after, details string
	if err := h.db.Raw(`SELECT before::text, after::text, details::text FROM instance_deviations WHERE id = ?`, written.ID).
		Row().Scan(&before, &after, &details); err != nil {
		t.Fatalf("read the stored row: %v", err)
	}
	if strings.Contains(before, "thousand") || strings.Contains(after, "thousand") {
		t.Errorf("a decision stored business values in clear: before=%s after=%s", before, after)
	}
	if !strings.Contains(details, "tasks_listed") {
		t.Errorf("details is not business data and is meant to stay readable in SQL: %s", details)
	}

	// A decision that names nothing but the status and the time: the rest
	// stays as the request wrote it, and nobody is named as having approved.
	bare := h.pendingWaive(instanceID, "dv1-bare")
	bare.After = map[string]any{"variables": map[string]any{"amount": "ninety"}}
	bare.Details = map[string]any{"open_work": float64(1)}
	bareRow := h.mustWrite(t, bare)
	expired, err := h.decideRow(h.tenantContext(), bareRow.ID, repocontracts.LedgerRowDecision{Status: entities.DeviationExpired, DecidedAt: decidedAt})
	if err != nil {
		t.Fatalf("expire: %v", err)
	}
	if expired.Status != entities.DeviationExpired || expired.ApprovedBy != "" || expired.ApprovedByID != uuid.Nil ||
		expired.Task != nil || expired.AuditEntryID != uuid.Nil ||
		expired.After["variables"].(map[string]any)["amount"] != "ninety" || expired.Details["open_work"] != float64(1) ||
		!reflect.DeepEqual(expired.Before, bareRow.Before) || len(expired.Before) == 0 ||
		expired.DecidedAt == nil || !expired.DecidedAt.Equal(decidedAt) {
		t.Errorf("a decision of nothing but a status left %+v", expired)
	}
}

// A decision the code got wrong is that code's mistake — a plain error — and
// one from another organization finds no row. Either way the row still waits.
func TestADecisionThatIsTheWritersMistakeOrAnotherOrganizationsChangesNothing(t *testing.T) {
	h := newDeviationHarness(t)
	instanceID := h.startOneStep(t, entities.Node{Name: "Approve", Type: entities.UserTask, Assignee: "alice"})
	written := h.mustWrite(t, h.pendingWaive(instanceID, "dv1-waiting"))
	now := time.Now()

	mistakes := map[string]repocontracts.LedgerRowDecision{
		"back to waiting":             {Status: entities.DeviationPendingApproval, DecidedAt: now},
		"to no status":                {DecidedAt: now},
		"to a status outside the set": {Status: "waived", DecidedAt: now},
		"at no time":                  {Status: entities.DeviationRejected},
	}
	for name, decision := range mistakes {
		if _, err := h.decideRow(h.tenantContext(), written.ID, decision); !isTheWritersMistake(err) {
			t.Errorf("%s: %v; want a plain server error", name, err)
		}
	}
	rejected := repocontracts.LedgerRowDecision{Status: entities.DeviationRejected, DecidedAt: now}
	if _, err := h.repo.DeviationDecider().Decide(h.tenantContext(), written.ID, rejected); !errors.Is(err, repocontracts.ErrDeviationOutsideTransaction) {
		t.Errorf("a decision outside a transaction: %v, want ErrDeviationOutsideTransaction", err)
	}
	if _, err := h.decideRow(h.tenantContext(), uuid.Must(uuid.NewV7()), rejected); !errors.Is(err, apierr.ErrNotFound) {
		t.Errorf("a decision of a row that does not exist: %v, want not found", err)
	}
	other := h.inAnotherOrganization(t, "Decision Other Org")
	if _, err := h.decideRow(other.tenantContext(), written.ID, rejected); !errors.Is(err, apierr.ErrNotFound) {
		t.Errorf("another organization deciding the row: %v, want not found", err)
	}

	still := h.theRow(t, instanceID, written.ID)
	if still.Status != entities.DeviationPendingApproval || still.DecidedAt != nil || still.ApprovedBy != "" {
		t.Errorf("after the refusals the row is %s, decided at %v by %q", still.Status, still.DecidedAt, still.ApprovedBy)
	}
	if live := h.liveVisitKeyOf(t, written.ID); live.String != "dv1-waiting" {
		t.Errorf("after the refusals the row holds its visit with %+v", live)
	}

	// A row that never waited — applied on one administrator's call — is not
	// decided at all.
	applied := h.sample(instanceID)
	applied.VisitKey = "dv1-never-waited"
	appliedRow := h.mustWrite(t, applied)
	if _, err := h.decideRow(h.tenantContext(), appliedRow.ID, rejected); !errors.Is(err, repocontracts.ErrDeviationRowDecided) {
		t.Errorf("a decision of a row that never waited: %v, want ErrDeviationRowDecided", err)
	}
	if live := h.liveVisitKeyOf(t, appliedRow.ID); live.String != "dv1-never-waited" {
		t.Errorf("a refused decision of an applied row left it holding its visit with %+v", live)
	}
}
