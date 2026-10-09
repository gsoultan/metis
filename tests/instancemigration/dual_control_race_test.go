package instancemigration

import (
	"errors"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/gsoultan/metis/internal/pkg/apierr"
	"github.com/gsoultan/metis/server/domains/entities"
	servicecontracts "github.com/gsoultan/metis/server/domains/services/contracts"
	"github.com/gsoultan/metis/server/domains/services/impl"
	"gorm.io/gorm"
)

// Decisions on one request, made at once.
//
// Every decision takes the request's row before it looks at the request, so
// two that meet are put in an order there, and the second reads what the
// first left. The tests below make the meeting happen every time: the row is
// held open by a transaction of the test's own, the decisions are sent, and
// the row is let go only once each of them is waiting for it. Then a few
// rounds with nothing held, for whatever order the scheduler chooses. Each
// round is a subtest with a fixture of its own, closed when the round ends.

// freeRounds is how many rounds each race is also run with nothing ordering
// it.
const freeRounds = 3

// heldRequest is a request's row held open by a transaction of the test's.
type heldRequest struct {
	tx      *gorm.DB
	session int
	letGo   bool
}

// holdingTheRequest takes a request's row as a decision takes it, in a
// transaction that stays open until release. Whatever comes for the row waits.
func (f *fixture) holdingTheRequest(t *testing.T, requestID uuid.UUID) *heldRequest {
	t.Helper()
	tx := f.db.Begin()
	if tx.Error != nil {
		t.Fatalf("open the transaction that holds the request: %v", tx.Error)
	}
	held := &heldRequest{tx: tx}
	t.Cleanup(func() {
		if !held.letGo {
			tx.Rollback()
		}
	})
	if err := tx.Raw("SELECT pg_backend_pid()").Scan(&held.session).Error; err != nil {
		t.Fatalf("read the holder's session: %v", err)
	}
	var id string
	if err := tx.Raw("SELECT id::text FROM deviation_requests WHERE id = ? FOR UPDATE", requestID).Scan(&id).Error; err != nil || id == "" {
		t.Fatalf("hold the request's row: %q %v", id, err)
	}
	return held
}

// release lets the row go.
func (h *heldRequest) release(t *testing.T) {
	t.Helper()
	h.letGo = true
	if err := h.tx.Commit().Error; err != nil {
		t.Fatalf("let the request's row go: %v", err)
	}
}

// waitUntilQueued waits until so many backends are waiting for the row the
// holder has. Bounded: a decision that never comes for the
// row fails the test here, naming how many did.
func (f *fixture) waitUntilQueued(t *testing.T, held *heldRequest, waiters int) {
	t.Helper()
	// Waiting for a lock, and queued for a row of this test's own table of
	// requests — which no other test shares: each has a schema of its own.
	// Counted whoever is ahead of it: the first to come waits for the holder,
	// and each one after waits for the one before.
	const queuedForTheRequest = `SELECT count(*) FROM pg_stat_activity a
		 WHERE cardinality(pg_blocking_pids(a.pid)) > 0 AND a.pid <> ?
		   AND EXISTS (SELECT 1 FROM pg_locks l
		                 JOIN pg_class c ON c.oid = l.relation
		                 JOIN pg_namespace n ON n.oid = c.relnamespace
		                WHERE l.pid = a.pid AND l.locktype = 'tuple'
		                  AND c.relname = 'deviation_requests' AND n.nspname = current_schema())`
	waiting := 0
	for deadline := time.Now().Add(raceWait); time.Now().Before(deadline); time.Sleep(5 * time.Millisecond) {
		if err := f.db.Raw(queuedForTheRequest, held.session).Scan(&waiting).Error; err != nil {
			t.Fatalf("look for what waits behind the request's row: %v", err)
		}
		if waiting >= waiters {
			return
		}
	}
	t.Fatalf("%d of %d were waiting for the request's row within %s", waiting, waiters, raceWait)
}

// decided is what one decision sent from a goroutine was answered.
type decided struct {
	who string
	out entities.DeviationRequestOutcome
	err error
}

