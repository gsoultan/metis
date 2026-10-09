package deviation_test

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/gsoultan/metis/server/domains/entities"
	repocontracts "github.com/gsoultan/metis/server/repositories/contracts"
	stormruntime "github.com/gsoultan/storm/runtime"
)

// What two people do at the same instant.
//
// A request is decided once and a visit is held once whoever else is trying:
// the row's lock orders two decisions, and the unique indexes on the live keys
// order two writes. The first group of tests sends several at once, under the
// race detector, and asserts what holds under every interleaving — one wins,
// every other is told so in a way its caller can recognise. The second makes
// the one interleaving that matters happen every time: a decision that waits
// behind another, and reads what the other left.

const (
	// raceWait bounds everything a race waits for. A race that deadlocks
	// would otherwise hold the package until its own timeout, and say nothing
	// of which test it was.
	raceWait = 20 * time.Second
	// atOnce is how many try the same thing together.
	atOnce = 8
	// raceRounds is how many times each race is run; each is a subtest.
	raceRounds = 6
)

// together runs attempt atOnce times at the same moment and answers each
// attempt's error, in order.
func together(t *testing.T, what string, attempt func(i int) error) []error {
	t.Helper()
	errs := make([]error, atOnce)
	var start, racing sync.WaitGroup
	start.Add(1)
	for i := range atOnce {
		racing.Go(func() {
			start.Wait()
			errs[i] = attempt(i)
		})
	}
	start.Done()
	finished := make(chan struct{})
	go func() {
		racing.Wait()
		close(finished)
	}()
	select {
	case <-finished:
	case <-time.After(raceWait):
		t.Fatalf("%s did not finish within %s", what, raceWait)
	}
	return errs
}

// oneWon checks that exactly one attempt succeeded and every other was told
// it lost in the way told recognises, and answers the winner.
func oneWon(t *testing.T, what string, errs []error, told func(error) bool) int {
	t.Helper()
	winner := -1
	for i, err := range errs {
		switch {
		case err == nil && winner < 0:
			winner = i
		case err == nil:
			t.Errorf("%s: attempts %d and %d both succeeded", what, winner, i)
		case !told(err):
			t.Errorf("%s: attempt %d was answered %v; want the answer that says somebody else came first", what, i, err)
		}
	}
	if winner < 0 {
		t.Fatalf("%s: nobody succeeded: %v", what, errs)
	}
	return winner
}

// Several administrators decide one request at the same moment, some to
// approve it and some to reject it. One decision is written — whole, never
// one's status with another's name — and every other is told the request was
// decided first.
func TestDecisionsOfOneRequestMadeTogetherEndInOne(t *testing.T) {
	h := newDeviationHarness(t)
	for round := range raceRounds {
		t.Run(fmt.Sprintf("round %d", round), func(t *testing.T) {
			request := h.mustCreateRequest(t, h.sampleMigration(t, fmt.Sprintf("mf1-race-decide-%d", round)))
			deciders := make([]repocontracts.DeviationRequestChange, atOnce)
			for i := range deciders {
				status := entities.DeviationRequestApproved
				if i%2 == 1 {
					status = entities.DeviationRequestRejected
				}
				deciders[i] = repocontracts.DeviationRequestChange{
					Status: status, DecidedBy: fmt.Sprintf("admin-%d", i), DecidedByID: uuid.Must(uuid.NewV7()),
					DecisionReason: fmt.Sprintf("decision %d", i), DecidedAt: time.Now(),
				}
			}
			errs := together(t, "the decisions", func(i int) error {
				_, err := h.transition(h.tenantContext(), request.ID, entities.DeviationRequestPending, deciders[i])
				return err
			})
			winner := oneWon(t, "deciding one request", errs, func(err error) bool {
				return errors.Is(err, repocontracts.ErrDeviationRequestDecided)
			})
			got, want := h.mustGetRequest(t, request.ID), deciders[winner]
			if got.Status != want.Status || got.DecidedBy != want.DecidedBy || got.DecidedByID != want.DecidedByID ||
				got.DecisionReason != want.DecisionReason {
				t.Errorf("the request is %s by %q (%s); the decision that won was %s by %q (%s)",
					got.Status, got.DecidedBy, got.DecisionReason, want.Status, want.DecidedBy, want.DecisionReason)
			}
			if key := h.storedRequest(t, request.ID).liveKey; key.Valid != got.Status.Live() {
				t.Errorf("decided %s, the request holds its fingerprint: %v", got.Status, key.Valid)
			}
		})
	}
}

