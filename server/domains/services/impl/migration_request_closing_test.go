package impl

import (
	"context"
	"fmt"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/gsoultan/metis/server/domains/entities"
	"github.com/gsoultan/metis/server/repositories"
	repocontracts "github.com/gsoultan/metis/server/repositories/contracts"
)

// passesRecorded is a store of requests that answers nothing to either of
// the sweep's reads, and keeps the order they were made in.
type passesRecorded struct {
	repocontracts.DeviationRequestRepository
	reads []string
}

func (p *passesRecorded) BoundLockWait(context.Context, time.Duration) error { return nil }

func (p *passesRecorded) ListOverdue(context.Context, time.Time, repocontracts.SweepCursor, int) ([]entities.DeviationRequest, error) {
	p.reads = append(p.reads, "past their deadline")
	return nil, nil
}

func (p *passesRecorded) ListUnreported(context.Context, time.Time, repocontracts.SweepCursor, int) ([]entities.DeviationRequest, error) {
	p.reads = append(p.reads, "approved and unreported")
	return nil, nil
}

// passesStore is a repository whose requests are a passesRecorded, with a
// unit of work that commits and a decider that is there.
type passesStore struct {
	repositories.Repository
	requests *passesRecorded
}

func (s passesStore) UnitOfWork() repocontracts.UnitOfWork { return &uowThatCommits{} }

func (s passesStore) DeviationRequest() repocontracts.DeviationRequestRepository { return s.requests }

func (s passesStore) DeviationDecider() repocontracts.DeviationDecider { return decidesNothing{} }

type decidesNothing struct{ repocontracts.DeviationDecider }

// The sweep makes two passes, and the one over approved requests whose run
// never reported comes first: there are few of them, each holds a migration
// nobody can ask for again until it is closed, and a long pass over overdue
// requests must not stand in front of that.
func TestTheSweepClosesUnreportedRunsBeforeOverdueRequests(t *testing.T) {
	requests := &passesRecorded{}
	service := &deviationRequestService{repo: passesStore{requests: requests}, sweepLockWait: time.Second, sweepBudget: 10}
	closed, err := service.ExpireDeviationRequests(entities.WithSystemContext(context.Background()), time.Now())
	if err != nil || closed != 0 {
		t.Fatalf("a sweep with nothing to close: %d, %v", closed, err)
	}
	if want := []string{"approved and unreported", "past their deadline"}; !slices.Equal(requests.reads, want) {
		t.Fatalf("the sweep read %v, want %v", requests.reads, want)
	}
}

// What a pass over unreported runs left behind is said in words that read as
// a sentence, in the log's line and in the error.
func TestWhatThePassOverUnreportedRunsLeftIsSaidInASentence(t *testing.T) {
	first := errString("request 0199: the row is locked by another transaction")
	if err := tallied(unreportedRuns, sweepTally{closed: 2, failed: 3, first: first}); err == nil ||
		err.Error() != "3 requests whose approved run never reported could not be closed (2 closed); the first: "+string(first) {
		t.Fatalf("three left behind: %v", err)
	}
	if err := tallied(unreportedRuns, sweepTally{failed: 1, first: first}); err == nil ||
		err.Error() != "1 request whose approved run never reported could not be closed (0 closed); the first: "+string(first) {
		t.Fatalf("one left behind: %v", err)
	}
	if err := tallied(unreportedRuns, sweepTally{closed: 4}); err != nil {
		t.Fatalf("a pass that closed everything it read answers %v", err)
	}
}

// errString is an error that is its own words.
type errString string

func (e errString) Error() string { return string(e) }

// The sweep's mark on an approved request says the run did not report, and
// keeps what a self-approval rested on: the mark replaces the outcome, and
// that record has to outlive it.
func TestTheSweepsMarkKeepsWhatASelfApprovalRestedOn(t *testing.T) {
	stored := map[string]any{auditSelfApproved: true, auditOtherAdministrators: float64(0), auditOrganizationID: "01990000-0000-7000-8000-000000000001", "note": "not carried"}
	mark := unreportedOutcome(stored)
	if mark[outcomeError] != runNeverReported || mark[auditSelfApproved] != true || mark[auditOrganizationID] != stored[auditOrganizationID] ||
		mark[auditOtherAdministrators] != float64(0) || len(mark) != 4 {
		t.Fatalf("the mark over a self-approved request: %v", mark)
	}
	if reportedByARun(mark) {
		t.Fatal("the sweep's mark reads as a run's own report")
	}
	if plain := unreportedOutcome(map[string]any{}); len(plain) != 1 || plain[outcomeError] != "the run did not report back" {
		t.Fatalf("the mark over a request somebody else approved: %v", plain)
	}
}

// What a stale request keeps, and what its approver is told, has a size. A
// plan refuses once for each thing wrong with a migration, and a migration
// is as large as its two graphs: the request keeps the first ten refusals
// and a count of the rest — as a plan lists its reasons — and the sentence
// said to the approver is made from those, not from all of them.
func TestAStaleRequestKeepsTenOfThePlansRefusalsAndCountsTheRest(t *testing.T) {
	var refusals []string
	for i := range 25 {
		refusals = append(refusals, fmt.Sprintf("refusal %02d", i))
	}
	why, shown := becausePlanRefuses(refusals)
	if len(shown) != 11 || shown[9] != "refusal 09" || shown[10] != "and 15 more refusal(s), not listed here" {
		t.Fatalf("25 refusals are kept as %d: %v; want the first ten and a count of the rest", len(shown), shown)
	}
	if why != "the plan now refuses it: "+strings.Join(shown, "; ") || strings.Contains(why, "refusal 10") {
		t.Fatalf("the sentence is %q; want it made from the ten that are kept", why)
	}
	why, shown = becausePlanRefuses(refusals[:3])
	if len(shown) != 3 || why != "the plan now refuses it: refusal 00; refusal 01; refusal 02" {
		t.Fatalf("three refusals are kept as %v and said as %q; want all three, as before", shown, why)
	}
}
