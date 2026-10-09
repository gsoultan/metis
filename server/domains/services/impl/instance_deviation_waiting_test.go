package impl

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/gsoultan/metis/internal/pkg/apierr"
	pkgauth "github.com/gsoultan/metis/internal/pkg/auth"
	"github.com/gsoultan/metis/server/domains/entities"
	"github.com/gsoultan/metis/server/repositories"
	repocontracts "github.com/gsoultan/metis/server/repositories/contracts"
)

// visitsRead is a ledger that answers each read of a visit's live row with
// the next of what it was given, and counts the reads.
type visitsRead struct {
	repocontracts.DeviationRepository
	rows  []*entities.Deviation
	reads int
}

func (v *visitsRead) FindLiveByVisit(context.Context, uuid.UUID, string) (entities.Deviation, bool, error) {
	row := v.rows[min(v.reads, len(v.rows)-1)]
	v.reads++
	if row == nil {
		return entities.Deviation{}, false, nil
	}
	return *row, true, nil
}

// requestsRead is a store of requests that answers one, or an error.
type requestsRead struct {
	repocontracts.DeviationRequestRepository
	request entities.DeviationRequest
	err     error
}

func (r requestsRead) Get(context.Context, uuid.UUID) (entities.DeviationRequest, error) {
	return r.request, r.err
}

// ledgerAndRequests is a repository with a ledger and a store of requests
// and nothing else.
type ledgerAndRequests struct {
	repositories.Repository
	rows     repocontracts.DeviationRepository
	requests repocontracts.DeviationRequestRepository
}

func (r ledgerAndRequests) Deviation() repocontracts.DeviationRepository { return r.rows }

func (r ledgerAndRequests) DeviationRequest() repocontracts.DeviationRequestRepository {
	if r.requests == nil {
		return nil
	}
	return r.requests
}