// together runs each of the sends in a goroutine of its own. With a row held
// it waits for all of them to be queued behind it and then lets it go; with
// none it only starts them. It answers when all have finished, or fails the
// test when they have not within raceWait.
func (f *fixture) together(t *testing.T, held *heldRequest, sends ...func()) {
	t.Helper()
	var racing sync.WaitGroup
	for _, send := range sends {
		racing.Go(send)
	}
	if held != nil {
		f.waitUntilQueued(t, held, len(sends))
		held.release(t)
	}
	waitForAll(t, &racing, "the decisions sent together")
}

// rounds runs a race once with the request's row held, so that everything
// sent meets at it, and then freeRounds times with nothing held.
func rounds(t *testing.T, round func(t *testing.T, ordered bool)) {
	t.Run("met at the row", func(t *testing.T) { round(t, true) })
	for i := range freeRounds {
		t.Run("free "+string(rune('a'+i)), func(t *testing.T) { round(t, false) })
	}
}

// skipsOf is how many ledger rows and node_skipped entries an instance has:
// one of each is one skip.
func (f *fixture) skipsOf(t *testing.T, instanceID uuid.UUID) (rows, entries int) {
	t.Helper()
	rows = len(f.ledger(t, instanceID))
	for _, kind := range f.trailTypes(t, instanceID) {
		if kind == impl.EventNodeSkipped {
			entries++
		}
	}
	return rows, entries
}

// Two administrators approve one request at once. One approval is taken and
// runs the migration; the other is told somebody approved first. The step is
// skipped once, and the request names the one who was first.
func TestTwoApprovalsOfOneMigrationAtOnceRunItOnce(t *testing.T) {
	rounds(t, func(t *testing.T, ordered bool) {
		f := newFixture(t)
		_, _, v2, requestID := f.askToSkipOps(t)
		var held *heldRequest
		if ordered {
			held = f.holdingTheRequest(t, requestID)
		}
		answers := make([]decided, 2)
		approve := func(i int, who string) func() {
			return func() {
				out, err := f.svc.ApproveDeviationRequest(adminAs(f.ctx, who), requestID, "")
				answers[i] = decided{who: who, out: out, err: err}
			}
		}
		f.together(t, held, approve(0, "omar"), approve(1, "pia"))

		var won, lost []decided
		for _, answer := range answers {
			if answer.err == nil {
				won = append(won, answer)
			} else {
				lost = append(lost, answer)
			}
		}
		if len(won) != 1 || len(lost) != 1 {
			t.Fatalf("two approvals at once: %+v and %+v, want exactly one taken", answers[0].err, answers[1].err)
		}
		if !won[0].out.Applied || won[0].out.MigrationResult == nil || won[0].out.MigrationResult.Changed != 1 {
			t.Fatalf("the approval that was taken: %+v", won[0].out)
		}
		if !errors.Is(lost[0].err, apierr.ErrInvalidArgument) || !strings.Contains(lost[0].err.Error(), won[0].who+" approved this") || lost[0].out.Applied {
			t.Fatalf("the approval that came second: %v, want to be told %s approved first", lost[0].err, won[0].who)
		}
		instance := f.onlyInstance(t)
		if instance.Definition == nil || instance.Definition.ID != v2 {
			t.Fatal("the instance was not moved")
		}
		if rows, entries := f.skipsOf(t, instance.ID); rows != 1 || entries != 1 {
			t.Fatalf("the step was skipped %d time(s) in the ledger and %d on the trail, want once", rows, entries)
		}
		if row := f.ledger(t, instance.ID)[0]; row.ApprovedBy != won[0].who || row.RequestID != requestID {
			t.Fatalf("the skip names %q as having approved it, want %s", row.ApprovedBy, won[0].who)
		}
		if got := f.storedStatus(t, requestID); got != "applied" {
			t.Fatalf("the request is %s, want applied", got)
		}
	})
}

