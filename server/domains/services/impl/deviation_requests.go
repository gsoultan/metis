package impl

import (
	"context"
	"time"

	"github.com/google/uuid"

	"github.com/gsoultan/metis/internal/pkg/apierr"
	"github.com/gsoultan/metis/server/domains/entities"
	servicecontracts "github.com/gsoultan/metis/server/domains/services/contracts"
	"github.com/gsoultan/metis/server/repositories"
)

// deviationRequestService reads and decides the requests that wait for a
// second administrator.
//
// A request is made by what asks for it — the in-place command, for a waive —
// and carried out by what would have carried it out had nobody else been
// needed. So approving a waive is the in-place service's own apply, reached
// through waives: this service finds the request and hands it over.
type deviationRequestService struct {
	repo repositories.Repository
	// waives approves a request for a waive: the in-place service, built here
	// over the same repository and engine so that an approval runs the code
	// an apply ran.
	waives *instanceDeviationService
	// rules says who may decide a request.
	rules approvalRules
	// sweepLockWait is how long the sweep's closing of one request waits for
	// a row somebody else holds (defaultSweepLockWait unless an option says
	// otherwise).
	sweepLockWait time.Duration
	// sweepBudget is how many closings one pass of the sweep attempts
	// (defaultSweepBudget).
	sweepBudget int
}

// DeviationRequestOption changes how the service of requests is built.
type DeviationRequestOption func(*deviationRequestService)

// WithSweepLockWait sets how long the sweep's closing of one request waits
// for a row somebody else holds before it leaves that request for the next
// pass. The server uses the default; a test that stops a sweep on a row it
// holds itself, to put two decisions in an order, gives it longer than the
// test will need.
func WithSweepLockWait(wait time.Duration) DeviationRequestOption {
	return func(s *deviationRequestService) { s.sweepLockWait = wait }
}

// NewDeviationRequestService builds the service of requests for a second
// administrator over a repository and the engine that runs the instances a
// waive acts on.
func NewDeviationRequestService(repo repositories.Repository, engine servicecontracts.ExecutionEngine, options ...DeviationRequestOption) servicecontracts.DeviationRequestService {
	service := &deviationRequestService{repo: repo, waives: newInstanceDeviationService(repo, engine), rules: approvalRules{repo: repo},
		sweepLockWait: defaultSweepLockWait, sweepBudget: defaultSweepBudget}
	for _, option := range options {
		option(service)
	}
	return service
}

// ApproveDeviationRequest approves a request and carries out what it asked
// for.
//
// The caller is asked for first, and nothing is read for anybody who may not
// decide. The request is then read inside the caller's organization — another
// organization's is not found, as one that never existed — only to learn what
// kind it is; whether it still waits, who may approve it and whether it still
// holds are asked again of the row the approval locks — and who is approving
// is asked again by the approval itself.
func (s *deviationRequestService) ApproveDeviationRequest(ctx context.Context, id uuid.UUID, reason string) (entities.DeviationRequestOutcome, error) {
	var none entities.DeviationRequestOutcome
	if _, err := requireDecidingAdministrator(ctx); err != nil {
		return none, err
	}
	requests := s.repo.DeviationRequest()
	if requests == nil {
		return none, errNoDeviationRequests
	}
	request, err := requests.Get(ctx, id)
	if err != nil {
		return none, err
	}
	switch request.Kind {
	case entities.DeviationRequestInstanceWaive:
		return s.waives.approveWaive(ctx, id, reason)
	case entities.DeviationRequestMigration:
		return none, apierr.Invalidf("approving a migration arrives with the migration's second approver")
	}
	return none, apierr.Invalidf("request %s is of a kind nothing here approves", id)
}

// ListDeviationRequests answers one page of the organization's requests,
// newest first, each with the status it has now.
//
// With no status named it lists what still waits: the queue is what somebody
// can act on. The repository filters by what a request reads as at this
// moment, so one past its deadline is listed as expired and not as waiting,
// whether or not anything has written that down — and each row is answered
// with that same reading, never with a stored status the filter contradicts.
func (s *deviationRequestService) ListDeviationRequests(ctx context.Context, query entities.DeviationRequestQuery) ([]entities.DeviationRequest, int64, error) {
	if _, err := requireDecidingAdministrator(ctx); err != nil {
		return nil, 0, err
	}
	requests := s.repo.DeviationRequest()
	if requests == nil {
		return nil, 0, errNoDeviationRequests
	}
	if query.Status == "" {
		query.Status = entities.DeviationRequestPending
	}
	if !query.Status.Valid() {
		return nil, 0, apierr.Invalidf("status must be one of pending_approval, approved, applied, interrupted, stale, rejected, expired")
	}
	now := time.Now()
	page, total, err := requests.List(ctx, query, now)
	if err != nil {
		return nil, 0, err
	}
	for i := range page {
		page[i].Status = page[i].EffectiveStatus(now)
	}
	return page, total, nil
}

// GetDeviationRequest answers one request, whole, with the status it has now.
// Another organization's request is not found.
//
// Whole, when what it stored still opens. A request whose sealed command,
// plan or list of instances no longer opens is answered all the same, with
// that document absent (nil, never empty): it still waits, or was decided,
// and somebody has to be able to see it to reject it. Only an approval needs
// every document, and reads them itself.
func (s *deviationRequestService) GetDeviationRequest(ctx context.Context, id uuid.UUID) (entities.DeviationRequest, error) {
	if _, err := requireDecidingAdministrator(ctx); err != nil {
		return entities.DeviationRequest{}, err
	}
	requests := s.repo.DeviationRequest()
	if requests == nil {
		return entities.DeviationRequest{}, errNoDeviationRequests
	}
	request, err := requests.GetReadable(ctx, id)
	if err != nil {
		return entities.DeviationRequest{}, err
	}
	request.Status = request.EffectiveStatus(time.Now())
	return request, nil
}