// The same thing is asked for by several people at the same moment. One
// request is written, and every other ask is told one is already waiting —
// by the database, since nothing else orders them.
func TestAsksForOneFingerprintMadeTogetherLeaveOneLiveRequest(t *testing.T) {
	h := newDeviationHarness(t)
	instanceID := h.startOneStep(t, entities.Node{Name: "Approve", Type: entities.UserTask, Assignee: "alice"})
	for round := range raceRounds {
		t.Run(fmt.Sprintf("round %d", round), func(t *testing.T) {
			fingerprint := fmt.Sprintf("dv1-race-ask-%d", round)
			asks := make([]entities.DeviationRequest, atOnce)
			for i := range asks {
				asks[i] = h.sampleRequest(instanceID, fingerprint)
			}
			errs := together(t, "the asks", func(i int) error {
				_, err := h.createRequest(h.tenantContext(), asks[i])
				return err
			})
			winner := oneWon(t, "asking for one thing", errs, func(err error) bool {
				return errors.Is(err, repocontracts.ErrDeviationRequestAlreadyWaiting)
			})
			var stored int
			if err := h.db.Raw(`SELECT count(*) FROM deviation_requests WHERE project_id = ? AND fingerprint = ?`, h.projID, fingerprint).
				Row().Scan(&stored); err != nil {
				t.Fatalf("count the requests: %v", err)
			}
			if stored != 1 {
				t.Errorf("%d request(s) were written for one fingerprint", stored)
			}
			live, ok, err := h.repo.DeviationRequest().FindLive(h.tenantContext(), h.projID, fingerprint)
			if err != nil || !ok || live.ID != asks[winner].ID {
				t.Errorf("the live request is %s (%v, %v); the ask that won was %s", live.ID, ok, err, asks[winner].ID)
			}
		})
	}
}

// Several decisions of one waiting ledger row, made together: one is written,
// and every other is told the row was decided.
func TestDecisionsOfOneLedgerRowMadeTogetherEndInOne(t *testing.T) {
	h := newDeviationHarness(t)
	instanceID := h.startOneStep(t, entities.Node{Name: "Approve", Type: entities.UserTask, Assignee: "alice"})
	for round := range raceRounds {
		t.Run(fmt.Sprintf("round %d", round), func(t *testing.T) {
			visit := fmt.Sprintf("dv1-race-row-%d", round)
			row := h.mustWrite(t, h.pendingWaive(instanceID, visit))
			decisions := make([]repocontracts.LedgerRowDecision, atOnce)
			for i := range decisions {
				status := entities.DeviationApplied
				if i%2 == 1 {
					status = entities.DeviationRejected
				}
				decisions[i] = repocontracts.LedgerRowDecision{
					Status: status, ApprovedBy: fmt.Sprintf("admin-%d", i), ApprovedByID: uuid.Must(uuid.NewV7()), DecidedAt: time.Now(),
					Details: map[string]any{"decision": float64(i)},
				}
			}
			errs := together(t, "the decisions", func(i int) error {
				_, err := h.decideRow(h.tenantContext(), row.ID, decisions[i])
				return err
			})
			winner := oneWon(t, "deciding one ledger row", errs, func(err error) bool {
				return errors.Is(err, repocontracts.ErrDeviationRowDecided)
			})
			got, want := h.theRow(t, instanceID, row.ID), decisions[winner]
			if got.Status != want.Status || got.ApprovedBy != want.ApprovedBy || got.ApprovedByID != want.ApprovedByID ||
				got.Details["decision"] != float64(winner) {
				t.Errorf("the row is %s by %q with %v; the decision that won was %s by %q (%d)",
					got.Status, got.ApprovedBy, got.Details, want.Status, want.ApprovedBy, winner)
			}
			if live := h.liveVisitKeyOf(t, row.ID); live.Valid != got.Status.Live() {
				t.Errorf("decided %s, the row holds its visit: %v", got.Status, live.Valid)
			}
		})
	}
}