// An approval and a rejection of one request at once end in one decision:
// approved and run, with the rejection told so — or rejected, with the
// approval told so and nothing moved. Never both, and never neither.
func TestApprovingAndRejectingAMigrationAtOnceEndInOneDecision(t *testing.T) {
	rounds(t, func(t *testing.T, ordered bool) {
		f := newFixture(t)
		_, v1, v2, requestID := f.askToSkipOps(t)
		var held *heldRequest
		if ordered {
			held = f.holdingTheRequest(t, requestID)
		}
		var approved decided
		var rejectErr error
		f.together(t, held,
			func() {
				out, err := f.svc.ApproveDeviationRequest(adminAs(f.ctx, "omar"), requestID, "")
				approved = decided{who: "omar", out: out, err: err}
			},
			func() {
				_, rejectErr = f.svc.RejectDeviationRequest(adminAs(f.ctx, "pia"), requestID, "not this quarter")
			},
		)
		switch {
		case approved.err == nil && rejectErr != nil:
			if !errors.Is(rejectErr, apierr.ErrInvalidArgument) || !strings.Contains(rejectErr.Error(), "omar approved this") {
				t.Fatalf("the rejection that came second: %v, want to be told omar approved first", rejectErr)
			}
			instance := f.onlyInstance(t)
			if rows, entries := f.skipsOf(t, instance.ID); instance.Definition.ID != v2 || rows != 1 || entries != 1 || f.storedStatus(t, requestID) != "applied" {
				t.Fatalf("approved first: the instance is on %s with %d row(s), the request %s", instance.Definition.ID, rows, f.storedStatus(t, requestID))
			}
		case approved.err != nil && rejectErr == nil:
			if !errors.Is(approved.err, apierr.ErrInvalidArgument) || !strings.Contains(approved.err.Error(), "pia rejected this") || approved.out.Applied {
				t.Fatalf("the approval that came second: %v, want to be told pia rejected first", approved.err)
			}
			if got := f.storedStatus(t, requestID); got != "rejected" {
				t.Fatalf("rejected first: the request is %s", got)
			}
			f.assertNothingMoved(t, v1)
		default:
			t.Fatalf("an approval and a rejection at once: %v and %v, want exactly one of them taken", approved.err, rejectErr)
		}
	})
}

// An approval that arrives as the request's deadline passes, with the sweep
// closing it at the same moment: the request expires once, the approval is
// refused, and nothing moves — whichever of the two writes the expiry down.
// While the approval holds the row, the sweep passes the request by instead
// of waiting for it.
func TestApprovingAMigrationAsItExpiresEndsExpiredOnce(t *testing.T) {
	t.Run("the sweep passes by a row that is held", func(t *testing.T) {
		f := newFixture(t)
		_, v1, _, requestID := f.askToSkipOps(t)
		f.letTimePass(t, requestID, 1)
		held := f.holdingTheRequest(t, requestID)
		var approveErr error
		var racing sync.WaitGroup
		racing.Go(func() { _, approveErr = f.svc.ApproveDeviationRequest(adminAs(f.ctx, "omar"), requestID, "") })
		f.waitUntilQueued(t, held, 1)
		// The row is held and the approval is queued for it: the sweep does
		// not join the queue.
		if n := f.sweep(t, time.Now()); n != 0 {
			t.Fatalf("the sweep closed %d while the request's row was held, want it passed by", n)
		}
		held.release(t)
		waitForAll(t, &racing, "the approval")
		if !errors.Is(approveErr, apierr.ErrInvalidArgument) || !strings.Contains(approveErr.Error(), "expired on") {
			t.Fatalf("the approval past the deadline: %v, want it refused as expired", approveErr)
		}
		if got := f.storedStatus(t, requestID); got != "expired" {
			t.Fatalf("the request is %s, want the expiry the approval found written down", got)
		}
		if n := f.sweep(t, time.Now()); n != 0 {
			t.Fatalf("a sweep afterwards closed %d more", n)
		}
		f.assertNothingMoved(t, v1)
	})
	for i := range freeRounds {
		t.Run("free "+string(rune('a'+i)), func(t *testing.T) {
			f := newFixture(t)
			_, v1, _, requestID := f.askToSkipOps(t)
			f.letTimePass(t, requestID, 1)
			var approveErr, sweepErr error
			var swept int64
			f.together(t, nil,
				func() { _, approveErr = f.svc.ApproveDeviationRequest(adminAs(f.ctx, "omar"), requestID, "") },
				func() {
					var closed entities.SweptRequests
					closed, sweepErr = f.svc.SweepDeviationRequests(entities.WithSystemContext(f.ctx), time.Now())
					swept = closed.Closed()
				},
			)
			if sweepErr != nil || swept > 1 {
				t.Fatalf("the sweep: %d, %v", swept, sweepErr)
			}
			if !errors.Is(approveErr, apierr.ErrInvalidArgument) || !strings.Contains(approveErr.Error(), "expired on") {
				t.Fatalf("the approval past the deadline: %v, want it refused as expired", approveErr)
			}
			if got := f.storedStatus(t, requestID); got != "expired" {
				t.Fatalf("the request is %s, want expired", got)
			}
			f.assertNothingMoved(t, v1)
		})
	}
}