// A visit that has been asked for and waits is answered to whoever asked, and
// to nobody else; and the request it waits on is read, never held — so it can
// be decided between the read of the row and the read of the request, by
// somebody who holds the request and needs no instance. A request found so,
// or found past its deadline, does not hold its visit: the replay says which
// request it is (overdueRequest) and writes nothing, for its caller to let go
// of the instance, close the request under the request's own row, and apply
// once more.
func TestAVisitThatWaitsIsAnsweredFromItsRowAndItsRequest(t *testing.T) {
	ana, budi := uuid.Must(uuid.NewV7()), uuid.Must(uuid.NewV7())
	as := func(name string, id uuid.UUID) context.Context {
		return context.WithValue(context.Background(), pkgauth.UserContextKey, entities.User{ID: id, Username: name})
	}
	waiting := deviationRow(entities.DeviationWaive, entities.DeviationScopeTask, "approve", "the CFO agreed", nil)
	waiting.Status, waiting.RequestID, waiting.VisitKey = entities.DeviationPendingApproval, uuid.Must(uuid.NewV7()), "dv1-k"
	command := entities.DeviationCommand{InstanceID: uuid.Must(uuid.NewV7()), Kind: entities.DeviationWaive, NodeID: "approve",
		Reason: "the CFO agreed", VisitKey: "dv1-k"}
	request := entities.DeviationRequest{ID: waiting.RequestID, Status: entities.DeviationRequestPending, RequestedBy: "ana", RequestedByID: ana,
		Plan: map[string]any{"because": []any{"why"}}, ExpiresAt: time.Now().Add(time.Hour)}
	service := func(rows *visitsRead, requests repocontracts.DeviationRequestRepository) *instanceDeviationService {
		return &instanceDeviationService{repo: ledgerAndRequests{rows: rows, requests: requests}}
	}

	rows := &visitsRead{rows: []*entities.Deviation{&waiting}}
	out, found, err := service(rows, requestsRead{request: request}).replay(as("ana", ana), command)
	if err != nil || !found || out.Applied || !out.Replayed || out.PendingApproval == nil || out.PendingApproval.RequestID != request.ID ||
		!out.Plan.RequiresSecondApprover || out.Deviation == nil || out.Deviation.ID != waiting.ID || rows.reads != 1 {
		t.Fatalf("the requester asking again: %+v, %v, %v (reads %d)", out, found, err, rows.reads)
	}
	for who, ctx := range map[string]context.Context{"another administrator": as("budi", budi), "nobody": context.Background(),
		"somebody with her name and no account": as("ana", uuid.Nil)} {
		out, found, err := service(&visitsRead{rows: []*entities.Deviation{&waiting}}, requestsRead{request: request}).replay(ctx, command)
		if !errors.Is(err, apierr.ErrInvalidArgument) || found || out.PendingApproval != nil || out.Deviation != nil {
			t.Errorf("%s asking for a visit that waits: %+v, %v, %v; want told one is waiting", who, out, found, err)
		}
	}
	other := command
	other.Reason = "another reason"
	if _, found, err := service(&visitsRead{rows: []*entities.Deviation{&waiting}}, requestsRead{request: request}).replay(as("ana", ana), other); !errors.Is(err, apierr.ErrInvalidArgument) || found {
		t.Errorf("the requester asking for something else on the visit: %v, %v", found, err)
	}

	// Past its deadline, or decided between the two reads: it holds nothing.
	// Whoever asks and whatever they ask, the answer is which request to
	// close — never the request as though it waited, never a refusal that says
	// one is waiting, and nothing of the row. The row is read once: reading it
	// again here would be deciding, while holding the instance, what only the
	// holder of the request's row can.
	overdue, decided := request, request
	overdue.ExpiresAt = time.Now().Add(-time.Minute)
	decided.Status = entities.DeviationRequestExpired
	for state, stored := range map[string]entities.DeviationRequest{"past its deadline": overdue, "decided between the reads": decided} {
		for who, ask := range map[string]struct {
			ctx     context.Context
			command entities.DeviationCommand
		}{
			"the requester asking again":         {as("ana", ana), command},
			"the requester asking for something": {as("ana", ana), other},
			"another administrator":              {as("budi", budi), command},
			"nobody":                             {context.Background(), command},
		} {
			rows = &visitsRead{rows: []*entities.Deviation{&waiting}}
			out, found, err := service(rows, requestsRead{request: stored}).replay(ask.ctx, ask.command)
			var closing overdueRequest
			if !errors.As(err, &closing) || closing.id != request.ID || found || out.Deviation != nil || out.PendingApproval != nil || rows.reads != 1 {
				t.Errorf("a request %s, %s: %+v, %v, %v (reads %d); want the request to close, named, and nothing else",
					state, who, out, found, err, rows.reads)
			}
			if errors.Is(err, apierr.ErrInvalidArgument) || errors.Is(err, apierr.ErrNotFound) || errors.Is(err, apierr.ErrForbidden) {
				t.Errorf("a request %s, %s: %v reads as a refusal a caller could be given", state, who, err)
			}
		}
	}

	// The request cannot be read: the instance is there, so it is not "not
	// found", and nothing is answered as though no act had been asked for.
	_, found, err = service(&visitsRead{rows: []*entities.Deviation{&waiting}}, requestsRead{err: apierr.ErrNotFound}).replay(as("ana", ana), command)
	if err == nil || found || errors.Is(err, apierr.ErrNotFound) {
		t.Fatalf("a request that could not be read: %v, %v; want the server's failure", found, err)
	}
	_, found, err = service(&visitsRead{rows: []*entities.Deviation{&waiting}}, nil).replay(as("ana", ana), command)
	if !errors.Is(err, errNoDeviationRequests) || found {
		t.Fatalf("a wiring with no store of requests: %v, %v", found, err)
	}

	// A row that was applied is answered as it always was, and no request is read.
	applied := waiting
	applied.Status = entities.DeviationApplied
	out, found, err = service(&visitsRead{rows: []*entities.Deviation{&applied}}, requestsRead{err: errors.New("not to be read")}).replay(as("budi", budi), command)
	if err != nil || !found || !out.Applied || !out.Replayed || out.PendingApproval != nil || out.Plan.RequiresSecondApprover {
		t.Fatalf("a visit that was waived: %+v, %v, %v", out, found, err)
	}
}