// Several waiting rows written for one visit at the same moment: one is
// written. The ledger's own write is 3a-1's and answers the database's
// refusal wrapped as it is — a unique violation — which is what a caller can
// recognise it by.
func TestWaitingRowsForOneVisitWrittenTogetherLeaveOne(t *testing.T) {
	h := newDeviationHarness(t)
	instanceID := h.startOneStep(t, entities.Node{Name: "Approve", Type: entities.UserTask, Assignee: "alice"})
	for round := range raceRounds {
		t.Run(fmt.Sprintf("round %d", round), func(t *testing.T) {
			visit := fmt.Sprintf("dv1-race-visit-%d", round)
			rows := make([]entities.Deviation, atOnce)
			for i := range rows {
				rows[i] = h.pendingWaive(instanceID, visit)
			}
			errs := together(t, "the writes", func(i int) error {
				_, err := h.write(h.tenantContext(), rows[i])
				return err
			})
			winner := oneWon(t, "holding one visit", errs, func(err error) bool {
				return errors.Is(err, stormruntime.ErrUniqueViolation)
			})
			live, ok, err := h.repo.Deviation().FindLiveByVisit(h.tenantContext(), instanceID, visit)
			if err != nil || !ok || live.ID != rows[winner].ID {
				t.Errorf("the visit's live row is %s (%v, %v); the write that won was %s", live.ID, ok, err, rows[winner].ID)
			}
			var stored int
			if err := h.db.Raw(`SELECT count(*) FROM instance_deviations WHERE instance_id = ? AND visit_key = ?`, instanceID, visit).
				Row().Scan(&stored); err != nil {
				t.Fatalf("count the rows: %v", err)
			}
			if stored != 1 {
				t.Errorf("%d row(s) were written for one visit", stored)
			}
		})
	}
}

// heldOpen is a change to one row that somebody has made and not yet
// committed: the first of two people, part-way through.
type heldOpen struct {
	t       *testing.T
	h       *deviationHarness
	session int
	commit  func() error
	undo    func()
}

// holdOpen runs statement in a transaction of its own and leaves it open.
func (h *deviationHarness) holdOpen(t *testing.T, statement string, args ...any) *heldOpen {
	t.Helper()
	tx := h.db.Begin()
	if tx.Error != nil {
		t.Fatalf("open the first decision's transaction: %v", tx.Error)
	}
	settled := false
	t.Cleanup(func() {
		if !settled {
			tx.Rollback()
		}
	})
	held := &heldOpen{t: t, h: h}
	if err := tx.Raw("SELECT pg_backend_pid()").Scan(&held.session).Error; err != nil {
		t.Fatalf("read the first decision's session: %v", err)
	}
	if err := tx.Exec(statement, args...).Error; err != nil {
		t.Fatalf("make the first decision: %v", err)
	}
	held.commit = func() error { settled = true; return tx.Commit().Error }
	held.undo = func() { settled = true; tx.Rollback() }
	return held
}

// untilBehind waits until another session waits for a lock this one holds.
// It fails the test if what was sent finishes first: then it never waited.
func (held *heldOpen) untilBehind(finished <-chan error) {
	held.t.Helper()
	const waitingBehind = `SELECT count(*) FROM pg_stat_activity a WHERE ? = ANY(pg_blocking_pids(a.pid))`
	for deadline := time.Now().Add(raceWait); time.Now().Before(deadline); time.Sleep(5 * time.Millisecond) {
		select {
		case err := <-finished:
			held.t.Fatalf("the second decision finished (%v) without waiting for the first", err)
		default:
		}
		var waiting int
		if err := held.h.db.Raw(waitingBehind, held.session).Scan(&waiting).Error; err != nil {
			held.t.Fatalf("look for the decision waiting behind the first: %v", err)
		}
		if waiting > 0 {
			return
		}
	}
	held.t.Fatalf("no decision was waiting behind the first after %s", raceWait)
}