// An approval and a second ask for the same migration at once.
//
// While the request still waits, the second ask is answered with it and does
// not stand in the approval's way: one request, approved and run. Once the
// request is past its deadline, the two meet at its row: whichever is first
// writes the expiry down, the approval is refused, and the ask makes a fresh
// request — one, whatever the order.
func TestApprovingAMigrationAsItIsAskedForAgain(t *testing.T) {
	t.Run("in time", func(t *testing.T) {
		rounds(t, func(t *testing.T, ordered bool) {
			f := newFixture(t)
			dita, v1, v2, requestID := f.askToSkipOps(t)
			var held *heldRequest
			if ordered {
				held = f.holdingTheRequest(t, requestID)
			}
			var approved decided
			var again entities.PendingApproval
			var askErr error
			var racing sync.WaitGroup
			racing.Go(func() {
				out, err := f.svc.ApproveDeviationRequest(adminAs(f.ctx, "omar"), requestID, "")
				approved = decided{out: out, err: err}
			})
			if ordered {
				// The approval is waiting for the row; the ask reads without
				// it and is answered at once, with the request that waits.
				f.waitUntilQueued(t, held, 1)
				again, askErr = f.svc.RequestMigrationApproval(dita, v1, v2, nil, skipOps("the role was eliminated")...)
				if askErr != nil || again.RequestID != requestID {
					t.Fatalf("the ask while an approval waits for the row: %+v %v, want the request that waits", again, askErr)
				}
				held.release(t)
			} else {
				racing.Go(func() {
					again, askErr = f.svc.RequestMigrationApproval(dita, v1, v2, nil, skipOps("the role was eliminated")...)
				})
			}
			waitForAll(t, &racing, "the approval and the ask")
			if approved.err != nil || !approved.out.Applied {
				t.Fatalf("the approval: %+v %v", approved.out, approved.err)
			}
			// The ask was answered with the request; or, the run under way,
			// told the migration is being applied; or, the run over and the
			// version empty, told there is nothing left that needs anybody.
			if askErr != nil && (!errors.Is(askErr, apierr.ErrInvalidArgument) ||
				(!strings.Contains(askErr.Error(), "is being applied now") && !strings.Contains(askErr.Error(), "needs no second administrator"))) {
				t.Fatalf("the ask beside an approval: %v", askErr)
			}
			instance := f.onlyInstance(t)
			if rows, entries := f.skipsOf(t, instance.ID); instance.Definition.ID != v2 || rows != 1 || entries != 1 {
				t.Fatalf("the instance is on %s with %d row(s) and %d entr(ies), want it moved by one skip", instance.Definition.ID, rows, entries)
			}
			if n := f.requestCount(t); n > 2 || (askErr == nil && again.RequestID == requestID && n != 1) {
				t.Fatalf("%d request(s) exist after the approval and the ask", n)
			}
		})
	})
	t.Run("past its deadline", func(t *testing.T) {
		rounds(t, func(t *testing.T, ordered bool) {
			f := newFixture(t)
			dita, v1, v2, requestID := f.askToSkipOps(t)
			f.letTimePass(t, requestID, 1)
			var held *heldRequest
			if ordered {
				held = f.holdingTheRequest(t, requestID)
			}
			var approveErr, askErr error
			var fresh entities.PendingApproval
			f.together(t, held,
				func() { _, approveErr = f.svc.ApproveDeviationRequest(adminAs(f.ctx, "omar"), requestID, "") },
				func() {
					fresh, askErr = f.svc.RequestMigrationApproval(dita, v1, v2, nil, skipOps("the role was eliminated")...)
				},
			)
			if !errors.Is(approveErr, apierr.ErrInvalidArgument) || !strings.Contains(approveErr.Error(), "expired on") {
				t.Fatalf("the approval past the deadline: %v, want it refused as expired", approveErr)
			}
			if askErr != nil || fresh.RequestID == requestID || fresh.Status != entities.DeviationRequestPending {
				t.Fatalf("the ask past the deadline: %+v %v, want a fresh request", fresh, askErr)
			}
			if got := f.storedStatus(t, requestID); got != "expired" {
				t.Fatalf("the overdue request is %s, want expired", got)
			}
			if n := f.requestCount(t); n != 2 {
				t.Fatalf("%d request(s) exist, want the expired one and the fresh one", n)
			}
			f.assertNothingMoved(t, v1)
		})
	})
}

