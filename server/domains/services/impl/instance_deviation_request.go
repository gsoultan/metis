package impl

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"maps"
	"time"

	"github.com/google/uuid"

	"github.com/gsoultan/metis/internal/pkg/apierr"
	"github.com/gsoultan/metis/server/domains/entities"
	repocontracts "github.com/gsoultan/metis/server/repositories/contracts"
	"github.com/gsoultan/metis/server/repositories/models"
)

// errNoDeviationRequests is what asking for, or deciding, a request answers
// in a wiring with no store of requests: a plain error, because it is how the
// server was put together. Answering as though nothing waited would let a
// waive be made with nobody asked.
var errNoDeviationRequests = errors.New(
	"a second administrator cannot be asked: this server was wired without the store of requests for one")

// request records that an administrator asked for a step to be waived, and
// changes nothing else: the waive waits for a second administrator.
//
// It runs in the apply's unit of work, on the row the apply locked and judged
// — after the replay, refuseEnded and refuseUnlessAsPreviewed — so a request
// is made only for an instance that is running, whose plan made from the
// locked row is the one previewed and refuses nothing, and that still waits
// at the step: what an apply had to pass before a waive needed anybody else.
//
// It writes three things, together or not at all: the request, with the
// command and the plan as the requester was shown it and a deadline fixed
// now; a ledger row that waits (pending_approval) and holds the visit, so a
// second ask finds it; and a trail entry that says a waive was asked for and
// that nothing has changed. Nothing is set, withdrawn or advanced, nobody is
// told anything, and no task's row is taken.
//
// The row names no task, in its task or in its before and after: nothing was
// withdrawn, and who holds the work is recorded from the rows the effect
// holds, when the waive is approved. It keeps two counts from the plan — the
// plan's own, never the length of a capped list — so a request for a step
// five thousand people hold is a row of two numbers. It does keep the values
// asked for, under after.variables: that is what a retry is compared with.
//
// The locks. Its caller holds the instance. This inserts a request's row and
// a ledger row that names it; it takes no lock on a request that exists. The
// order everywhere is request, then instance, and this does not turn it
// round: the request it writes is new and nobody else can see it, let alone
// hold it, so the key share the ledger row's foreign key takes on it waits
// for nobody. And the insert of the request cannot wait behind another ask
// for the same fingerprint, though the index on it would make it: a
// fingerprint is a visit key, a visit key is made from the instance's id, and
// whoever else asks for this instance's visit is waiting for the lock held
// here, or finished before it was taken.
func (s *instanceDeviationService) request(
	ctx context.Context,
	locked models.ProcessInstanceModel,
	plan entities.DeviationPlan,
	command entities.DeviationCommand,
	account entities.User,
) (entities.DeviationOutcome, error) {
	var none entities.DeviationOutcome
	requests := s.repo.DeviationRequest()
	if requests == nil {
		return none, errNoDeviationRequests
	}
	requestID, err := uuid.NewV7()
	if err != nil {
		return none, err
	}
	runID, err := uuid.NewV7()
	if err != nil {
		return none, err
	}
	row := inPlaceDeviation(locked, plan, command, account.Username, runID)
	row.Status, row.RequestID, row.ActorID = entities.DeviationPendingApproval, requestID, account.ID
	if len(command.Outputs) > 0 {
		row.After = map[string]any{"variables": maps.Clone(command.Outputs)}
	}
	row.Details = map[string]any{"open_work": plan.OpenWorkInAll, "decision_points": plan.DecisionPointsInAll}

	asked, err := storeRequest(ctx, requests, waiveRequest(requestID, locked, plan, command, account), row)
	if err != nil {
		return none, err
	}
	narrative := fmt.Sprintf("%s asked for “%s” to be waived — nobody would perform it. "+
		"Nothing changes until a different administrator approves; the request expires on %s. Reason: %s",
		asked.RequestedBy, plan.NodeName, asked.ExpiresAt.UTC().Format(decidedOnLayout), command.Reason)
	recorded, err := s.actions.record(ctx, row, requestEntry(EventDeviationRequested, asked, row, narrative))
	if err != nil {
		return none, effectFailed(fmt.Sprintf("recording that “%s” was asked to be waived", plan.NodeName), err)
	}
	waiting := entities.PendingApprovalOf(asked)
	return entities.DeviationOutcome{Plan: plan, Deviation: &recorded, PendingApproval: &waiting}, nil
}

// waiveRequest is the request for a waive as it is first written: what was
// asked (the command), what the requester was shown (the plan, and why it
// needs somebody else), who asked, and a deadline fixed now from the setting
// as it is now. Its fingerprint is the visit key of the plan, which is what
// makes it the one request for that visit.
func waiveRequest(
	id uuid.UUID,
	locked models.ProcessInstanceModel,
	plan entities.DeviationPlan,
	command entities.DeviationCommand,
	account entities.User,
) entities.DeviationRequest {
	// Its problem, if it has one, was said when the server started.
	ttl, _ := DeviationApprovalTTL()
	because := []string{fmt.Sprintf("“%s” would be waived: nobody performs it, and the process moves on", plan.NodeName)}
	return entities.DeviationRequest{
		ID:            id,
		Project:       &entities.Project{ID: uuid.UUID(locked.ProjectID)},
		Kind:          entities.DeviationRequestInstanceWaive,
		Status:        entities.DeviationRequestPending,
		Instance:      &entities.ProcessInstance{ID: uuid.UUID(locked.ID)},
		RequestedBy:   account.Username,
		RequestedByID: account.ID,
		Reason:        command.Reason,
		Command:       waiveCommandDocument(command),
		Plan:          waivePlanDocument(plan, because),
		Fingerprint:   plan.VisitKey,
		ExpiresAt:     time.Now().Add(ttl),
	}
}