func answerOf(t *testing.T, finished <-chan error, what string) error {
	t.Helper()
	select {
	case err := <-finished:
		return err
	case <-time.After(raceWait):
		t.Fatalf("%s did not finish once the first was settled", what)
		return nil
	}
}

// A decision that arrives while another is being made waits for it, and then
// decides from what it left — not from what the request was when it set out.
// If the first is written, the second is told; if the first is undone, the
// second is the decision.
func TestADecisionWaitsForTheOneAheadOfItAndReadsWhatItLeft(t *testing.T) {
	h := newDeviationHarness(t)
	const firstRejects = `UPDATE deviation_requests SET status = 'rejected', live_key = NULL, decided_by = 'ana' WHERE id = ?`
	send := func(request entities.DeviationRequest) <-chan error {
		finished := make(chan error, 1)
		go func() {
			_, err := h.transition(h.tenantContext(), request.ID, entities.DeviationRequestPending, decisionTo(entities.DeviationRequestApproved))
			finished <- err
		}()
		return finished
	}

	t.Run("the first is written", func(t *testing.T) {
		request := h.mustCreateRequest(t, h.sampleMigration(t, "mf1-behind-written"))
		first := h.holdOpen(t, firstRejects, request.ID)
		second := send(request)
		first.untilBehind(second)
		if err := first.commit(); err != nil {
			t.Fatalf("commit the first decision: %v", err)
		}
		if err := answerOf(t, second, "the second decision"); !errors.Is(err, repocontracts.ErrDeviationRequestDecided) {
			t.Fatalf("the decision that waited: %v, want ErrDeviationRequestDecided", err)
		}
		if got := h.mustGetRequest(t, request.ID); got.Status != entities.DeviationRequestRejected || got.DecidedBy != "ana" {
			t.Errorf("the request is %s by %q; the first decision was a rejection by ana", got.Status, got.DecidedBy)
		}
	})

	t.Run("the first is undone", func(t *testing.T) {
		request := h.mustCreateRequest(t, h.sampleMigration(t, "mf1-behind-undone"))
		first := h.holdOpen(t, firstRejects, request.ID)
		second := send(request)
		first.untilBehind(second)
		first.undo()
		if err := answerOf(t, second, "the second decision"); err != nil {
			t.Fatalf("the decision that waited behind one that was undone: %v", err)
		}
		if got := h.mustGetRequest(t, request.ID); got.Status != entities.DeviationRequestApproved || got.DecidedBy != "budi" {
			t.Errorf("the request is %s by %q; want approved by budi", got.Status, got.DecidedBy)
		}
	})
}

