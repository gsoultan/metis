package deviation_test

import (
	"context"
	"errors"
	"slices"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/gsoultan/metis/internal/pkg/apierr"
	"github.com/gsoultan/metis/server/domains/entities"
	repocontracts "github.com/gsoultan/metis/server/repositories/contracts"
	stormruntime "github.com/gsoultan/storm/runtime"
	"gorm.io/gorm"
)

// sweepRead is one of the sweep's reads, as the repository has it.
type sweepRead = func(context.Context, time.Time, repocontracts.SweepCursor, int) ([]entities.DeviationRequest, error)

// fromTheStart is one of the sweep's reads with its cursor at the start: the
// read the tests written before it took a cursor asked for.
func fromTheStart(list sweepRead) func(context.Context, time.Time, int) ([]entities.DeviationRequest, error) {
	return func(ctx context.Context, now time.Time, limit int) ([]entities.DeviationRequest, error) {
		return list(ctx, now, repocontracts.SweepCursor{}, limit)
	}
}

// A sweep reads on from where it stopped. Its reads are ordered by deadline
// and then by id, and answer only what comes strictly after the cursor — so a
// pass that could not close a request moves beyond it and reaches the ones
// behind, each once, and a pass of any page size reads every request there
// is and then nothing. Two requests with one deadline are told apart by id. A
// row somebody else holds is not answered, and the cursor passes it with the
// rest. The cursor narrows a read and never widens it: another
// organization's sweep still reads nothing of this one's.
func TestTheSweepsReadsGoOnFromACursor(t *testing.T) {
	h := newDeviationHarness(t)
	system := entities.WithSystemContext(context.Background())
	requests := h.repo.DeviationRequest()
	now := time.Now()

	// Five that wait past their deadline. The third and fourth share one
	// deadline, to the microsecond.
	var overdue []uuid.UUID
	for i := range 5 {
		instanceID := h.startOneStep(t, entities.Node{Name: "Approve", Type: entities.UserTask, Assignee: "alice"})
		written := h.mustCreateRequest(t, h.sampleRequest(instanceID, "dv1-cursor-"+string(rune('a'+i))))
		overdue = append(overdue, written.ID)
	}
	base := now.Add(-10 * time.Hour).Truncate(time.Microsecond)
	for i, minutes := range []int{0, 1, 2, 2, 3} {
		if err := h.db.Exec(`UPDATE deviation_requests SET expires_at = ? WHERE id = ?`, base.Add(time.Duration(minutes)*time.Minute), overdue[i]).Error; err != nil {
			t.Fatalf("set a deadline: %v", err)
		}
	}
	if slices.Compare(overdue[2][:], overdue[3][:]) > 0 {
		overdue[2], overdue[3] = overdue[3], overdue[2]
	}
	// Two approved whose run never reported, for the other read.
	var unreported []uuid.UUID
	for _, fingerprint := range []string{"mf1-cursor-a", "mf1-cursor-b"} {
		approved := h.requestAt(t, h.sampleMigration(t, fingerprint), entities.DeviationRequestApproved)
		unreported = append(unreported, approved.ID)
	}
	for i, id := range unreported {
		if err := h.db.Exec(`UPDATE deviation_requests SET decided_at = ?, expires_at = ? WHERE id = ?`,
			now.Add(-3*time.Hour), base.Add(time.Duration(10+i)*time.Minute), id).Error; err != nil {
			t.Fatalf("let the run window close: %v", err)
		}
	}

	// Two the clock has not closed, each standing after every request above
	// in a read's order: one that waits and is not yet due, and an approved
	// one whose run window is open. A cursor says where a read goes on from
	// and nothing else — the predicate on time still holds beside it — so
	// neither read may answer them, from the start or from any cursor. Every
	// comparison below is with exactly the requests above: each is also a
	// check that these two are left out.
	notDue := h.mustCreateRequest(t, h.sampleRequest(
		h.startOneStep(t, entities.Node{Name: "Approve", Type: entities.UserTask, Assignee: "alice"}), "dv1-cursor-not-due"))
	running := h.requestAt(t, h.sampleMigration(t, "mf1-cursor-running"), entities.DeviationRequestApproved)
	if !notDue.ExpiresAt.After(now.Add(time.Hour)) || running.DecidedAt == nil || running.DecidedAt.Before(now.Add(-time.Minute)) ||
		!running.ExpiresAt.After(now.Add(time.Hour)) {
		t.Fatalf("the fixtures are not what they are meant to be: a request due at %s, and one approved at %v and due at %s",
			notDue.ExpiresAt, running.DecidedAt, running.ExpiresAt)
	}

	// paged reads everything a read answers, a page at a time, each page in a
	// transaction of its own, the cursor at the last request of the page before.
	paged := func(ctx context.Context, list sweepRead, size int) (pages [][]uuid.UUID) {
		t.Helper()
		var cursor repocontracts.SweepCursor
		for range 20 {
			var page []entities.DeviationRequest
			err := h.repo.UnitOfWork().Do(ctx, func(tx context.Context) error {
				var err error
				page, err = list(tx, now, cursor, size)
				return err
			})
			if err != nil {
				t.Fatalf("read a page of %d after %+v: %v", size, cursor, err)
			}
			if len(page) == 0 {
				return pages
			}
			pages = append(pages, requestIDs(page))
			cursor = repocontracts.SweepCursorAt(page[len(page)-1])
		}
		t.Fatalf("a read of pages of %d never ended: %v", size, pages)
		return nil
	}
	flat := func(pages [][]uuid.UUID) []uuid.UUID { return slices.Concat(pages...) }

	for size, wantPages := range map[int]int{1: 5, 2: 3, 5: 1, 100: 1} {
		pages := paged(system, requests.ListOverdue, size)
		if got := flat(pages); !slices.Equal(got, overdue) || len(pages) != wantPages {
			t.Errorf("overdue, in pages of %d: %d page(s) %v; want %d page(s) holding %v, each request once and in order", size, len(pages), pages, wantPages, overdue)
		}
	}
	for size, wantPages := range map[int]int{1: 2, 2: 1} {
		pages := paged(system, requests.ListUnreported, size)
		if got := flat(pages); !slices.Equal(got, unreported) || len(pages) != wantPages {
			t.Errorf("unreported, in pages of %d: %v; want %d page(s) holding %v", size, pages, wantPages, unreported)
		}
	}

	// From a cursor in the middle: strictly what follows it, and from the
	// last request, nothing — nor from a cursor beyond every deadline.
	after := func(cursor repocontracts.SweepCursor) []uuid.UUID {
		t.Helper()
		var page []entities.DeviationRequest
		err := h.repo.UnitOfWork().Do(system, func(tx context.Context) error {
			var err error
			page, err = requests.ListOverdue(tx, now, cursor, 100)
			return err
		})
		if err != nil {
			t.Fatalf("read after %+v: %v", cursor, err)
		}
		return requestIDs(page)
	}
	deadline := base.Add(2 * time.Minute)
	if got := after(repocontracts.SweepCursor{ExpiresAt: deadline, ID: overdue[2]}); !slices.Equal(got, overdue[3:]) {
		t.Errorf("after the first of two with one deadline: %v, want its twin and what follows, %v", got, overdue[3:])
	}
	if got := after(repocontracts.SweepCursor{ExpiresAt: deadline, ID: overdue[3]}); !slices.Equal(got, overdue[4:]) {
		t.Errorf("after the second of two with one deadline: %v, want %v", got, overdue[4:])
	}
	if got := after(repocontracts.SweepCursor{ExpiresAt: base.Add(3 * time.Minute), ID: overdue[4]}); len(got) != 0 {
		t.Errorf("after the last request: %v, want nothing", got)
	}
	if got := after(repocontracts.SweepCursor{ExpiresAt: now.Add(time.Hour), ID: uuid.Nil}); len(got) != 0 {
		t.Errorf("after a moment later than every deadline: %v, want nothing", got)
	}

	// The same of the other read, from the last request whose run never
	// reported: the approved request whose window is open stands after it,
	// and is not answered.
	var beyond []entities.DeviationRequest
	if err := h.repo.UnitOfWork().Do(system, func(tx context.Context) error {
		var err error
		beyond, err = requests.ListUnreported(tx, now, repocontracts.SweepCursor{ExpiresAt: base.Add(11 * time.Minute), ID: unreported[1]}, 100)
		return err
	}); err != nil || len(beyond) != 0 {
		t.Errorf("after the last request whose run never reported: %v (%v), want nothing — the one whose window is open is not due", requestIDs(beyond), err)
	}

	// A row somebody holds is not answered; the pass reads the rest.
	held := h.holdOpen(t, `SELECT id FROM deviation_requests WHERE id = ? FOR UPDATE`, overdue[1])
	want := []uuid.UUID{overdue[0], overdue[2], overdue[3], overdue[4]}
	if got := flat(paged(system, requests.ListOverdue, 1)); !slices.Equal(got, want) {
		t.Errorf("with the second request held, a pass read %v; want the other four, %v", got, want)
	}
	held.undo()

	// Another organization pages through its own requests, which are none.
	other := h.inAnotherOrganization(t, "The Other Organization")
	for name, list := range map[string]sweepRead{"overdue": requests.ListOverdue, "unreported": requests.ListUnreported} {
		if pages := paged(other.tenantContext(), list, 2); len(pages) != 0 {
			t.Errorf("another organization paging through what is %s: %v, want nothing", name, pages)
		}
	}
	if got := flat(paged(h.tenantContext(), requests.ListOverdue, 2)); !slices.Equal(got, overdue) {
		t.Errorf("the organization's own sweep, paged: %v, want %v", got, overdue)
	}
	if got := flat(paged(h.tenantContext(), requests.ListUnreported, 1)); !slices.Equal(got, unreported) {
		t.Errorf("the organization's own sweep of what never reported, paged: %v, want %v", got, unreported)
	}

	// And the sweep itself, over all of it, closes neither. (What it answers
	// is not looked at: the requests above were written through the
	// repository and have no ledger row to close with them.)
	_, _ = h.svc.SweepDeviationRequests(system, now)
	if got := h.requestStatus(t, notDue.ID.String()); got != string(entities.DeviationRequestPending) {
		t.Errorf("after a sweep the request that is not yet due is stored as %s, want it still waiting", got)
	}
	if got := h.requestStatus(t, running.ID.String()); got != string(entities.DeviationRequestApproved) {
		t.Errorf("after a sweep the approved request whose window is open is stored as %s, want it still approved", got)
	}
	if got := h.requestStatus(t, unreported[0].String()); got != string(entities.DeviationRequestInterrupted) {
		t.Errorf("after a sweep the approved request whose run never reported is stored as %s, want interrupted — the sweep did run", got)
	}
}