// Two asks for one migration at the same instant, which no lock orders: the
// database lets one request in. Made by one administrator, both are answered
// with it. Made by two, one has the request and the other is pointed at it.
func TestTwoAsksForOneMigrationAtOnceMakeOneRequest(t *testing.T) {
	for _, second := range []string{"dita", "omar"} {
		for i := range freeRounds {
			t.Run("dita and "+second+" "+string(rune('a'+i)), func(t *testing.T) {
				f := newFixture(t)
				first, target := f.parkedOnOpsApprove(t)
				v1, v2 := uuidOf(t, first), uuidOf(t, target)
				answers := make([]entities.PendingApproval, 2)
				errs := make([]error, 2)
				ask := func(i int, who string) func() {
					return func() {
						answers[i], errs[i] = f.svc.RequestMigrationApproval(adminAs(f.ctx, who), v1, v2, nil, skipOps("the role was eliminated")...)
					}
				}
				f.together(t, nil, ask(0, "dita"), ask(1, second))
				if n := f.requestCount(t); n != 1 {
					t.Fatalf("two asks at once left %d request(s), want the one", n)
				}
				if second == "dita" {
					if errs[0] != nil || errs[1] != nil || answers[0].RequestID != answers[1].RequestID {
						t.Fatalf("one administrator asking twice at once: %+v %v and %+v %v, want the one request both times", answers[0], errs[0], answers[1], errs[1])
					}
					return
				}
				granted, refused := 0, 0
				for _, err := range errs {
					switch {
					case err == nil:
						granted++
					case errors.Is(err, apierr.ErrInvalidArgument) && strings.Contains(err.Error(), "already waiting for approval"):
						refused++
					}
				}
				if granted != 1 || refused != 1 {
					t.Fatalf("two administrators asking at once: %v and %v, want one request made and the other pointed at it", errs[0], errs[1])
				}
				f.assertNothingMoved(t, v1)
			})
		}
	}
}

