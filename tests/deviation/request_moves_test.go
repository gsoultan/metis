package deviation_test

import (
	"errors"
	"fmt"
	"slices"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/gsoultan/metis/internal/pkg/apierr"
	"github.com/gsoultan/metis/server/domains/entities"
	repocontracts "github.com/gsoultan/metis/server/repositories/contracts"
)

var everyRequestStatus = []entities.DeviationRequestStatus{
	entities.DeviationRequestPending, entities.DeviationRequestApproved, entities.DeviationRequestApplied,
	entities.DeviationRequestInterrupted, entities.DeviationRequestStale, entities.DeviationRequestRejected,
	entities.DeviationRequestExpired,
}

// A request's status moves only the way the product moves it: a waiting
// request is decided once; an approved one is reported on; an interrupted one
// can still be reported on by the run the sweep gave up on. Nothing else moves,
// and nothing ever becomes live again. A move outside that is the mistake of
// the code that asked for it — a plain error — and changes nothing.
func TestARequestsStatusMovesOnlyTheWayTheClosedSetAllows(t *testing.T) {
	h := newDeviationHarness(t)
	instanceID := h.startOneStep(t, entities.Node{Name: "Approve", Type: entities.UserTask, Assignee: "alice"})
	allowed := map[entities.DeviationRequestStatus][]entities.DeviationRequestStatus{
		entities.DeviationRequestPending: {
			entities.DeviationRequestApproved, entities.DeviationRequestApplied, entities.DeviationRequestRejected,
			entities.DeviationRequestExpired, entities.DeviationRequestStale,
		},
		entities.DeviationRequestApproved:    {entities.DeviationRequestApplied, entities.DeviationRequestInterrupted},
		entities.DeviationRequestInterrupted: {entities.DeviationRequestApplied, entities.DeviationRequestInterrupted},
	}
	moves := 0
	for _, from := range everyRequestStatus {
		for _, to := range everyRequestStatus {
			moves++
			request := h.requestAt(t, h.sampleRequest(instanceID, fmt.Sprintf("dv1-move-%d", moves)), from)
			moved, err := h.transition(h.tenantContext(), request.ID, from, decisionTo(to))
			if slices.Contains(allowed[from], to) {
				if err != nil || moved.Status != to {
					t.Errorf("%s → %s: %v (now %s); the product makes this move", from, to, err, moved.Status)
				}
				continue
			}
			if err == nil {
				t.Errorf("%s → %s was written; nothing in the product makes this move", from, to)
			} else if !isTheWritersMistake(err) {
				t.Errorf("%s → %s: refused as %v; a move the code should not ask for is a plain server error", from, to, err)
			}
			if stored := h.mustGetRequest(t, request.ID); stored.Status != from {
				t.Errorf("%s → %s was refused and the request is now %s", from, to, stored.Status)
			}
		}
	}

	waiting := h.mustCreateRequest(t, h.sampleRequest(instanceID, "dv1-move-mistakes"))
	mistakes := map[string]struct {
		from   entities.DeviationRequestStatus
		change repocontracts.DeviationRequestChange
	}{
		"to no status":                  {entities.DeviationRequestPending, repocontracts.DeviationRequestChange{DecidedAt: time.Now()}},
		"to a status outside the set":   {entities.DeviationRequestPending, decisionTo("done")},
		"from no status":                {"", decisionTo(entities.DeviationRequestRejected)},
		"from a status outside the set": {"waiting", decisionTo(entities.DeviationRequestRejected)},
		"approved at no recorded time": {entities.DeviationRequestPending, repocontracts.DeviationRequestChange{
			Status: entities.DeviationRequestApproved, DecidedBy: "budi", DecidedByID: budi,
		}},
		"approved by no account": {entities.DeviationRequestPending, repocontracts.DeviationRequestChange{
			Status: entities.DeviationRequestApproved, DecidedBy: "budi", DecidedAt: time.Now(),
		}},
		"approved by nobody named": {entities.DeviationRequestPending, repocontracts.DeviationRequestChange{
			Status: entities.DeviationRequestApproved, DecidedByID: budi, DecidedAt: time.Now(),
		}},
		"approved by a name of spaces": {entities.DeviationRequestPending, repocontracts.DeviationRequestChange{
			Status: entities.DeviationRequestApproved, DecidedBy: "  ", DecidedByID: budi, DecidedAt: time.Now(),
		}},
		"applied by no account": {entities.DeviationRequestPending, repocontracts.DeviationRequestChange{
			Status: entities.DeviationRequestApplied, DecidedBy: "budi", DecidedAt: time.Now(),
		}},
		"applied at no recorded time": {entities.DeviationRequestPending, repocontracts.DeviationRequestChange{
			Status: entities.DeviationRequestApplied, DecidedBy: "budi", DecidedByID: budi,
		}},
		"rejected by no account": {entities.DeviationRequestPending, repocontracts.DeviationRequestChange{
			Status: entities.DeviationRequestRejected, DecidedBy: "budi", DecidedAt: time.Now(),
		}},
		"rejected at no recorded time": {entities.DeviationRequestPending, repocontracts.DeviationRequestChange{
			Status: entities.DeviationRequestRejected, DecidedBy: "budi", DecidedByID: budi,
		}},
		"rejected by nobody at all": {entities.DeviationRequestPending, repocontracts.DeviationRequestChange{
			Status: entities.DeviationRequestRejected,
		}},
	}
	for name, mistake := range mistakes {
		if _, err := h.transition(h.tenantContext(), waiting.ID, mistake.from, mistake.change); !isTheWritersMistake(err) {
			t.Errorf("%s: %v; want a plain server error", name, err)
		}
	}
	if stored := h.mustGetRequest(t, waiting.ID); stored.Status != entities.DeviationRequestPending || stored.DecidedAt != nil ||
		stored.DecidedBy != "" || stored.DecidedByID != uuid.Nil {
		t.Errorf("the refused moves left the request %s, decided at %v by %q", stored.Status, stored.DecidedAt, stored.DecidedBy)
	}
	if key := h.storedRequest(t, waiting.ID).liveKey; key.String != waiting.Fingerprint {
		t.Errorf("the refused moves left the request holding its fingerprint with %+v", key)
	}

	// The clock and a plan that no longer holds decide without anybody:
	// an expiry and a stale request name nobody, and are written.
	for _, status := range []entities.DeviationRequestStatus{entities.DeviationRequestExpired, entities.DeviationRequestStale} {
		nobody := h.mustCreateRequest(t, h.sampleRequest(instanceID, "dv1-nobody-"+string(status)))
		moved, err := h.transition(h.tenantContext(), nobody.ID, entities.DeviationRequestPending, repocontracts.DeviationRequestChange{Status: status})
		if err != nil || moved.Status != status || moved.DecidedBy != "" || moved.DecidedByID != uuid.Nil || moved.DecidedAt != nil {
			t.Errorf("%s with nobody named: %v, %+v", status, err, moved)
		}
	}

	// An approval the table holds with no time — nothing in the product
	// writes one, so it is made here by SQL — is closed by the sweep, which
	// names nobody, and is not reported as applied without a time.
	undated := h.requestAt(t, h.sampleRequest(instanceID, "dv1-undated"), entities.DeviationRequestApproved)
	if err := h.db.Exec(`UPDATE deviation_requests SET decided_at = NULL WHERE id = ?`, undated.ID).Error; err != nil {
		t.Fatalf("take the time of the approval away: %v", err)
	}
	report := repocontracts.DeviationRequestChange{Status: entities.DeviationRequestApplied, Outcome: map[string]any{"changed": float64(1)}}
	if _, err := h.transition(h.tenantContext(), undated.ID, entities.DeviationRequestApproved, report); !isTheWritersMistake(err) {
		t.Errorf("an approval with no time reported as applied: %v; want a plain server error", err)
	}
	swept, err := h.transition(h.tenantContext(), undated.ID, entities.DeviationRequestApproved,
		repocontracts.DeviationRequestChange{Status: entities.DeviationRequestInterrupted})
	if err != nil || swept.Status != entities.DeviationRequestInterrupted || swept.DecidedBy != "budi" {
		t.Errorf("the sweep closing an approval with no time: %v, %s by %q", err, swept.Status, swept.DecidedBy)
	}

	// What a caller is told, and acts on.
	if _, err := h.repo.DeviationRequest().Transition(h.tenantContext(), waiting.ID, entities.DeviationRequestPending,
		decisionTo(entities.DeviationRequestRejected)); !errors.Is(err, repocontracts.ErrDeviationRequestOutsideTransaction) {
		t.Errorf("a decision outside a transaction: %v, want ErrDeviationRequestOutsideTransaction", err)
	}
	if _, err := h.transition(h.tenantContext(), waiting.ID, entities.DeviationRequestApproved,
		decisionTo(entities.DeviationRequestApplied)); !errors.Is(err, repocontracts.ErrDeviationRequestDecided) {
		t.Errorf("a move from a status the request is not at: %v, want ErrDeviationRequestDecided", err)
	}
	if _, err := h.transition(h.tenantContext(), uuid.Must(uuid.NewV7()), entities.DeviationRequestPending,
		decisionTo(entities.DeviationRequestRejected)); !errors.Is(err, apierr.ErrNotFound) {
		t.Errorf("a decision of a request that does not exist: %v, want not found", err)
	}
	if stored := h.mustGetRequest(t, waiting.ID); stored.Status != entities.DeviationRequestPending {
		t.Errorf("after the refusals the request is %s", stored.Status)
	}
}

