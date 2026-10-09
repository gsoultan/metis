package impl

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/gsoultan/metis/server/domains/entities"
	"github.com/gsoultan/metis/server/repositories"
	repocontracts "github.com/gsoultan/metis/server/repositories/contracts"
)

// reportStore is the store a run's report is written to, as far as the
// report uses it: one request, the context each unit of work was given, and
// every change asked of it.
type reportStore struct {
	repositories.Repository

	stored  entities.DeviationRequest
	given   []context.Context
	changes []repocontracts.DeviationRequestChange
	// What reading a definition, and what a unit of work, panics with; nil
	// for a part of the store that works.
	fallsOnDefinitions, fallsOnReport any
}

func (r *reportStore) UnitOfWork() repocontracts.UnitOfWork { return reportUnits{store: r} }

func (r *reportStore) DeviationRequest() repocontracts.DeviationRequestRepository {
	return reportRequests{store: r}
}

func (r *reportStore) Definition() repocontracts.DefinitionRepository {
	panic(r.fallsOnDefinitions)
}

type reportUnits struct {
	repocontracts.UnitOfWork
	store *reportStore
}

func (u reportUnits) Do(ctx context.Context, fn func(ctx context.Context) error) error {
	if u.store.fallsOnReport != nil {
		panic(u.store.fallsOnReport)
	}
	u.store.given = append(u.store.given, ctx)
	return fn(ctx)
}

type reportRequests struct {
	repocontracts.DeviationRequestRepository
	store *reportStore
}

func (r reportRequests) GetForUpdateWithoutDocuments(context.Context, uuid.UUID) (entities.DeviationRequest, error) {
	return r.store.stored, nil
}

func (r reportRequests) GetReadable(context.Context, uuid.UUID) (entities.DeviationRequest, error) {
	return r.store.stored, nil
}

func (r reportRequests) Transition(_ context.Context, _ uuid.UUID, from entities.DeviationRequestStatus, change repocontracts.DeviationRequestChange) (entities.DeviationRequest, error) {
	if r.store.stored.Status != from {
		return entities.DeviationRequest{}, fmt.Errorf("the request is %s, not %s", r.store.stored.Status, from)
	}
	r.store.changes = append(r.store.changes, change)
	r.store.stored.Status, r.store.stored.Outcome = change.Status, change.Outcome
	return r.store.stored, nil
}

func approvedRequest() entities.DeviationRequest {
	return entities.DeviationRequest{ID: uuid.Must(uuid.NewV7()), Kind: entities.DeviationRequestMigration, Status: entities.DeviationRequestApproved}
}

// The report of a run is owed whatever became of the call that approved it:
// it is written though that call has been cancelled. And it does not wait for
// ever on a store that has gone: it has a deadline of its own.
func TestTheReportOfARunIsWrittenUnderADeadlineOfItsOwnThoughItsCallWasCancelled(t *testing.T) {
	t.Parallel()
	store := &reportStore{stored: approvedRequest()}
	s := &migrationService{repo: store}
	called, cancel := context.WithCancel(context.Background())
	cancel()

	before := time.Now()
	reported := s.reportRun(called, store.stored, entities.MigrationResult{Changed: 2}, nil, before)

	if reported.Status != entities.DeviationRequestApplied || len(store.changes) != 1 || store.changes[0].Outcome[outcomeChanged] != 2 {
		t.Fatalf("the report of a run whose call was cancelled: %q with %+v, want it written", reported.Status, store.changes)
	}
	if len(store.given) != 1 {
		t.Fatalf("the report was written in %d unit(s) of work, want one", len(store.given))
	}
	deadline, has := store.given[0].Deadline()
	if !has {
		t.Fatal("the report is written with no deadline: on a store that has gone it waits for ever, and the approver's call with it")
	}
	if wait := deadline.Sub(before); wait < runReportTimeout-time.Second || wait > runReportTimeout+5*time.Second {
		t.Fatalf("the report waits %s to be written, want about %s", wait, runReportTimeout)
	}
	// By the time the report returns its context is released; while it was
	// being written it was not the cancelled one it was called with.
	if cause := context.Cause(store.given[0]); errors.Is(cause, context.DeadlineExceeded) {
		t.Fatalf("the report's context ended on its deadline: %v", cause)
	}
}

