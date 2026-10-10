package impl

import (
	"cmp"
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/gsoultan/metis/internal/pkg/apierr"
	"github.com/gsoultan/metis/server/domains/entities"
)

// replay answers a request that was already made with the record of what it
// did, and reports whether it found one.
//
// The visit is looked for by the key the request names: a retry repeats the
// key of the plan it previewed, and the row the first attempt wrote carries
// it. Found, and asking for the same thing, it is answered as done and
// nothing is done again. Found, and asking for something else, it is refused
// with who acted: the visit has had its act.
//
// A row that waits for a second administrator is answered by replayWaiting:
// the visit has been asked for, and has not had its act. One that waits on a
// request that is past its deadline, or over, is not answered at all: the
// error says which request (overdueRequest), and nothing is written.
//
// Its caller holds the instance's lock, so an apply that was in flight for
// the same visit has finished, and its row is here to find.
func (s *instanceDeviationService) replay(ctx context.Context, command entities.DeviationCommand) (entities.DeviationOutcome, bool, error) {
	row, waitsOn, found, err := s.recordOfVisit(ctx, command)
	if err != nil || !found {
		return entities.DeviationOutcome{}, false, err
	}
	if waitsOn != nil {
		return replayWaiting(ctx, row, *waitsOn, command)
	}
	same, err := sameRequest(row, command)
	if err != nil {
		return entities.DeviationOutcome{}, false, err
	}
	if !same {
		return entities.DeviationOutcome{}, false, alreadyActedOn(row)
	}
	return replayed(row, command), true, nil
}

// replayed is the answer to a request whose visit already has its row: the
// row, and a plan that says what it was for and no more. Applied says whether
// the row is an act that was made, or one that waits.
//
// The plan says that a second administrator is needed wherever a plan of
// that kind says it (startPlanning): of a waive, whether its row waits or was
// applied. It is what the kind needs, not where this request stands — which
// Applied and PendingApproval say.
func replayed(row entities.Deviation, command entities.DeviationCommand) entities.DeviationOutcome {
	plan := entities.DeviationPlan{
		InstanceID: command.InstanceID, Kind: row.Kind, Scope: row.Scope, NodeID: command.NodeID, VisitKey: row.VisitKey,
		RequiresSecondApprover: row.Kind == entities.DeviationWaive,
	}
	if row.Node != nil {
		plan.NodeName = cmp.Or(row.Node.Name, row.Node.ID)
	}
	return entities.DeviationOutcome{
		Plan: plan, Applied: row.Status == entities.DeviationApplied, Replayed: true, Deviation: &row,
	}
}

// recordOfVisit reads the live row of the visit a command names and, when
// that row waits for a second administrator, the request it waits on.
//
// Read from the store the ledger writes to. The ledger's own interface has no
// such read, so a wiring with no store is refused here in the ledger's words,
// as it refuses a write: answering "no such act" would let the act be made
// again.
//
// The request is read and not held. The caller holds the instance, and a
// request's row is taken before an instance's, never after: an approval that
// holds the request and is waiting for this instance would otherwise wait for
// ever, and so would this.
//
// A request that no longer holds the visit is not answered as waiting. That
// is one past its deadline which nothing has yet written down — the clock
// decides, and the sweep only records — and one that was decided between the
// two reads, by somebody who holds its row and needs no instance. Neither can
// be put right here: recording an expiry takes the request's row, and that is
// never taken while an instance is held. So this answers which request it is
// (overdueRequest) and writes nothing, whoever asks and whatever they ask;
// the caller of the apply lets go of the instance, closes the request, and
// applies once more.
func (s *instanceDeviationService) recordOfVisit(
	ctx context.Context,
	command entities.DeviationCommand,
) (row entities.Deviation, waitsOn *entities.DeviationRequest, found bool, err error) {
	store := s.repo.Deviation()
	if store == nil {
		return entities.Deviation{}, nil, false, errNoDeviationLedger
	}
	row, found, err = store.FindLiveByVisit(ctx, command.InstanceID, command.VisitKey)
	if err != nil || !found || row.Status != entities.DeviationPendingApproval {
		return row, nil, found, err
	}
	request, err := s.requestWaitedOn(ctx, row)
	if err != nil {
		return entities.Deviation{}, nil, false, err
	}
	if request.EffectiveStatus(time.Now()) != entities.DeviationRequestPending {
		return entities.Deviation{}, nil, false, overdueRequest{id: request.ID}
	}
	return row, &request, true, nil
}