// A request is read, and held, by whoever is not about to act on what it
// asked — whatever has become of what it stored. GetReadable answers each
// sealed document that opens and leaves one that does not absent, where Get
// fails; for a request that is whole the two agree. The locking read that
// opens no document at all holds the row exactly as GetForUpdate does, in the
// same scope, and answers a damaged request as it answers a whole one.
// GetForUpdate itself still fails on a damaged request: an approval must
// never be handed an empty plan for one that could not be read.
func TestARequestIsReadAndHeldWithoutTheDocumentsThatNoLongerOpen(t *testing.T) {
	h := newDeviationHarness(t)
	requests := h.repo.DeviationRequest()
	instanceID := h.startOneStep(t, entities.Node{Name: "Approve", Type: entities.UserTask, Assignee: "alice"})
	whole := h.mustCreateRequest(t, h.sampleRequest(instanceID, "dv1-readable-whole"))
	other := h.startOneStep(t, entities.Node{Name: "Approve", Type: entities.UserTask, Assignee: "alice"})
	damaged := h.mustCreateRequest(t, h.sampleRequest(other, "dv1-readable-damaged"))
	h.breakThePlanOf(t, damaged.ID)

	read, err := requests.GetReadable(h.tenantContext(), whole.ID)
	strict, strictErr := requests.Get(h.tenantContext(), whole.ID)
	if err != nil || strictErr != nil || read.Plan == nil || read.Command == nil || read.ApprovedInstances == nil {
		t.Fatalf("a whole request, read leniently: %+v (%v); want what Get answers, %+v (%v)", read, err, strict, strictErr)
	}
	sameRequest(t, "a whole request read leniently, against Get's answer", read, strict)
	read, err = requests.GetReadable(h.tenantContext(), damaged.ID)
	if err != nil || read.ID != damaged.ID || read.Status != entities.DeviationRequestPending || read.RequestedBy != damaged.RequestedBy ||
		!read.ExpiresAt.Equal(damaged.ExpiresAt) || read.Fingerprint != damaged.Fingerprint || read.Instance == nil {
		t.Fatalf("a request whose plan no longer opens, read leniently: %+v, %v; want the request", read, err)
	}
	if read.Plan != nil || read.Command == nil || read.ApprovedInstances == nil {
		t.Fatalf("it reads with plan %v, command %v, instances %v; want the plan absent and the two that open present", read.Plan, read.Command, read.ApprovedInstances)
	}
	elsewhere := h.inAnotherOrganization(t, "Elsewhere")
	for name, id := range map[string]uuid.UUID{"another organization's": damaged.ID, "nobody's": uuid.Must(uuid.NewV7())} {
		ctx := h.tenantContext()
		if name == "another organization's" {
			ctx = elsewhere.tenantContext()
		}
		if _, err := requests.GetReadable(ctx, id); !errors.Is(err, apierr.ErrNotFound) {
			t.Errorf("reading %s request leniently: %v, want not found", name, err)
		}
		err := h.repo.UnitOfWork().Do(ctx, func(tx context.Context) error {
			_, err := requests.GetForUpdateWithoutDocuments(tx, id)
			return err
		})
		if !errors.Is(err, apierr.ErrNotFound) {
			t.Errorf("holding %s request: %v, want not found", name, err)
		}
	}

	if _, err := requests.GetForUpdateWithoutDocuments(h.tenantContext(), whole.ID); !errors.Is(err, repocontracts.ErrDeviationRequestOutsideTransaction) {
		t.Fatalf("holding a request outside a transaction: %v, want it refused", err)
	}
	free := func(id uuid.UUID) error {
		return h.db.Transaction(func(tx *gorm.DB) error {
			return tx.Exec(`SELECT id FROM deviation_requests WHERE id = ? FOR UPDATE NOWAIT`, id).Error
		})
	}
	for name, written := range map[string]entities.DeviationRequest{"whole": whole, "damaged": damaged} {
		err := h.repo.UnitOfWork().Do(h.tenantContext(), func(tx context.Context) error {
			held, err := requests.GetForUpdateWithoutDocuments(tx, written.ID)
			if err != nil {
				return err
			}
			if held.ID != written.ID || held.Status != entities.DeviationRequestPending || held.RequestedByID != written.RequestedByID ||
				held.Instance == nil || held.Fingerprint != written.Fingerprint || !held.ExpiresAt.Equal(written.ExpiresAt) {
				t.Errorf("the %s request, held: %+v; want what a closing needs of it", name, held)
			}
			if held.Command != nil || held.Plan != nil || held.ApprovedInstances != nil {
				t.Errorf("the %s request, held without its documents, carries one: %+v", name, held)
			}
			if err := free(written.ID); err == nil {
				t.Errorf("the %s request's row is free while the read that holds it is open", name)
			}
			return nil
		})
		if err != nil {
			t.Fatalf("hold the %s request without its documents: %v", name, err)
		}
		if err := free(written.ID); err != nil {
			t.Errorf("the %s request's row is still held after the transaction ended: %v", name, err)
		}
	}
	err = h.repo.UnitOfWork().Do(h.tenantContext(), func(tx context.Context) error {
		_, err := requests.GetForUpdate(tx, damaged.ID)
		return err
	})
	if !isTheWritersMistake(err) {
		t.Fatalf("the read an approval makes, of a request whose plan no longer opens: %v; want it to fail", err)
	}
}