// storeRequest writes the request for a waive, and refuses when one for the
// visit already waits. row is the ledger row that will wait on it, for the
// step's name.
//
// The ledger's row is what an ask finds first (replay), and a request and its
// row are written and decided together, so by the time an ask gets here no
// live request for the visit should exist. One is looked for all the same,
// before the write: found, it is told as any waiting request is, naming it.
// The look is not a race — whoever else could write a request with this
// fingerprint needs the instance this ask holds. And should the database
// refuse the write itself (ErrDeviationRequestAlreadyWaiting), that is the
// same state, answered as the same refusal; the statement that failed has
// ended the transaction, so nothing more is read in it and the error is
// returned for the unit of work to undo.
//
// Any other failure is the server's, whatever class it came with: the
// repository answers "not found" for a project or an instance it cannot see,
// and the caller has just locked this instance in their own organization.
func storeRequest(
	ctx context.Context,
	requests repocontracts.DeviationRequestRepository,
	asking entities.DeviationRequest,
	row entities.Deviation,
) (entities.DeviationRequest, error) {
	var none entities.DeviationRequest
	doing := fmt.Sprintf("recording the request to waive “%s”", stepOf(row))
	waiting, found, err := requests.FindLive(ctx, asking.Project.ID, asking.Fingerprint)
	if err != nil {
		return none, effectFailed(doing, err)
	}
	if found {
		return none, alreadyWaiting(row, waiting)
	}
	asked, err := requests.Create(ctx, asking)
	if errors.Is(err, repocontracts.ErrDeviationRequestAlreadyWaiting) {
		return none, apierr.Invalidf("A request to waive “%s” is already waiting for approval; approve or reject that one.", stepOf(row))
	}
	if err != nil {
		return none, effectFailed(doing, err)
	}
	return asked, nil
}

// alreadyWaiting is the refusal of a request for a visit that has one
// waiting, when it is not that request made again by whoever made it. It
// names the request and who asked, which is what the caller needs to stop
// asking and go and decide it: a second administrator who asks for the same
// thing does not get a request of their own to approve (Ruling 14).
//
// A state they met, not a mistake: there is no class for a conflict, so it is
// an invalid argument, as "already waived by …" is.
func alreadyWaiting(row entities.Deviation, request entities.DeviationRequest) error {
	return apierr.Invalidf("%s", alreadyWaitingSentence(row, request))
}

// alreadyWaitingSentence is alreadyWaiting's words, for a preview to say
// before an apply would.
func alreadyWaitingSentence(row entities.Deviation, request entities.DeviationRequest) string {
	return fmt.Sprintf("A request to waive “%s” is already waiting for approval (request %s, asked by %s); approve or reject that one.",
		stepOf(row), request.ID, request.RequestedBy)
}

// withWhatWaits adds to a preview's plan what an apply of it would meet when
// a request already waits for the visit — so that it is read in the preview
// and not found by applying.
//
// To whoever made that request, previewing the same thing, it is a warning:
// an apply would answer with the request that waits, and the plan still
// applies. To anybody else, and for anything else asked of the visit, it is a
// refusal, in the words the apply would refuse with (alreadyWaiting). The two
// are told apart by the function the apply tells them apart with
// (sameAskByItsRequester): by account id, and by what is asked.
//
// A request past its deadline, or over, says nothing here: it does not hold
// the visit, and an apply closes it and makes a fresh one. Only a waive is
// looked at — nothing else waits for anybody — and only a plan that names a
// visit.
//
// It reads the visit's live row and its request, takes no lock and writes
// nothing, as a preview does. It is added to a preview's plan only: the plan
// an apply or an approval makes under the instance's lock is judged without
// it, or a request would refuse its own approval.
func (s *instanceDeviationService) withWhatWaits(
	ctx context.Context,
	plan entities.DeviationPlan,
	command entities.DeviationCommand,
) (entities.DeviationPlan, error) {
	if plan.Kind != entities.DeviationWaive || plan.VisitKey == "" {
		return plan, nil
	}
	store := s.repo.Deviation()
	if store == nil {
		return plan, errNoDeviationLedger
	}
	row, found, err := store.FindLiveByVisit(ctx, command.InstanceID, plan.VisitKey)
	if err != nil || !found || row.Status != entities.DeviationPendingApproval {
		return plan, err
	}
	request, err := s.requestWaitedOn(ctx, row)
	if err != nil {
		return plan, err
	}
	if request.EffectiveStatus(time.Now()) != entities.DeviationRequestPending {
		return plan, nil
	}
	mine, err := sameAskByItsRequester(ctx, row, request, command)
	if err != nil {
		return plan, err
	}
	if !mine {
		plan.Refusals = append(plan.Refusals, alreadyWaitingSentence(row, request))
		return plan, nil
	}
	plan.Warnings = append(plan.Warnings, fmt.Sprintf(
		"A request to waive “%s” is already waiting for approval (request %s, asked by %s, until %s). "+
			"Applying this again answers with that request and makes no second one.",
		stepOf(row), request.ID, request.RequestedBy, request.ExpiresAt.UTC().Format(decidedOnLayout)))
	return plan, nil
}

// stepOf is the step a ledger row names, by its name or failing that its id.
func stepOf(row entities.Deviation) string {
	if row.Node == nil {
		return ""
	}
	return cmp.Or(row.Node.Name, row.Node.ID)
}
