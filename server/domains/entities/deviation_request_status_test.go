package entities

import (
	"encoding/json"
	"slices"
	"testing"
	"time"

	"github.com/google/uuid"
)

// A request's status is the record of who decided what. The set is closed so
// a typo cannot become a state nobody reads back, and which ones are still
// "live" is what frees a visit for a new request (migration 34).
func TestDeviationRequestStatusesAndKindsAreClosedSets(t *testing.T) {
	t.Parallel()
	live := map[DeviationRequestStatus]bool{
		DeviationRequestPending: true, DeviationRequestApproved: true,
		DeviationRequestApplied: false, DeviationRequestInterrupted: false, DeviationRequestStale: false,
		DeviationRequestRejected: false, DeviationRequestExpired: false,
	}
	for status, wantLive := range live {
		if !status.Valid() || status.Live() != wantLive || status.Terminal() == wantLive {
			t.Errorf("%q: Valid %v Live %v Terminal %v", status, status.Valid(), status.Live(), status.Terminal())
		}
	}
	for _, bad := range []DeviationRequestStatus{"", "done"} {
		if bad.Valid() || bad.Live() || bad.Terminal() {
			t.Errorf("%q is accepted", bad)
		}
	}
	if !DeviationRequestInstanceWaive.Valid() || !DeviationRequestMigration.Valid() || DeviationRequestKind("skip").Valid() {
		t.Error("the request kinds are not the closed set {instance_waive, migration}")
	}
}

// Expiry is decided by the clock, not by the sweep: a pending request past its
// deadline reads as expired whether or not the sweep has reached it.
func TestAPendingRequestPastItsDeadlineReadsAsExpired(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 10, 3, 12, 0, 0, 0, time.UTC)
	r := DeviationRequest{Status: DeviationRequestPending, ExpiresAt: now}
	if got := r.EffectiveStatus(now); got != DeviationRequestExpired {
		t.Fatalf("at its deadline a pending request reads %q", got)
	}
	if got := r.EffectiveStatus(now.Add(-time.Second)); got != DeviationRequestPending {
		t.Fatalf("before its deadline it reads %q", got)
	}
	r.Status = DeviationRequestRejected
	if got := r.EffectiveStatus(now.Add(time.Hour)); got != DeviationRequestRejected {
		t.Fatalf("a decided request reads %q after its deadline", got)
	}
}

// "Approved" is never where a request rests (rulings §14): a run that ends
// reports, and one whose process died leaves the request approved. Past its
// deadline, or an hour after the approval, whichever comes first, such a
// request is out of use and reads as what it is before any sweep records it.
func TestAnApprovedRequestLeftBehindReadsAsInterrupted(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 10, 3, 12, 0, 0, 0, time.UTC)
	decided := now.Add(-10 * time.Minute)
	r := DeviationRequest{Status: DeviationRequestApproved, DecidedAt: &decided, ExpiresAt: now.Add(time.Hour)}
	if r.RunWindowClosed(now) || r.EffectiveStatus(now) != DeviationRequestApproved {
		t.Fatalf("ten minutes into its run it reads %q", r.EffectiveStatus(now))
	}
	if after := decided.Add(ApprovedRunReportWindow); !r.RunWindowClosed(after) || r.EffectiveStatus(after) != DeviationRequestInterrupted {
		t.Fatalf("an hour after the approval it reads %q", r.EffectiveStatus(after))
	}
	r.ExpiresAt = now
	if !r.RunWindowClosed(now) || r.EffectiveStatus(now) != DeviationRequestInterrupted {
		t.Fatalf("at its deadline, ten minutes after the approval, it reads %q", r.EffectiveStatus(now))
	}
	for _, status := range []DeviationRequestStatus{DeviationRequestPending, DeviationRequestApplied, DeviationRequestInterrupted} {
		if (DeviationRequest{Status: status, DecidedAt: &decided, ExpiresAt: now}).RunWindowClosed(now.Add(48 * time.Hour)) {
			t.Errorf("a %s request has a run window", status)
		}
	}
}

// An approval with no time of approval cannot be bounded by "an hour after the
// approval", and absent constraint means deny: such a request is out of use at
// once, whatever its deadline says, and reads as interrupted. Nothing in the
// product writes one; a row a bug wrote must not be runnable for the whole
// approval window.
func TestAnApprovedRequestWithNoTimeOfApprovalIsOutOfUseAtOnce(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 10, 3, 12, 0, 0, 0, time.UTC)
	for name, expires := range map[string]time.Time{
		"a deadline three days off": now.Add(72 * time.Hour),
		"a deadline already passed": now.Add(-time.Minute),
		"no deadline either":        {},
	} {
		r := DeviationRequest{Status: DeviationRequestApproved, ExpiresAt: expires}
		if !r.RunWindowClosed(now) {
			t.Errorf("%s: an approved request with no time of approval is still in use", name)
		}
		if got := r.EffectiveStatus(now); got != DeviationRequestInterrupted {
			t.Errorf("%s: it reads %q, want interrupted", name, got)
		}
	}
	// Only an approved request: a pending one has not been decided yet, and
	// its lack of a decision time says nothing.
	pending := DeviationRequest{Status: DeviationRequestPending, ExpiresAt: now.Add(72 * time.Hour)}
	if pending.RunWindowClosed(now) || pending.EffectiveStatus(now) != DeviationRequestPending {
		t.Errorf("a pending request with no decision time reads %q", pending.EffectiveStatus(now))
	}
}