// One live request per fingerprint per project. A request holds its
// fingerprint while it waits and while it is approved and running, and lets
// go the moment it is over — whichever way it ended — so the same thing can be
// asked for again and never twice at once.
func TestALiveRequestHoldsItsFingerprintAndOneThatIsOverLetsGo(t *testing.T) {
	h := newDeviationHarness(t)
	instanceID := h.startOneStep(t, entities.Node{Name: "Approve", Type: entities.UserTask, Assignee: "alice"})
	requests := h.repo.DeviationRequest()
	held := func(what string, request entities.DeviationRequest) {
		t.Helper()
		if key := h.storedRequest(t, request.ID).liveKey; !key.Valid || key.String != request.Fingerprint {
			t.Errorf("%s: the live key is %+v; want the fingerprint %q", what, key, request.Fingerprint)
		}
		if _, err := h.createRequest(h.tenantContext(), h.sampleRequest(instanceID, request.Fingerprint)); !errors.Is(err, repocontracts.ErrDeviationRequestAlreadyWaiting) {
			t.Errorf("%s: a second request for the fingerprint: %v, want ErrDeviationRequestAlreadyWaiting", what, err)
		}
		found, ok, err := requests.FindLive(h.tenantContext(), h.projID, request.Fingerprint)
		if err != nil || !ok || found.ID != request.ID {
			t.Errorf("%s: the live request of the fingerprint is %s (%v, %v); want %s", what, found.ID, ok, err, request.ID)
		}
	}
	letGo := func(what string, request entities.DeviationRequest) {
		t.Helper()
		if key := h.storedRequest(t, request.ID).liveKey; key.Valid {
			t.Errorf("%s: the live key is still %q; the request would refuse every later one", what, key.String)
		}
		if _, ok, err := requests.FindLive(h.tenantContext(), h.projID, request.Fingerprint); err != nil || ok {
			t.Errorf("%s: a request that is over is still found as live (%v, %v)", what, ok, err)
		}
	}

	for _, status := range everyRequestStatus {
		fingerprint := "dv1-held-" + string(status)
		request := h.mustCreateRequest(t, h.sampleRequest(instanceID, fingerprint))
		held("waiting, on its way to "+string(status), request)
		for _, next := range movesTo[status] {
			moved, err := h.transition(h.tenantContext(), request.ID, request.Status, decisionTo(next))
			if err != nil {
				t.Fatalf("move %s to %s: %v", fingerprint, next, err)
			}
			request = moved
			if next.Live() {
				held("once "+string(next), request)
			}
		}
		if status.Live() {
			continue
		}
		letGo("once "+string(status), request)
		again, err := h.createRequest(h.tenantContext(), h.sampleRequest(instanceID, fingerprint))
		if err != nil {
			t.Errorf("asking again after a request ended %s was refused: %v", status, err)
			continue
		}
		held("asked again after "+string(status), again)
	}

	// An interrupted request the run reports on after all stays over.
	swept := h.requestAt(t, h.sampleRequest(instanceID, "dv1-reported-late"), entities.DeviationRequestInterrupted)
	for _, report := range []entities.DeviationRequestStatus{entities.DeviationRequestInterrupted, entities.DeviationRequestApplied} {
		moved, err := h.transition(h.tenantContext(), swept.ID, swept.Status, decisionTo(report))
		if err != nil {
			t.Fatalf("the run's report (%s) over the sweep's mark: %v", report, err)
		}
		swept = moved
		letGo("reported as "+string(report)+" after the sweep", swept)
	}

	// Live is what the table says, not what the clock says: a request past
	// its deadline that no sweep has closed is still found, with the status
	// it is stored at. Whoever asks reads it through EffectiveStatus.
	overdue := h.sampleRequest(instanceID, "dv1-overdue-unswept")
	overdue.ExpiresAt = time.Now().Add(-time.Minute)
	unswept := h.mustCreateRequest(t, overdue)
	found, ok, err := requests.FindLive(h.tenantContext(), h.projID, "dv1-overdue-unswept")
	if err != nil || !ok || found.ID != unswept.ID || found.Status != entities.DeviationRequestPending {
		t.Errorf("a request past its deadline and not yet swept: found %v as %s (%v); want it found, as stored", ok, found.Status, err)
	}
	if reads := found.EffectiveStatus(time.Now()); reads != entities.DeviationRequestExpired {
		t.Errorf("the request found past its deadline reads as %s; want expired", reads)
	}
	held("past its deadline and not yet swept", unswept)

	// The same fingerprint in another project is another request.
	second, err := h.svc.CreateProject(h.tenantContext(), h.orgID, "Another Project", "")
	if err != nil {
		t.Fatalf("create the second project: %v", err)
	}
	if _, ok, err := requests.FindLive(h.tenantContext(), second.ID, "dv1-held-approved"); err != nil || ok {
		t.Errorf("a project found another project's live request (%v, %v)", ok, err)
	}
}