// A sweep bounds how long its transaction waits for a row somebody else
// holds. Past the bound the statement that waited fails — here the decision
// of a ledger row another transaction holds — where without it the sweep
// would stand there for as long as the other did. The bound is the
// transaction's own, and is refused outside one, as is a wait of nothing.
func TestASweepsWaitForALockIsBounded(t *testing.T) {
	h := newDeviationHarness(t)
	requests := h.repo.DeviationRequest()
	instanceID := h.startOneStep(t, entities.Node{Name: "Approve", Type: entities.UserTask, Assignee: "alice"})
	row := h.mustWrite(t, h.pendingWaive(instanceID, "dv1-bounded-wait"))

	if err := requests.BoundLockWait(h.tenantContext(), time.Second); !errors.Is(err, repocontracts.ErrDeviationRequestOutsideTransaction) {
		t.Fatalf("bounding a wait outside a transaction: %v, want it refused", err)
	}
	for _, wait := range []time.Duration{0, -time.Second, 500 * time.Microsecond} {
		err := h.repo.UnitOfWork().Do(h.tenantContext(), func(tx context.Context) error { return requests.BoundLockWait(tx, wait) })
		if !isTheWritersMistake(err) {
			t.Errorf("a wait of %s: %v, want a plain server error", wait, err)
		}
	}

	decide := func(wait time.Duration) (time.Duration, error) {
		began := time.Now()
		// Bounded by the test as well: a wait the repository failed to bound
		// ends here, as a failure, instead of holding the test.
		inTime, stop := context.WithTimeout(h.tenantContext(), raceWait)
		defer stop()
		err := h.repo.UnitOfWork().Do(inTime, func(tx context.Context) error {
			if err := requests.BoundLockWait(tx, wait); err != nil {
				return err
			}
			_, err := h.repo.DeviationDecider().Decide(tx, row.ID, repocontracts.LedgerRowDecision{Status: entities.DeviationExpired, DecidedAt: time.Now()})
			return err
		})
		return time.Since(began), err
	}
	held := h.holdOpen(t, `UPDATE instance_deviations SET status = status WHERE id = ?`, row.ID)
	took, err := decide(300 * time.Millisecond)
	if !errors.Is(err, stormruntime.ErrLockNotAvailable) || errors.Is(err, repocontracts.ErrDeviationRowDecided) {
		t.Fatalf("deciding a row somebody holds, with a bounded wait: %v; want the statement to give up on the lock", err)
	}
	if took < 250*time.Millisecond || took > raceWait/2 {
		t.Fatalf("it gave up after %s; want about the 300ms it was given", took)
	}
	if live := h.liveVisitKeyOf(t, row.ID); !live.Valid {
		t.Fatal("the decision that gave up was written all the same")
	}
	held.undo()
	// The bound was that transaction's: with the row free, the next one — bound
	// or not — decides it.
	if _, err := decide(300 * time.Millisecond); err != nil {
		t.Fatalf("deciding the row once it was let go: %v", err)
	}
}