// "No second person approved it" is a fact derived from account ids, so a
// renamed account cannot hide it.
func TestASelfApprovalIsTheRequesterDecidingAnApproval(t *testing.T) {
	t.Parallel()
	ana := uuid.Must(uuid.NewV7())
	r := DeviationRequest{RequestedByID: ana, DecidedByID: ana, Status: DeviationRequestApplied}
	if !r.SelfApproved() {
		t.Fatal("the requester's own approval is not a self-approval")
	}
	r.Status = DeviationRequestRejected
	if r.SelfApproved() {
		t.Fatal("the requester withdrawing their own request reads as a self-approval")
	}
	r.Status, r.DecidedByID = DeviationRequestApplied, uuid.Must(uuid.NewV7())
	if r.SelfApproved() {
		t.Fatal("a second administrator's approval reads as a self-approval")
	}
	r.Plan = map[string]any{"because": []any{"“Operations approve” would be waived"}}
	if got := r.Because(); len(got) != 1 || got[0] != "“Operations approve” would be waived" {
		t.Fatalf("Because() = %v", got)
	}
}

// The reasons a request needs a second administrator are stored in its plan as
// a list of sentences. Anything else there is not a list of sentences, and a
// reader is given none rather than half of one.
func TestBecauseReadsAListOfSentencesAndNothingElse(t *testing.T) {
	t.Parallel()
	for name, tc := range map[string]struct {
		plan map[string]any
		want []string
	}{
		"as decoded from JSON":     {map[string]any{"because": []any{"a", "b"}}, []string{"a", "b"}},
		"as built in memory":       {map[string]any{"because": []string{"a"}}, []string{"a"}},
		"no plan":                  {nil, nil},
		"no reasons in the plan":   {map[string]any{"kind": "waive"}, nil},
		"a sentence, not a list":   {map[string]any{"because": "a"}, nil},
		"a list of other things":   {map[string]any{"because": []any{"a", 2}}, nil},
		"an empty list, from JSON": {map[string]any{"because": []any{}}, []string{}},
	} {
		got := DeviationRequest{Plan: tc.plan}.Because()
		if (got == nil) != (tc.want == nil) || !slices.Equal(got, tc.want) {
			t.Errorf("%s: Because() = %#v, want %#v", name, got, tc.want)
		}
	}
}

// What a route answers with while a request waits. Its list of reasons is a
// list even when there is nothing in it: the routes write every list as a
// list, never as null.
func TestAPendingApprovalSaysWhoAskedUntilWhenAndAlwaysCarriesAList(t *testing.T) {
	t.Parallel()
	id := uuid.Must(uuid.NewV7())
	deadline := time.Date(2026, 10, 6, 12, 0, 0, 0, time.UTC)
	got := PendingApprovalOf(DeviationRequest{
		ID: id, Status: DeviationRequestPending, RequestedBy: "ana", ExpiresAt: deadline,
		Plan: map[string]any{"because": []any{"“Operations approve” would be waived"}},
	})
	if got.RequestID != id || got.Status != DeviationRequestPending || got.RequestedBy != "ana" || !got.ExpiresAt.Equal(deadline) ||
		len(got.Because) != 1 || got.Because[0] != "“Operations approve” would be waived" {
		t.Fatalf("PendingApprovalOf = %+v", got)
	}

	bare := PendingApprovalOf(DeviationRequest{ID: id, Status: DeviationRequestPending, ExpiresAt: deadline})
	if bare.Because == nil {
		t.Fatal("a request with no reasons stored answers a nil list, which a route would write as null")
	}
	body, err := json.Marshal(bare)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	want := `{"request_id":"` + id.String() + `","status":"pending_approval","requested_by":"","expires_at":"2026-10-06T12:00:00Z","because":[]}`
	if string(body) != want {
		t.Fatalf("a pending approval is written as\n  %s\nwant\n  %s", body, want)
	}
}