// A change writes what it names and leaves the rest as stored.
func TestAChangeLeavesWhatItDoesNotNameAsStored(t *testing.T) {
	h := newDeviationHarness(t)
	instanceID := h.startOneStep(t, entities.Node{Name: "Approve", Type: entities.UserTask, Assignee: "alice"})
	request := h.mustCreateRequest(t, h.sampleRequest(instanceID, "dv1-partial"))
	approvedAt := time.Date(2026, 10, 5, 9, 30, 0, 0, time.UTC)
	approved, err := h.transition(h.tenantContext(), request.ID, entities.DeviationRequestPending, repocontracts.DeviationRequestChange{
		Status: entities.DeviationRequestApproved, DecidedBy: "budi", DecidedByID: budi,
		DecisionReason: "agreed with the owner", DecidedAt: approvedAt, Outcome: map[string]any{"note": "running"},
	})
	if err != nil {
		t.Fatalf("approve: %v", err)
	}

	// The run reports: a status and nothing else.
	reported, err := h.transition(h.tenantContext(), request.ID, entities.DeviationRequestApproved,
		repocontracts.DeviationRequestChange{Status: entities.DeviationRequestInterrupted})
	if err != nil {
		t.Fatalf("report: %v", err)
	}
	for what, got := range map[string]entities.DeviationRequest{"the move's answer": reported, "a read": h.mustGetRequest(t, request.ID)} {
		if got.Status != entities.DeviationRequestInterrupted || got.DecidedBy != "budi" || got.DecidedByID != budi ||
			got.DecisionReason != "agreed with the owner" || got.DecidedAt == nil || !got.DecidedAt.Equal(approvedAt) ||
			got.Outcome["note"] != "running" {
			t.Errorf("%s: a change that named only a status left %+v", what, got)
		}
		if got.Fingerprint != approved.Fingerprint || got.RequestedBy != "ana" || got.Reason != "the approver has left" ||
			got.Command["node_id"] != "step" || !got.ExpiresAt.Equal(approved.ExpiresAt) {
			t.Errorf("%s: a decision changed what was asked: %+v", what, got)
		}
	}

	// The run's own report replaces the outcome whole.
	late, err := h.transition(h.tenantContext(), request.ID, entities.DeviationRequestInterrupted, repocontracts.DeviationRequestChange{
		Status: entities.DeviationRequestApplied, Outcome: map[string]any{"changed": float64(3), "reported_after_sweep": true},
	})
	if err != nil {
		t.Fatalf("the late report: %v", err)
	}
	if _, kept := late.Outcome["note"]; kept || late.Outcome["changed"] != float64(3) || late.Outcome["reported_after_sweep"] != true {
		t.Errorf("the outcome after the run's report is %v; want the report and nothing of what it replaced", late.Outcome)
	}
	if late.DecidedBy != "budi" || late.DecidedAt == nil || !late.DecidedAt.Equal(approvedAt) {
		t.Errorf("the report changed who approved and when: %q at %v", late.DecidedBy, late.DecidedAt)
	}
}
