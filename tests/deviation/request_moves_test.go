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

var everyRequestKind = []entities.DeviationRequestKind{entities.DeviationRequestInstanceWaive, entities.DeviationRequestMigration}

// requestOfKind is a well-formed request of kind: a waive of the step
// instanceID waits at, or a migration between two versions of the project.
func (h *deviationHarness) requestOfKind(t *testing.T, kind entities.DeviationRequestKind, instanceID uuid.UUID, fingerprint string) entities.DeviationRequest {
	t.Helper()
	if kind == entities.DeviationRequestMigration {
		return h.sampleMigration(t, fingerprint)
	}
	return h.sampleRequest(instanceID, fingerprint)
}

// A request's status moves only the way the product moves a request of its
// kind. A waive that waits is applied — approved and done in one change — or
// rejected, expired or made stale, and then it is over: it is never approved
// without being applied. A migration that waits is approved, rejected, expired
// or made stale; approved, its run reports it applied or interrupted; and an
// interrupted one can still be reported on by the run the sweep gave up on.
// It is never applied without having been approved. Nothing else moves, and
// nothing ever becomes live again. A move outside that is the mistake of the
// code that asked for it — a plain error — and changes nothing.
func TestARequestsStatusMovesOnlyTheWayTheClosedSetAllows(t *testing.T) {
	h := newDeviationHarness(t)
	instanceID := h.startOneStep(t, entities.Node{Name: "Approve", Type: entities.UserTask, Assignee: "alice"})
	type moves = map[entities.DeviationRequestStatus][]entities.DeviationRequestStatus
	allowed := map[entities.DeviationRequestKind]moves{
		entities.DeviationRequestInstanceWaive: {
			entities.DeviationRequestPending: {
				entities.DeviationRequestApplied, entities.DeviationRequestRejected,
				entities.DeviationRequestExpired, entities.DeviationRequestStale,
			},
		},
		entities.DeviationRequestMigration: {
			entities.DeviationRequestPending: {
				entities.DeviationRequestApproved, entities.DeviationRequestRejected,
				entities.DeviationRequestExpired, entities.DeviationRequestStale,
			},
			entities.DeviationRequestApproved:    {entities.DeviationRequestApplied, entities.DeviationRequestInterrupted},
			entities.DeviationRequestInterrupted: {entities.DeviationRequestApplied, entities.DeviationRequestInterrupted},
		},
	}
	tried := 0
	for _, kind := range everyRequestKind {
		for _, from := range everyRequestStatus {
			if _, reachable := movesTo[kind][from]; !reachable {
				// A waive is never approved or interrupted: the moves that
				// would make it so are among those refused below.
				continue
			}
			for _, to := range everyRequestStatus {
				tried++
				request := h.requestAt(t, h.requestOfKind(t, kind, instanceID, fmt.Sprintf("dv1-move-%d", tried)), from)
				moved, err := h.transition(h.tenantContext(), request.ID, from, moveTo(from, to))
				if slices.Contains(allowed[kind][from], to) {
					if err != nil || moved.Status != to {
						t.Errorf("%s, %s → %s: %v (now %s); the product makes this move", kind, from, to, err, moved.Status)
					}
					continue
				}
				if err == nil {
					t.Errorf("%s, %s → %s was written; nothing in the product makes this move", kind, from, to)
				} else if !isTheWritersMistake(err) {
					t.Errorf("%s, %s → %s: refused as %v; a move the code should not ask for is a plain server error", kind, from, to, err)
				}
				stored := h.mustGetRequest(t, request.ID)
				if stored.Status != from || !stored.UpdatedAt.Equal(request.UpdatedAt) {
					t.Errorf("%s, %s → %s was refused and the request is now %s, last written %v", kind, from, to, stored.Status, stored.UpdatedAt)
				}
			}
		}
	}
	if tried != 5*7+7*7 {
		t.Fatalf("%d moves were tried; a waive has five statuses it can be at and a migration seven, each with seven to go to", tried)
	}

	// The mistakes are tried on a migration that waits: it is the kind every
	// one of these statuses is open to but applied, which is tried on a waive.
	waiting := h.mustCreateRequest(t, h.sampleMigration(t, "mf1-move-mistakes"))
	waitingWaive := h.mustCreateRequest(t, h.sampleRequest(instanceID, "dv1-move-mistakes"))
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
		target := waiting
		if mistake.change.Status == entities.DeviationRequestApplied {
			target = waitingWaive
		}
		if _, err := h.transition(h.tenantContext(), target.ID, mistake.from, mistake.change); !isTheWritersMistake(err) {
			t.Errorf("%s: %v; want a plain server error", name, err)
		}
	}
	for _, target := range []entities.DeviationRequest{waiting, waitingWaive} {
		if stored := h.mustGetRequest(t, target.ID); stored.Status != entities.DeviationRequestPending || stored.DecidedAt != nil ||
			stored.DecidedBy != "" || stored.DecidedByID != uuid.Nil {
			t.Errorf("the refused moves left the %s request %s, decided at %v by %q", target.Kind, stored.Status, stored.DecidedAt, stored.DecidedBy)
		}
		if key := h.storedRequest(t, target.ID).liveKey; key.String != target.Fingerprint {
			t.Errorf("the refused moves left the %s request holding its fingerprint with %+v", target.Kind, key)
		}
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
	undated := h.requestAt(t, h.sampleMigration(t, "mf1-undated"), entities.DeviationRequestApproved)
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
		moveTo(entities.DeviationRequestApproved, entities.DeviationRequestApplied)); !errors.Is(err, repocontracts.ErrDeviationRequestDecided) {
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

	for _, kind := range everyRequestKind {
		for _, status := range everyRequestStatus {
			path, reachable := movesTo[kind][status]
			if !reachable {
				continue
			}
			fingerprint := fmt.Sprintf("dv1-held-%s-%s", kind, status)
			what := string(kind) + " "
			request := h.mustCreateRequest(t, h.requestOfKind(t, kind, instanceID, fingerprint))
			held(what+"waiting, on its way to "+string(status), request)
			for _, next := range path {
				moved, err := h.transition(h.tenantContext(), request.ID, request.Status, moveTo(request.Status, next))
				if err != nil {
					t.Fatalf("move %s to %s: %v", fingerprint, next, err)
				}
				request = moved
				if next.Live() {
					held(what+"once "+string(next), request)
				}
			}
			if status.Live() {
				continue
			}
			letGo(what+"once "+string(status), request)
			again, err := h.createRequest(h.tenantContext(), h.requestOfKind(t, kind, instanceID, fingerprint))
			if err != nil {
				t.Errorf("asking again after a %s request ended %s was refused: %v", kind, status, err)
				continue
			}
			held(what+"asked again after "+string(status), again)
		}
	}

	// An interrupted request the run reports on after all stays over.
	swept := h.requestAt(t, h.sampleMigration(t, "mf1-reported-late"), entities.DeviationRequestInterrupted)
	for _, report := range []entities.DeviationRequestStatus{entities.DeviationRequestInterrupted, entities.DeviationRequestApplied} {
		moved, err := h.transition(h.tenantContext(), swept.ID, swept.Status, moveTo(swept.Status, report))
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
	if _, ok, err := requests.FindLive(h.tenantContext(), second.ID, "dv1-held-migration-approved"); err != nil || ok {
		t.Errorf("a project found another project's live request (%v, %v)", ok, err)
	}
}

// A change writes what it names and leaves the rest as stored.
func TestAChangeLeavesWhatItDoesNotNameAsStored(t *testing.T) {
	h := newDeviationHarness(t)
	request := h.mustCreateRequest(t, h.sampleMigration(t, "mf1-partial"))
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
			got.Command["node_mapping"] == nil || !got.ExpiresAt.Equal(approved.ExpiresAt) {
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

// Who approved is written once. A run's report on an approved request — or on
// one the sweep gave up on — says what the run did, not who approved it: a
// report that names a decider, an account, a time or a reason is refused, and
// who approved and when stay as they were. Otherwise one stray field would
// turn a self-approval into one a second person gave, or the other way round.
func TestAReportNeverChangesWhoApproved(t *testing.T) {
	h := newDeviationHarness(t)
	carol := uuid.Must(uuid.NewV7())
	naming := map[string]func(*repocontracts.DeviationRequestChange){
		"another decider":        func(c *repocontracts.DeviationRequestChange) { c.DecidedBy = "carol" },
		"another account":        func(c *repocontracts.DeviationRequestChange) { c.DecidedByID = carol },
		"another time":           func(c *repocontracts.DeviationRequestChange) { c.DecidedAt = time.Now() },
		"another reason":         func(c *repocontracts.DeviationRequestChange) { c.DecisionReason = "the run went well" },
		"the same decider again": func(c *repocontracts.DeviationRequestChange) { c.DecidedBy, c.DecidedByID = "budi", budi },
	}
	reports := 0
	for _, from := range []entities.DeviationRequestStatus{entities.DeviationRequestApproved, entities.DeviationRequestInterrupted} {
		for _, to := range []entities.DeviationRequestStatus{entities.DeviationRequestApplied, entities.DeviationRequestInterrupted} {
			reports++
			request := h.requestAt(t, h.sampleMigration(t, fmt.Sprintf("mf1-report-%d", reports)), from)
			for name, name1 := range naming {
				report := repocontracts.DeviationRequestChange{Status: to, Outcome: map[string]any{"changed": float64(1)}}
				name1(&report)
				if _, err := h.transition(h.tenantContext(), request.ID, from, report); !isTheWritersMistake(err) {
					t.Errorf("%s → %s, a report naming %s: %v; want a plain server error", from, to, name, err)
				}
			}
			still := h.mustGetRequest(t, request.ID)
			if still.Status != from || still.DecidedBy != request.DecidedBy || still.DecidedByID != request.DecidedByID ||
				still.DecisionReason != request.DecisionReason || still.DecidedAt == nil || !still.DecidedAt.Equal(*request.DecidedAt) ||
				len(still.Outcome) != 0 || !still.UpdatedAt.Equal(request.UpdatedAt) {
				t.Errorf("%s → %s: the refused reports left the request %s by %q (%s) at %v with outcome %v; it was %s by %q (%s) at %v",
					from, to, still.Status, still.DecidedBy, still.DecisionReason, still.DecidedAt, still.Outcome,
					from, request.DecidedBy, request.DecisionReason, request.DecidedAt)
			}

			// The report that names nobody is written, and budi still approved.
			reported, err := h.transition(h.tenantContext(), request.ID, from,
				repocontracts.DeviationRequestChange{Status: to, Outcome: map[string]any{"changed": float64(1)}})
			if err != nil || reported.Status != to || reported.DecidedBy != "budi" || reported.DecidedByID != budi ||
				reported.DecidedAt == nil || !reported.DecidedAt.Equal(*request.DecidedAt) || reported.Outcome["changed"] != float64(1) {
				t.Errorf("%s → %s, a report naming nobody: %v, %+v", from, to, err, reported)
			}
		}
	}
}