// A report is written once. A later one finds the request as the first left
// it and leaves it so — whether the first counted what the run did or, for a
// run that panicked, said that nobody knows. Only the sweep's mark, left on a
// request whose run was still going, is written over.
func TestOnlyTheSweepsMarkIsWrittenOverByALaterReport(t *testing.T) {
	t.Parallel()
	panicked := runOutcome(entities.MigrationResult{}, errRunPanicked)
	if _, counted := panicked[outcomeChanged]; counted {
		t.Fatalf("a run that panicked claims a count nobody has: %v", panicked)
	}
	for name, c := range map[string]struct {
		outcome     map[string]any
		writtenOver bool
	}{
		"the sweep's mark":                    {unreportedOutcome(nil), true},
		"the sweep's mark on a self-approval": {unreportedOutcome(map[string]any{"self_approved": true}), true},
		"a run's report":                      {runOutcome(entities.MigrationResult{Changed: 1}, errors.New("stopped")), false},
		"the report of a run that panicked":   {panicked, false},
	} {
		if got := !reportedByARun(c.outcome); got != c.writtenOver {
			t.Errorf("%s (%v): written over by a later report = %v, want %v", name, c.outcome, got, c.writtenOver)
		}
		request := approvedRequest()
		request.Status, request.Outcome = entities.DeviationRequestInterrupted, c.outcome
		store := &reportStore{stored: request}
		s := &migrationService{repo: store}
		reported := s.reportRun(context.Background(), request, entities.MigrationResult{Changed: 3}, nil, time.Now())
		switch {
		case c.writtenOver && (len(store.changes) != 1 || reported.Status != entities.DeviationRequestApplied || reported.Outcome[outcomeReportedAfterSweep] != true):
			t.Errorf("%s: a run that finished after it reads %q with %v, want its own report over the mark", name, reported.Status, reported.Outcome)
		case !c.writtenOver && (len(store.changes) != 0 || reported.Status != entities.DeviationRequestInterrupted):
			t.Errorf("%s: a later report made %d change(s) and reads %q, want the request left as it was", name, len(store.changes), reported.Status)
		}
	}
}

// A run that panics is reported from a deferred call, and the panic then goes
// on its way. If that report panics too — the store that failed the run is
// the store the report is written to — it is the run's panic that goes on,
// not the report's: the first failure is the one to be read.
func TestAPanicInTheReportOfARunThatPanickedDoesNotReplaceTheRunsOwn(t *testing.T) {
	t.Parallel()
	for name, fallsOnReport := range map[string]any{
		"the report is written": nil,
		"the report panics too": "the request's store fell over",
	} {
		store := &reportStore{stored: approvedRequest(), fallsOnDefinitions: "the definitions fell over", fallsOnReport: fallsOnReport}
		s := &migrationService{repo: store}
		run := approvedRun{request: store.stored, source: uuid.Must(uuid.NewV7()), target: uuid.Must(uuid.NewV7())}
		var recovered any
		func() {
			defer func() { recovered = recover() }()
			_, _, _ = s.runAndReport(context.Background(), run, store.stored.ID)
		}()
		if recovered != "the definitions fell over" {
			t.Errorf("%s: the approval's call ended on %v, want the run's own panic passed on", name, recovered)
		}
		if fallsOnReport == nil && (store.stored.Status != entities.DeviationRequestInterrupted || !reportedByARun(store.stored.Outcome)) {
			t.Errorf("%s: the request reads %q with %v, want it interrupted by a report a later one leaves alone", name, store.stored.Status, store.stored.Outcome)
		}
	}
}
