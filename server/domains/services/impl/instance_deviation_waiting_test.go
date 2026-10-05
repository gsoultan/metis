package impl

import (
	"context"
	"errors"
	"testing"

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
// somebody who holds the request and needs no instance. The row is then read
// again: what decided the request moved the row with it.
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
		Plan: map[string]any{"because": []any{"why"}}}
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

	// Decided between the two reads: the row is read again and is gone, so
	// the visit is free and the apply goes on.
	decided := request
	decided.Status = entities.DeviationRequestExpired
	rows = &visitsRead{rows: []*entities.Deviation{&waiting, nil}}
	out, found, err = service(rows, requestsRead{request: decided}).replay(as("ana", ana), command)
	if err != nil || found || out.Deviation != nil || rows.reads != 2 {
		t.Fatalf("a request decided between the reads: %+v, %v, %v (reads %d); want no record of the visit, read twice", out, found, err, rows.reads)
	}
	// Still waiting on a request that is over: the ledger and the request
	// disagree, which is the server's to explain — after two reads, no more.
	rows = &visitsRead{rows: []*entities.Deviation{&waiting}}
	_, found, err = service(rows, requestsRead{request: decided}).replay(as("ana", ana), command)
	if err == nil || found || rows.reads != 2 || errors.Is(err, apierr.ErrInvalidArgument) || errors.Is(err, apierr.ErrNotFound) {
		t.Fatalf("a row that waits on a request that is over: %v, %v (reads %d); want a plain error after two reads", found, err, rows.reads)
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