// Two runs under one approval. While a request is approved — for as long as
// its run takes — a second in-process apply carrying its id is admitted, and
// resumes as any second run of one migration does: it moves nothing the
// first moved, and between them every instance is skipped and moved once.
// Once the run has reported, the request is spent, and no apply runs under it.
func TestTwoRunsUnderOneApprovalMoveEachInstanceOnce(t *testing.T) {
	const instances = 4
	t.Run("the second run, whole, inside the first", func(t *testing.T) {
		f, listing := newRacedFixture(t)
		v1, v2 := f.severalParkedOnOpsApprove(t, instances)
		opts := skipOps("the role was eliminated")
		pending, err := f.svc.RequestMigrationApproval(adminAs(f.ctx, "dita"), v1, v2, nil, opts...)
		if err != nil {
			t.Fatalf("ask: %v", err)
		}
		under := append(slices.Clip(opts), servicecontracts.WithApprovedRequest(pending.RequestID))
		var second entities.MigrationResult
		var secondErr error
		listing.atTheApprovedApplysListing(func() { second, secondErr = f.svc.ApplyInstanceMigration(f.ctx, v1, v2, nil, under...) })
		out, err := f.svc.ApproveDeviationRequest(adminAs(f.ctx, "omar"), pending.RequestID, "")
		reached(t, listing)
		if secondErr != nil || second.Changed != instances {
			t.Fatalf("the second run, admitted while the request was approved: %+v %v, want it to move all %d", second, secondErr, instances)
		}
		if err != nil || out.MigrationResult == nil || out.MigrationResult.Changed != 0 || len(out.MigrationResult.PassedOver) != instances || out.Applied {
			t.Fatalf("the first run, reaching instances the second had moved: %+v %v, want it to pass every one over and say nothing was applied", out, err)
		}
		f.requireEachSkippedOnce(t, v1, v2, pending.RequestID, instances)
		// The run has reported: the request reads applied, with what the
		// first run did — nothing — and is spent. (That no apply runs under a
		// spent request is TestAnApplyRefusesASkipWithoutAnApprovedRequest's;
		// here no instance is left on the old version for one to be about.)
		if got := f.storedStatus(t, pending.RequestID); got != "applied" || out.Request.Outcome["changed"] != float64(0) ||
			out.Request.Outcome["passed_over"] != float64(instances) || out.Request.Outcome["note"] != "every instance the run reached was passed over" {
			t.Fatalf("the request is %s with %v, want applied, saying the reporting run changed none and passed %d over", got, out.Request.Outcome, instances)
		}
	})
	for i := range freeRounds {
		t.Run("both at once "+string(rune('a'+i)), func(t *testing.T) {
			f, listing := newRacedFixture(t)
			v1, v2 := f.severalParkedOnOpsApprove(t, instances)
			opts := skipOps("the role was eliminated")
			pending, err := f.svc.RequestMigrationApproval(adminAs(f.ctx, "dita"), v1, v2, nil, opts...)
			if err != nil {
				t.Fatalf("ask: %v", err)
			}
			under := append(slices.Clip(opts), servicecontracts.WithApprovedRequest(pending.RequestID))
			var second entities.MigrationResult
			var secondErr error
			var racing sync.WaitGroup
			// Started when the first run has listed its instances, and left to
			// run beside it.
			listing.atTheApprovedApplysListing(func() {
				racing.Go(func() { second, secondErr = f.svc.ApplyInstanceMigration(f.ctx, v1, v2, nil, under...) })
			})
			out, err := f.svc.ApproveDeviationRequest(adminAs(f.ctx, "omar"), pending.RequestID, "")
			reached(t, listing)
			waitForAll(t, &racing, "the second run")
			// The second run was admitted while the request was approved, or —
			// arriving once the first had reported — refused as spent.
			if secondErr != nil && (!errors.Is(secondErr, apierr.ErrForbidden) || !strings.Contains(secondErr.Error(), "not an approved migration")) {
				t.Fatalf("the second run: %v", secondErr)
			}
			if err != nil || out.MigrationResult == nil {
				t.Fatalf("the first run: %+v %v", out, err)
			}
			if moved := out.MigrationResult.Changed + second.Changed; moved != instances {
				t.Fatalf("the two runs moved %d and %d instance(s), want %d between them", out.MigrationResult.Changed, second.Changed, instances)
			}
			f.requireEachSkippedOnce(t, v1, v2, pending.RequestID, instances)
		})
	}
}

// requireEachSkippedOnce fails unless every instance runs the new version,
// none the old, and each has exactly one skip — one ledger row and one
// node_skipped entry — under the request.
func (f *fixture) requireEachSkippedOnce(t *testing.T, v1, v2, requestID uuid.UUID, instances int) {
	t.Helper()
	if left := f.stillOn(t, v1.String()); len(left) != 0 {
		t.Fatalf("%d instance(s) are still on the old version", len(left))
	}
	moved := f.stillOn(t, v2.String())
	if len(moved) != instances {
		t.Fatalf("%d instance(s) are on the new version, want %d", len(moved), instances)
	}
	for _, instance := range moved {
		rows, entries := f.skipsOf(t, instance.ID)
		if rows != 1 || entries != 1 {
			t.Fatalf("instance %s was skipped %d time(s) in the ledger and %d on the trail, want once", instance.ID, rows, entries)
		}
		if row := f.ledger(t, instance.ID)[0]; row.RequestID != requestID || row.ApprovedBy != "omar" {
			t.Fatalf("the skip of %s names request %s approved by %q", instance.ID, row.RequestID, row.ApprovedBy)
		}
	}
}