// A self-approval is told from who decided an approval, at every status an
// approval leaves a request in, and never from a decision nobody is named for.
func TestASelfApprovalIsReadAtEveryStatusAnApprovalLeaves(t *testing.T) {
	t.Parallel()
	ana := uuid.Must(uuid.NewV7())
	for status, want := range map[DeviationRequestStatus]bool{
		DeviationRequestApproved: true, DeviationRequestApplied: true, DeviationRequestInterrupted: true,
		DeviationRequestPending: false, DeviationRequestRejected: false, DeviationRequestExpired: false, DeviationRequestStale: false,
	} {
		if got := (DeviationRequest{RequestedByID: ana, DecidedByID: ana, Status: status}).SelfApproved(); got != want {
			t.Errorf("%s decided by its requester: SelfApproved() = %v, want %v", status, got, want)
		}
	}
	if (DeviationRequest{Status: DeviationRequestApplied}).SelfApproved() {
		t.Error("a request that names neither a requester nor a decider reads as a self-approval")
	}
}

// A clock that goes backwards — a server whose time was corrected, or one
// replica behind another — is not guarded against, and this pins what it
// does rather than changing it: both windows are asked of the moment given,
// so read at an earlier moment a request waits, or runs, for longer by
// exactly the step back, and never past its deadline. Nothing a request
// stores is rewritten by a reading, so a later reading at the right time
// says what it should.
func TestAClockThatGoesBackLengthensBothWindowsByTheStepAndNeverPastTheDeadline(t *testing.T) {
	t.Parallel()
	asked := time.Date(2026, 10, 1, 9, 0, 0, 0, time.UTC)
	deadline := asked.Add(72 * time.Hour)
	waiting := DeviationRequest{Status: DeviationRequestPending, ExpiresAt: deadline}
	if got := waiting.EffectiveStatus(deadline); got != DeviationRequestExpired {
		t.Fatalf("at its deadline a waiting request reads %s, want expired", got)
	}
	// The clock is put back an hour: the request waits again, until the
	// deadline comes round on that clock — and not a moment after it.
	if got := waiting.EffectiveStatus(deadline.Add(-time.Hour)); got != DeviationRequestPending {
		t.Fatalf("read an hour before its deadline, after having read as expired, it reads %s; want it waiting again", got)
	}
	if got := waiting.EffectiveStatus(deadline.Add(time.Nanosecond)); got != DeviationRequestExpired {
		t.Fatalf("past its deadline on any clock it reads %s, want expired", got)
	}

	approvedAt := asked.Add(time.Hour)
	running := DeviationRequest{Status: DeviationRequestApproved, ExpiresAt: deadline, DecidedAt: &approvedAt}
	closes := approvedAt.Add(ApprovedRunReportWindow)
	if !running.RunWindowClosed(closes) || running.RunWindowClosed(closes.Add(-time.Minute)) {
		t.Fatalf("the run window is closed at %v and a minute before it: %v, %v; want closed, then open again on a clock put back",
			closes, running.RunWindowClosed(closes), running.RunWindowClosed(closes.Add(-time.Minute)))
	}
	// A reading from before the approval itself: the window is open, as it is
	// at the approval — the step back is all it gains.
	if running.RunWindowClosed(approvedAt.Add(-24 * time.Hour)) {
		t.Fatal("read a day before it was approved, an approved request's run window is closed")
	}
	// Approved near its deadline, the deadline closes the window first, on
	// whatever clock: a run is never given longer than the request had.
	late := deadline.Add(-10 * time.Minute)
	nearly := DeviationRequest{Status: DeviationRequestApproved, ExpiresAt: deadline, DecidedAt: &late}
	if !nearly.RunWindowClosed(deadline) || nearly.RunWindowClosed(deadline.Add(-time.Second)) {
		t.Fatal("a request approved ten minutes before its deadline does not close its run window at the deadline")
	}
}

// A deadline that was never set is no deadline to wait for: the zero time is
// before every moment, so a request that waits with one reads expired, and an
// approved one — whenever it was approved — is out of use. Nothing in the
// product writes one; absent constraint means deny.
func TestARequestWithNoDeadlineIsOverAtOnce(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 10, 1, 9, 0, 0, 0, time.UTC)
	if got := (DeviationRequest{Status: DeviationRequestPending}).EffectiveStatus(now); got != DeviationRequestExpired {
		t.Fatalf("a waiting request with no deadline reads %s, want expired", got)
	}
	justNow := now.Add(-time.Second)
	approved := DeviationRequest{Status: DeviationRequestApproved, DecidedAt: &justNow}
	if !approved.RunWindowClosed(now) || approved.EffectiveStatus(now) != DeviationRequestInterrupted {
		t.Fatalf("a request approved a second ago with no deadline: window closed = %v, reads %s; want it out of use and interrupted",
			approved.RunWindowClosed(now), approved.EffectiveStatus(now))
	}
	// A decided request is as stored, deadline or none.
	for _, status := range []DeviationRequestStatus{DeviationRequestApplied, DeviationRequestRejected, DeviationRequestStale, DeviationRequestExpired, DeviationRequestInterrupted} {
		if got := (DeviationRequest{Status: status}).EffectiveStatus(now); got != status {
			t.Errorf("a request stored as %s with no deadline reads %s", status, got)
		}
	}
}