// The same, for a ledger row.
func TestALedgerDecisionWaitsForTheOneAheadOfItAndReadsWhatItLeft(t *testing.T) {
	h := newDeviationHarness(t)
	instanceID := h.startOneStep(t, entities.Node{Name: "Approve", Type: entities.UserTask, Assignee: "alice"})
	const firstRejects = `UPDATE instance_deviations SET status = 'rejected', live_visit_key = NULL WHERE id = ?`
	send := func(row entities.Deviation) <-chan error {
		finished := make(chan error, 1)
		go func() {
			_, err := h.decideRow(h.tenantContext(), row.ID, repocontracts.LedgerRowDecision{
				Status: entities.DeviationApplied, ApprovedBy: "budi", ApprovedByID: budi, DecidedAt: time.Now()})
			finished <- err
		}()
		return finished
	}

	t.Run("the first is written", func(t *testing.T) {
		row := h.mustWrite(t, h.pendingWaive(instanceID, "dv1-row-behind-written"))
		first := h.holdOpen(t, firstRejects, row.ID)
		second := send(row)
		first.untilBehind(second)
		if err := first.commit(); err != nil {
			t.Fatalf("commit the first decision: %v", err)
		}
		if err := answerOf(t, second, "the second decision"); !errors.Is(err, repocontracts.ErrDeviationRowDecided) {
			t.Fatalf("the decision that waited: %v, want ErrDeviationRowDecided", err)
		}
		if got := h.theRow(t, instanceID, row.ID); got.Status != entities.DeviationRejected || got.ApprovedBy != "" {
			t.Errorf("the row is %s, approved by %q; the first decision was a rejection", got.Status, got.ApprovedBy)
		}
	})

	t.Run("the first is undone", func(t *testing.T) {
		row := h.mustWrite(t, h.pendingWaive(instanceID, "dv1-row-behind-undone"))
		first := h.holdOpen(t, firstRejects, row.ID)
		second := send(row)
		first.untilBehind(second)
		first.undo()
		if err := answerOf(t, second, "the second decision"); err != nil {
			t.Fatalf("the decision that waited behind one that was undone: %v", err)
		}
		if got := h.theRow(t, instanceID, row.ID); got.Status != entities.DeviationApplied || got.ApprovedBy != "budi" {
			t.Errorf("the row is %s, approved by %q; want applied by budi", got.Status, got.ApprovedBy)
		}
	})
}

// The sweep passes by a request somebody holds — it is being decided, or
// reported on, right now — and does not wait for it; it takes the rest, and
// takes that one on a later pass if it is still there to take.
func TestTheSweepPassesByARequestSomebodyHolds(t *testing.T) {
	h := newDeviationHarness(t)
	instanceID := h.startOneStep(t, entities.Node{Name: "Approve", Type: entities.UserTask, Assignee: "alice"})
	overdue := func(fingerprint string) entities.DeviationRequest {
		r := h.sampleRequest(instanceID, fingerprint)
		r.ExpiresAt = time.Now().Add(-time.Minute)
		return h.mustCreateRequest(t, r)
	}
	heldRequest, free := overdue("dv1-sweep-held"), overdue("dv1-sweep-free")
	// What the sweep read, or why it could not. The goroutine reports through
	// the channel and never through t: if the read is still waiting when the
	// test gives up on it, it ends after the test has.
	type sweepRead struct {
		ids []uuid.UUID
		err error
	}
	sweep := func() <-chan sweepRead {
		swept := make(chan sweepRead, 1)
		go func() {
			var ids []uuid.UUID
			err := h.repo.UnitOfWork().Do(entities.WithSystemContext(t.Context()), func(tx context.Context) error {
				rows, err := h.repo.DeviationRequest().ListOverdue(tx, time.Now(), repocontracts.SweepCursor{}, 100)
				ids = requestIDs(rows)
				return err
			})
			swept <- sweepRead{ids: ids, err: err}
		}()
		return swept
	}
	read := func(what string) []uuid.UUID {
		t.Helper()
		select {
		case got := <-sweep():
			if got.err != nil {
				t.Fatalf("the sweep's read %s: %v", what, got.err)
			}
			return got.ids
		case <-time.After(raceWait):
			t.Fatalf("the sweep %s did not answer within %s: it waited for a row somebody holds", what, raceWait)
			return nil
		}
	}

	holder := h.holdOpen(t, `SELECT 1 FROM deviation_requests WHERE id = ? FOR UPDATE`, heldRequest.ID)
	if got := read("beside a held request"); !sameIDs(got, []uuid.UUID{free.ID}) {
		t.Errorf("beside a held request the sweep read %v; want only the free one, %s", got, free.ID)
	}
	holder.undo()
	if got := read("after the request was let go"); !sameIDs(got, []uuid.UUID{heldRequest.ID, free.ID}) {
		t.Errorf("once the request was let go the sweep read %v; want both", got)
	}
}