// The gate and the sweep, at one request at once. The gate reads the request
// under its row; the sweep passes by a row that is held, and marks only a
// request that is still approved. So an apply under an approved request is
// either admitted — and moves the instance under that request — or, the
// sweep having marked the request first, refused with nothing moved. Never
// refused and moved, and never moved under nothing.
func TestTheGateAndTheSweepAtOneApprovedRequest(t *testing.T) {
	later := func() time.Time { return time.Now().Add(2 * time.Hour) }
	approvedAMinuteAgo := func(t *testing.T) (f *fixture, v1, v2, requestID uuid.UUID, under []servicecontracts.MigrationOption) {
		t.Helper()
		f = newFixture(t)
		_, v1, v2, requestID = f.askToSkipOps(t)
		f.leftApproved(t, requestID, "1 minute")
		under = append(slices.Clip(skipOps("the role was eliminated")), servicecontracts.WithApprovedRequest(requestID))
		return f, v1, v2, requestID, under
	}

	t.Run("the gate holds the row and the sweep passes by", func(t *testing.T) {
		f, v1, v2, requestID, under := approvedAMinuteAgo(t)
		held := f.holdingTheRequest(t, requestID)
		var applyErr error
		var racing sync.WaitGroup
		racing.Go(func() { _, applyErr = f.svc.ApplyInstanceMigration(f.ctx, v1, v2, nil, under...) })
		// The gate is waiting to read the request under its row.
		f.waitUntilQueued(t, held, 1)
		if n := f.sweep(t, later()); n != 0 {
			t.Fatalf("the sweep closed %d while the request's row was held, want it passed by", n)
		}
		held.release(t)
		waitForAll(t, &racing, "the apply")
		instance := f.onlyInstance(t)
		if applyErr != nil || instance.Definition.ID != v2 {
			t.Fatalf("the apply the gate admitted: %v, the instance on %s", applyErr, instance.Definition.ID)
		}
		if rows := f.ledger(t, instance.ID); len(rows) != 1 || rows[0].RequestID != requestID {
			t.Fatalf("the admitted run's ledger: %+v", rows)
		}
	})

	t.Run("the sweep first", func(t *testing.T) {
		f, v1, v2, requestID, under := approvedAMinuteAgo(t)
		if n := f.sweep(t, later()); n != 1 {
			t.Fatalf("the sweep closed %d, want the one", n)
		}
		_, err := f.svc.ApplyInstanceMigration(f.ctx, v1, v2, nil, under...)
		if !errors.Is(err, apierr.ErrForbidden) || !strings.Contains(err.Error(), "is interrupted, not an approved migration; nothing was moved") {
			t.Fatalf("an apply under a request the sweep had marked: %v, want it forbidden", err)
		}
		f.assertNothingMoved(t, v1)
		if got := f.storedStatus(t, requestID); got != "interrupted" {
			t.Fatalf("the request is %s", got)
		}
	})

	for i := range freeRounds {
		t.Run("free "+string(rune('a'+i)), func(t *testing.T) {
			f, v1, v2, requestID, under := approvedAMinuteAgo(t)
			var applyErr, sweepErr error
			f.together(t, nil,
				func() { _, applyErr = f.svc.ApplyInstanceMigration(f.ctx, v1, v2, nil, under...) },
				func() { _, sweepErr = f.svc.SweepDeviationRequests(entities.WithSystemContext(f.ctx), later()) },
			)
			if sweepErr != nil {
				t.Fatalf("the sweep: %v", sweepErr)
			}
			instance := f.onlyInstance(t)
			if applyErr == nil {
				if rows := f.ledger(t, instance.ID); instance.Definition.ID != v2 || len(rows) != 1 || rows[0].RequestID != requestID {
					t.Fatalf("the apply was admitted: the instance is on %s with %+v", instance.Definition.ID, rows)
				}
				return
			}
			if !errors.Is(applyErr, apierr.ErrForbidden) || !strings.Contains(applyErr.Error(), "nothing was moved") {
				t.Fatalf("the apply beside the sweep: %v", applyErr)
			}
			f.assertNothingMoved(t, v1)
		})
	}
}