// requestWaitedOn reads the request a waiting ledger row names, without
// holding it, and without needing its sealed documents to open: whether it
// still waits, who asked and until when are all this is read for, and a
// request whose stored plan is damaged must not make its step unaskable. A failure is the server's, whatever class it came with: the
// caller has locked the instance the row is of, so "not found" is not an
// answer about anything they asked for.
func (s *instanceDeviationService) requestWaitedOn(ctx context.Context, row entities.Deviation) (entities.DeviationRequest, error) {
	requests := s.repo.DeviationRequest()
	if requests == nil {
		return entities.DeviationRequest{}, errNoDeviationRequests
	}
	request, err := requests.GetReadable(ctx, row.RequestID)
	if err != nil {
		return entities.DeviationRequest{}, effectFailed(fmt.Sprintf("reading the request deviation %s waits on", row.ID), err)
	}
	return request, nil
}

// replayWaiting answers a request for a visit that has been asked for and
// waits for a second administrator.
//
// Whoever made the request, asking for the same thing again, is answered with
// it: the row, not applied, and the request it waits on — a lost answer is
// safe to ask for again, and no second request is made. They are told apart
// by account id, not by name. Anybody else, and the requester asking for
// something else on the visit, is pointed at the request that waits
// (alreadyWaiting): another administrator does not get a request of their own
// to approve by asking for the same thing.
func replayWaiting(
	ctx context.Context,
	row entities.Deviation,
	request entities.DeviationRequest,
	command entities.DeviationCommand,
) (entities.DeviationOutcome, bool, error) {
	var none entities.DeviationOutcome
	again, err := sameAskByItsRequester(ctx, row, request, command)
	if err != nil {
		return none, false, err
	}
	if !again {
		return none, false, alreadyWaiting(row, request)
	}
	outcome := replayed(row, command)
	waiting := entities.PendingApprovalOf(request)
	outcome.PendingApproval = &waiting
	return outcome, true, nil
}

// sameAskByItsRequester reports whether a command is the waiting request
// made again by whoever made it: the same account — by id, never by name —
// asking for the same thing (sameRequest).
//
// It is the one place that is decided. An apply answers such a command with
// the request that waits and anything else with a refusal (replayWaiting); a
// preview warns of the first and refuses the second (withWhatWaits). Were the
// two to ask it differently, a preview would promise what its apply refuses.
func sameAskByItsRequester(ctx context.Context, row entities.Deviation, request entities.DeviationRequest, command entities.DeviationCommand) (bool, error) {
	caller := signedIn(ctx)
	if caller == nil || caller.ID == uuid.Nil || caller.ID != request.RequestedByID {
		return false, nil
	}
	return sameRequest(row, command)
}

// alreadyActedOn is the refusal of a request for a visit that has had its
// act, when it is not that act asked for again. It says what was done and by
// whom, which is what the caller needs to stop asking.
func alreadyActedOn(row entities.Deviation) error {
	what := "this instance"
	if row.Scope == entities.DeviationScopeTask {
		what = "this step"
	}
	return apierr.Invalidf("%s was already %s by %s", what, pastTense(row.Kind), row.Actor)
}

// sameRequest reports whether a command asks for what a row records: the
// same act, on the same step or on none, for the same reason, counting as the
// same values.
func sameRequest(row entities.Deviation, command entities.DeviationCommand) (bool, error) {
	recordedNode := ""
	if row.Node != nil {
		recordedNode = row.Node.ID
	}
	if row.Kind != command.Kind || recordedNode != command.NodeID ||
		strings.TrimSpace(row.Reason) != strings.TrimSpace(command.Reason) {
		return false, nil
	}
	var recorded map[string]any
	if set, has := row.After["variables"]; has {
		values, ok := set.(map[string]any)
		if !ok {
			return false, fmt.Errorf("reading what deviation %s set: it is recorded as %T, not as values by name", row.ID, set)
		}
		recorded = values
	}
	was, err := canonicalValues(recorded)
	if err != nil {
		return false, fmt.Errorf("reading what deviation %s set: %w", row.ID, err)
	}
	asked, err := canonicalValues(command.Outputs)
	if err != nil {
		return false, apierr.Invalidf("the outputs cannot be written as JSON: %v", err)
	}
	return was == asked, nil
}

// canonicalValues writes values the one way two sets of them are compared:
// as JSON, read back and written again. Written once, the keys are in order;
// read back, a number is the number the record keeps, whatever type carried
// it here — 1, 1.0 and a decoder's "1" are one value, as they are once
// stored. No values and an empty set are the same nothing.
func canonicalValues(values map[string]any) (string, error) {
	if len(values) == 0 {
		return "{}", nil
	}
	written, err := json.Marshal(values)
	if err != nil {
		return "", err
	}
	var read any
	if err := json.Unmarshal(written, &read); err != nil {
		return "", err
	}
	again, err := json.Marshal(read)
	if err != nil {
		return "", err
	}
	return string(again), nil
}
