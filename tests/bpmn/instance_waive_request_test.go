package bpmn_test

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/gsoultan/metis/internal/pkg/apierr"
	"github.com/gsoultan/metis/server/domains/entities"
	serviceimpl "github.com/gsoultan/metis/server/domains/services/impl"
	deviationendpoint "github.com/gsoultan/metis/server/endpoints/deviation"
	"github.com/gsoultan/metis/server/repositories"
	repocontracts "github.com/gsoultan/metis/server/repositories/contracts"
	"gorm.io/gorm"
)

// requestCount is how many requests for a second administrator the test's
// database holds.
func requestCount(t *testing.T, h engineHarness) int {
	t.Helper()
	var n int
	if err := h.db.Raw(`SELECT count(*) FROM deviation_requests`).Scan(&n).Error; err != nil {
		t.Fatalf("count the requests: %v", err)
	}
	return n
}

// D9 names a waive, and a migration that loosens a rule, and nothing else
// (Ruling 42). A cancel ends an instance and a hold stops one: neither
// loosens a rule on work that goes on, and each stays one administrator's
// call — applied by the request that asks for it, with no request made and
// nobody else asked.
func TestACancelAndAHoldStillApplyOnOneAdministratorsCall(t *testing.T) {
	h := newEngineHarness(t, "One Call Project")
	events := &eventLog{}
	h.dispatcher.Register(events)
	w := newWaiver(h)
	h.deploy(t, opsApproval(h.projID, "ops-one-call"))
	for _, kind := range []entities.DeviationKind{entities.DeviationHold, entities.DeviationCancel} {
		id, err := h.svc.StartProcess(h.Ctx(), h.projID, "ops-one-call", nil)
		if err != nil {
			t.Fatalf("start an instance to %s: %v", kind, err)
		}
		cmd := deviationCommand(kind, id, "opsApprove", nil)
		if plan := w.preview(t, cmd); plan.RequiresSecondApprover || !plan.Applicable() {
			t.Fatalf("the plan of a %s: needs a second administrator %v, refusals%s", kind, plan.RequiresSecondApprover, lines(plan.Refusals))
		}
		// Through the service itself, with nobody to approve anything.
		out, err := w.asking.DeviateInstance(w.ctx, w.previewed(t, cmd))
		if err != nil || !out.Applied || out.Replayed || out.PendingApproval != nil || out.Deviation == nil {
			t.Fatalf("a %s by one administrator: %+v, %v; want it applied and waiting on nobody", kind, out, err)
		}
		row := out.Deviation
		if row.Status != entities.DeviationApplied || row.Actor != "ana" || row.RequestID != uuid.Nil || row.ApprovedBy != "" || row.DecidedAt != nil {
			t.Fatalf("the row of a %s names a request or an approver: %+v", kind, row)
		}
		if asks := entriesOfType(t, h, id, serviceimpl.EventDeviationRequested); len(asks) != 0 {
			t.Fatalf("the trail says a %s was asked of somebody: %+v", kind, asks)
		}
	}
	if n := requestCount(t, h); n != 0 {
		t.Fatalf("a hold and a cancel made %d request(s) for a second administrator", n)
	}
	// The cancel did what a cancel does, there and then.
	if withdrawn := events.ofType(entities.EventTaskCanceled); len(withdrawn) != 1 {
		t.Fatalf("the cancel withdrew %d task(s), want the one", len(withdrawn))
	}
	// A waive is the one that waits.
	id, err := h.svc.StartProcess(h.Ctx(), h.projID, "ops-one-call", nil)
	if err != nil {
		t.Fatalf("start an instance to waive: %v", err)
	}
	if plan := w.preview(t, deviationCommand(entities.DeviationWaive, id, "opsApprove", nil)); !plan.RequiresSecondApprover {
		t.Fatal("the plan of a waive does not say it needs a second administrator")
	}
	w.ask(t, deviationCommand(entities.DeviationWaive, id, "opsApprove", nil))
	if n := requestCount(t, h); n != 1 {
		t.Fatalf("a waive made %d request(s), want the one", n)
	}
}

// A request that waits is answered again to whoever made it, as the same
// request: a retry of a lost answer makes no second one. Anybody else asking
// for the visit, and the requester asking for something else on it, is
// pointed at the one that waits — nobody re-asks in order to approve the
// first as "their own" (Ruling 14). None of it writes anything.
func TestAWaiveAskedForAgainIsAnsweredWithTheRequestThatWaits(t *testing.T) {
	h := newEngineHarness(t, "Asked Again Project")
	h.recordsAsProductionDoes()
	w := newWaiver(h)
	id := w.start(t, unroutable(h, "asked-again", entities.ExclusiveGateway, "Verdict?"), nil)
	cmd := w.previewed(t, deviationCommand(entities.DeviationWaive, id, "review", map[string]any{"verdict": "accept"}))
	first, err := w.asking.DeviateInstance(w.ctx, cmd)
	if err != nil || first.Applied || first.Replayed || first.PendingApproval == nil {
		t.Fatalf("the ask: %+v, %v", first, err)
	}
	if want := []string{"“Review the claim” would be waived: nobody performs it, and the process moves on"}; !reflect.DeepEqual(first.PendingApproval.Because, want) {
		t.Fatalf("the request says it needs somebody else because %q, want %q", first.PendingApproval.Because, want)
	}
	before := everyRow(t, h)

	again, err := w.asking.DeviateInstance(w.ctx, cmd)
	if err != nil || again.Applied || !again.Replayed || again.PendingApproval == nil || again.Deviation == nil {
		t.Fatalf("the same ask again: %+v, %v; want the request that waits, replayed", again, err)
	}
	if again.PendingApproval.RequestID != first.PendingApproval.RequestID || !again.PendingApproval.ExpiresAt.Equal(first.PendingApproval.ExpiresAt) ||
		!reflect.DeepEqual(again.PendingApproval.Because, first.PendingApproval.Because) || again.Deviation.ID != first.Deviation.ID {
		t.Fatalf("the retry answered another request or another row: %+v, first %+v", again.PendingApproval, first.PendingApproval)
	}
	if !again.Plan.RequiresSecondApprover || again.Plan.VisitKey != cmd.VisitKey || again.Plan.NodeID != "review" {
		t.Errorf("the replay's plan %+v does not say what waits", again.Plan)
	}
	// ana renamed is still ana: the request is hers by account.
	if renamed, err := w.asking.DeviateInstance(w.asAccount("ana-renamed", accountID("ana")), cmd); err != nil || !renamed.Replayed || renamed.PendingApproval == nil {
		t.Fatalf("the requester, renamed, asking again: %+v, %v; want her request replayed", renamed, err)
	}

	waiting := apierr.Invalidf("A request to waive “Review the claim” is already waiting for approval (request %s, asked by ana); approve or reject that one.",
		first.PendingApproval.RequestID)
	otherValue, otherReason := cmd, cmd
	otherValue.Outputs = map[string]any{"verdict": "reject"}
	otherReason.Reason = "another reason altogether"
	for who, ask := range map[string]struct {
		ctx context.Context
		cmd entities.DeviationCommand
	}{
		"another administrator asking for the same": {w.as("budi"), cmd},
		"somebody else who is called ana":           {w.asAccount("ana", accountID("citra")), cmd},
		"the requester asking for another value":    {w.ctx, otherValue},
		"the requester asking with another reason":  {w.ctx, otherReason},
		"another administrator asking for another":  {w.as("budi"), otherValue},
	} {
		out, err := w.asking.DeviateInstance(ask.ctx, ask.cmd)
		if !errors.Is(err, apierr.ErrInvalidArgument) || err.Error() != waiting.Error() {
			t.Errorf("%s: got %v\nwant exactly\n  %v", who, err, waiting)
		}
		if out.PendingApproval != nil || out.Deviation != nil || out.Replayed {
			t.Errorf("%s was answered with the request or its row: %+v", who, out)
		}
	}
	if changed := tablesThatDiffer(before, everyRow(t, h)); len(changed) != 0 {
		t.Fatalf("asking again changed %v", changed)
	}
	if n := requestCount(t, h); n != 1 {
		t.Fatalf("%d requests wait for one visit, want the one", n)
	}

	// Approved, it is an act made: the requester's retry reads it as done,
	// and anybody else is told who waived the step.
	if _, err := w.approve(first); err != nil {
		t.Fatalf("budi approves: %v", err)
	}
	done, err := w.asking.DeviateInstance(w.ctx, cmd)
	if err != nil || !done.Applied || !done.Replayed || done.PendingApproval != nil || done.Deviation == nil || done.Deviation.ID != first.Deviation.ID {
		t.Fatalf("the requester's retry after the approval: %+v, %v; want the waive, replayed", done, err)
	}
	if _, err := w.asking.DeviateInstance(w.ctx, otherValue); !errors.Is(err, apierr.ErrInvalidArgument) ||
		!strings.Contains(err.Error(), "this step was already waived by ana") {
		t.Fatalf("another request for a visit that was waived: %v", err)
	}
	requireInstanceStatus(h.Ctx(), t, h, id, entities.ProcessCompleted)
}

// An approval is of what was asked, on the instance as it then stands. The
// value the requester gave is the value set, and the gateway after the step
// decides from it; who held the work is read from the task as the approval
// finds it — here it changed hands while the request waited — and what the
// instance held under the name is what it held then.
func TestAnApprovalSetsWhatWasAskedAndRecordsTheInstanceAsItThenStood(t *testing.T) {
	h := newEngineHarness(t, "Approved As Asked Project")
	events := &eventLog{}
	h.dispatcher.Register(events)
	w := newWaiver(h)
	id := w.start(t, unroutable(h, "approved-as-asked", entities.ExclusiveGateway, "Verdict?"), map[string]any{"verdict": "undecided"})
	task := theOpenTask(t, h, id, "review")
	asked := w.ask(t, deviationCommand(entities.DeviationWaive, id, "review", map[string]any{"verdict": "accept"}))
	if held := variablesOf(t, h, id)["verdict"]; held != "undecided" {
		t.Fatalf("asking set verdict to %v", held)
	}
	if row := w.theWaive(t, id); !reflect.DeepEqual(row.After["variables"], map[string]any{"verdict": "accept"}) || len(row.Before) != 0 ||
		row.Details["open_work"] != float64(1) || row.Details["decision_points"] != float64(1) {
		t.Fatalf("the row of the waive that waits: after %v, before %v, details %v", row.After, row.Before, row.Details)
	}

	// While it waits the task changes hands and the instance's value changes:
	// neither is what the visit is made of.
	if err := h.db.Exec(`UPDATE tasks SET assignee = 'carol' WHERE id = ?`, task.ID).Error; err != nil {
		t.Fatalf("hand the task to carol: %v", err)
	}
	stored, err := h.repo.Process().Get(h.Ctx(), id)
	if err != nil {
		t.Fatalf("read the instance: %v", err)
	}
	stored.Variables["verdict"] = "leaning-no"
	if err := h.repo.Process().Update(h.Ctx(), stored); err != nil {
		t.Fatalf("change what the instance holds: %v", err)
	}

	out, err := w.approve(asked)
	if err != nil || !out.Applied {
		t.Fatalf("budi approves: %+v, %v", out, err)
	}
	instance := requireInstanceStatus(h.Ctx(), t, h, id, entities.ProcessCompleted)
	if instance.Variables["verdict"] != "accept" {
		t.Fatalf("the instance holds verdict = %v, want what the requester asked for", instance.Variables["verdict"])
	}
	row := w.theWaive(t, id)
	if !reflect.DeepEqual(row.After["variables"], map[string]any{"verdict": "accept"}) ||
		!reflect.DeepEqual(row.Before["variables"], map[string]any{"verdict": "leaning-no"}) {
		t.Fatalf("the approved row: variables before %v, after %v; want what the instance held when it was approved, and what was asked",
			row.Before["variables"], row.After["variables"])
	}
	if recorded, _ := json.Marshal(row.Before["tasks"]); !strings.Contains(string(recorded), "carol") || strings.Contains(string(recorded), "rita") {
		t.Fatalf("the approved row names who held the work as %s, want carol, who held it when it was withdrawn", recorded)
	}
	withdrawn := events.ofType(entities.EventTaskCanceled)
	if len(withdrawn) != 1 || withdrawn[0].Assignee != "carol" {
		t.Fatalf("the withdrawal was announced to %+v, want carol once", withdrawn)
	}
	// The requester's retry is still the same request, values and all.
	cmd := deviationCommand(entities.DeviationWaive, id, "review", map[string]any{"verdict": "accept"})
	cmd.VisitKey = asked.Plan.VisitKey
	if again, err := w.asking.DeviateInstance(w.ctx, cmd); err != nil || !again.Applied || !again.Replayed {
		t.Fatalf("the requester's retry after the approval: %+v, %v", again, err)
	}
}

// The lock order is request → instance, everywhere. An ask holds the
// instance, so it may never wait for a request's row: it reads the request
// that waits and does not take it. Here an approver holds the request — as
// one who is waiting for the instance does — and the requester asks again:
// the ask answers at once. Were it to wait, the two would be each other's
// deadlock.
func TestAskingAgainNeverWaitsForTheRequestsRow(t *testing.T) {
	h := newEngineHarness(t, "Request Lock Order Project")
	w := newWaiver(h)
	id := w.start(t, opsApproval(h.projID, "ops-lock-order"), nil)
	cmd := w.previewed(t, deviationCommand(entities.DeviationWaive, id, "opsApprove", nil))
	first, err := w.asking.DeviateInstance(w.ctx, cmd)
	if err != nil || first.PendingApproval == nil {
		t.Fatalf("the ask: %+v, %v", first, err)
	}
	request := first.PendingApproval.RequestID

	// The request's row, taken FOR UPDATE as a decision takes it.
	held := h.holding(t, `WITH taken AS (SELECT id FROM deviation_requests WHERE id = ? FOR UPDATE)
		UPDATE deviation_requests r SET status = r.status FROM taken WHERE r.id = taken.id`, request)
	for who, ctx := range map[string]context.Context{"the requester": w.ctx, "another administrator": w.as("budi")} {
		asking := send(func() (entities.DeviationOutcome, error) {
			inTime, stop := context.WithTimeout(ctx, lockWait)
			defer stop()
			return w.asking.DeviateInstance(inTime, cmd)
		})
		out, err := asking.answer(t, who+" asking while the request's row is held")
		if who == "the requester" {
			if err != nil || !out.Replayed || out.PendingApproval == nil || out.PendingApproval.RequestID != request {
				t.Fatalf("%s: %+v, %v; want the request that waits", who, out, err)
			}
			continue
		}
		if !errors.Is(err, apierr.ErrInvalidArgument) || !strings.Contains(err.Error(), "already waiting for approval") {
			t.Fatalf("%s: %v, want it told one is waiting", who, err)
		}
	}
	// And the approver, who holds the request, gets the instance once nobody
	// has it: the approval behind the held row finishes when the row is let go.
	approval := w.sendApproval("budi", request)
	h.waitForWaiters(t, held, 1, approval.answered)
	held.letGo(t, false)
	if out, err := approval.answer(t, "budi's approval"); err != nil || !out.Applied {
		t.Fatalf("the approval that waited for the request's row: %+v, %v", out, err)
	}
}

// An approval takes its request's row before it asks for the instance, and
// holds it while it waits: that is what a second decision of the same request
// queues behind, and the first half of the order every decision keeps. Here
// the instance is held by somebody part-way through something, as a
// completion holds it; the approval is stopped waiting for it, and the
// request's row is by then taken — asking for it without waiting is refused.
func TestAnApprovalHoldsItsRequestWhileItWaitsForTheInstance(t *testing.T) {
	h := newEngineHarness(t, "Request First Project")
	w := newWaiver(h)
	id := w.start(t, opsApproval(h.projID, "ops-request-first"), nil)
	asked := w.ask(t, deviationCommand(entities.DeviationWaive, id, "opsApprove", nil))
	request := asked.PendingApproval.RequestID
	free := func() error {
		return h.db.Transaction(func(tx *gorm.DB) error {
			return tx.Exec(`SELECT id FROM deviation_requests WHERE id = ? FOR UPDATE NOWAIT`, request).Error
		})
	}
	if err := free(); err != nil {
		t.Fatalf("with nobody deciding it the request's row is held: %v", err)
	}

	held := h.holding(t, `UPDATE process_instances SET status = status WHERE id = ?`, id)
	approval := w.sendApproval("budi", request)
	h.waitForWaiters(t, held, 1, approval.answered)
	if err := free(); err == nil || !strings.Contains(err.Error(), "55P03") && !strings.Contains(err.Error(), "could not obtain lock") {
		t.Fatalf("while the approval waits for the instance the request's row answers %v, want it held", err)
	}
	held.letGo(t, false)
	if out, err := approval.answer(t, "budi's approval"); err != nil || !out.Applied {
		t.Fatalf("the approval, once the instance was let go: %+v, %v", out, err)
	}
	if err := free(); err != nil {
		t.Fatalf("after the approval the request's row is still held: %v", err)
	}
}

// repositoryBlindToLiveRequests is a repository that never finds a live
// request: what the service would see if one were written between its look
// and its own write.
type repositoryBlindToLiveRequests struct {
	repositories.Repository
}

func (r repositoryBlindToLiveRequests) DeviationRequest() repocontracts.DeviationRequestRepository {
	return blindToLiveRequests{r.Repository.DeviationRequest()}
}

type blindToLiveRequests struct {
	repocontracts.DeviationRequestRepository
}

func (blindToLiveRequests) FindLive(context.Context, uuid.UUID, string) (entities.DeviationRequest, bool, error) {
	return entities.DeviationRequest{}, false, nil
}

// One live request for a visit, whatever else is so. The ledger's row is what
// an ask finds first, and a request and its row are written and decided
// together — so a live request with no row beside it is a state the product
// does not make. It is written here through the repository, as an import or a
// repair might leave it, because what the service does on meeting it is the
// point: the database refuses a second live request for the fingerprint, and
// that is answered as somebody's request already waiting — a 400 in words —
// never as the server's failure, and nothing of the refused ask is kept.
func TestALiveRequestWithNoRowStillRefusesASecondForItsVisit(t *testing.T) {
	for name, blind := range map[string]bool{
		"found before the write":         false,
		"refused by the database itself": true,
	} {
		t.Run(name, func(t *testing.T) {
			h := newEngineHarness(t, "Bare Request Project")
			h.recordsAsProductionDoes()
			w := newWaiver(h)
			id := w.start(t, opsApproval(h.projID, "ops-bare-request"), nil)
			cmd := w.previewed(t, deviationCommand(entities.DeviationWaive, id, "opsApprove", nil))

			var bare entities.DeviationRequest
			err := h.repo.UnitOfWork().Do(h.Ctx(), func(txCtx context.Context) error {
				var err error
				bare, err = h.repo.DeviationRequest().Create(txCtx, entities.DeviationRequest{
					Project: &entities.Project{ID: h.projID}, Kind: entities.DeviationRequestInstanceWaive, Status: entities.DeviationRequestPending,
					Instance: &entities.ProcessInstance{ID: id}, RequestedBy: "citra", RequestedByID: accountID("citra"),
					Reason: waiveReason, Fingerprint: cmd.VisitKey, ExpiresAt: time.Now().Add(time.Hour),
				})
				return err
			})
			if err != nil {
				t.Fatalf("write a request with no row: %v", err)
			}
			asking := w.asking
			want := apierr.Invalidf("A request to waive “Operations approve” is already waiting for approval (request %s, asked by citra); approve or reject that one.", bare.ID)
			if blind {
				asking = serviceimpl.NewInstanceDeviationService(repositoryBlindToLiveRequests{h.repo}, h.engine)
				want = apierr.Invalidf("A request to waive “Operations approve” is already waiting for approval; approve or reject that one.")
			}
			before := everyRow(t, h)
			out, err := asking.DeviateInstance(w.ctx, cmd)
			if !errors.Is(err, apierr.ErrInvalidArgument) || err.Error() != want.Error() {
				t.Fatalf("an ask for a visit whose request already waits: %v\nwant exactly\n  %v", err, want)
			}
			if out.PendingApproval != nil || out.Deviation != nil {
				t.Errorf("the refused ask answered a request or a row: %+v", out)
			}
			if changed := tablesThatDiffer(before, everyRow(t, h)); len(changed) != 0 {
				t.Fatalf("the refused ask changed %v", changed)
			}
			if rows := w.ledger(t, id); len(rows) != 0 {
				t.Fatalf("the refused ask left %d ledger row(s)", len(rows))
			}
			if open := theOpenTask(t, h, id, "opsApprove"); open.AssigneeUsername() != "ollie" {
				t.Fatalf("after the refused ask the task is with %q", open.AssigneeUsername())
			}
		})
	}
}

// What a second administrator reads of a request is the plan the requester
// was shown — the fields of the route's plan view, under the same names, each
// capped list beside its count — and why it needs somebody else. It is kept
// as it was shown: what the instance has become since is the approval's to
// find, under its locks, and nothing is decided from this.
func TestWhatASecondAdministratorReadsIsThePlanTheRequesterWasShown(t *testing.T) {
	h := newEngineHarness(t, "Stored Plan Project")
	w := newWaiver(h)
	id := w.start(t, unroutable(h, "stored-plan", entities.ExclusiveGateway, "Verdict?"), nil)
	asked := w.ask(t, deviationCommand(entities.DeviationWaive, id, "review", map[string]any{"verdict": "accept"}))
	if len(asked.Plan.OpenWork) != 1 || len(asked.Plan.DecisionPoints) != 1 || len(asked.Plan.Warnings) == 0 {
		t.Fatalf("the plan asked with holds no open work, decision point or warning to compare: %+v", asked.Plan)
	}

	shown := map[string]any{}
	written, err := json.Marshal(deviationendpoint.PlanViewOf(asked.Plan))
	if err != nil {
		t.Fatalf("write the plan as the route does: %v", err)
	}
	if err := json.Unmarshal(written, &shown); err != nil {
		t.Fatalf("read it back: %v", err)
	}
	shown["because"] = []any{"“Review the claim” would be waived: nobody performs it, and the process moves on"}

	waiting, err := w.approvals.GetDeviationRequest(w.as("budi"), asked.PendingApproval.RequestID)
	if err != nil {
		t.Fatalf("budi reads the request: %v", err)
	}
	kept := map[string]any{}
	rewritten, err := json.Marshal(waiting.Plan)
	if err != nil {
		t.Fatalf("write the stored plan: %v", err)
	}
	if err := json.Unmarshal(rewritten, &kept); err != nil {
		t.Fatalf("read the stored plan: %v", err)
	}
	if !reflect.DeepEqual(kept, shown) {
		t.Fatalf("the request keeps\n  %s\nthe requester was shown\n  %s", rewritten, mustJSON(t, shown))
	}
	if kept["requires_second_approver"] != true || kept["applicable"] != true || kept["open_work_in_all"] != float64(1) {
		t.Fatalf("the stored plan does not say what the plan said: %s", rewritten)
	}
	// What was asked is kept with it: the command an approval is made from.
	want := map[string]any{"instance_id": id.String(), "kind": "waive", "node_id": "review", "reason": waiveReason,
		"outputs": map[string]any{"verdict": "accept"}, "visit_key": asked.Plan.VisitKey}
	if !reflect.DeepEqual(waiting.Command, want) {
		t.Fatalf("the request keeps the command\n  %v\nwant\n  %v", waiting.Command, want)
	}
	if waiting.Reason != waiveReason || waiting.Fingerprint != asked.Plan.VisitKey || waiting.Kind != entities.DeviationRequestInstanceWaive ||
		waiting.Instance == nil || waiting.Instance.ID != id || !reflect.DeepEqual(waiting.Because(), asked.PendingApproval.Because) {
		t.Fatalf("the request does not say what it is for: %+v", waiting)
	}
}

func mustJSON(t *testing.T, value any) string {
	t.Helper()
	written, err := json.Marshal(value)
	if err != nil {
		t.Fatalf("write %v as JSON: %v", value, err)
	}
	return string(written)
}

// The queue is an administrator's to read, in the organization the request
// is for: what waits by default, a status when one is named, and each request
// as it reads now — one past its deadline is expired, not offered as waiting,
// whether or not anything has written that down.
func TestTheRequestsAnAdministratorReadsAreTheOnesThatStillWait(t *testing.T) {
	h := newEngineHarness(t, "Request Queue Project")
	w := newWaiver(h)
	h.deploy(t, opsApproval(h.projID, "ops-queue"))
	asks := make([]entities.DeviationOutcome, 3)
	for i := range asks {
		id, err := h.svc.StartProcess(h.Ctx(), h.projID, "ops-queue", nil)
		if err != nil {
			t.Fatalf("start: %v", err)
		}
		asks[i] = w.ask(t, deviationCommand(entities.DeviationWaive, id, "opsApprove", nil))
	}
	waits, approved, overdue := asks[0].PendingApproval.RequestID, asks[1].PendingApproval.RequestID, asks[2].PendingApproval.RequestID
	if _, err := w.approve(asks[1]); err != nil {
		t.Fatalf("budi approves the second: %v", err)
	}
	if err := h.db.Exec(`UPDATE deviation_requests SET expires_at = now() - interval '1 minute' WHERE id = ?`, overdue).Error; err != nil {
		t.Fatalf("let time pass for the third: %v", err)
	}

	listed := func(status entities.DeviationRequestStatus) map[uuid.UUID]entities.DeviationRequestStatus {
		t.Helper()
		page, total, err := w.approvals.ListDeviationRequests(w.as("budi"), entities.DeviationRequestQuery{Status: status})
		if err != nil || int(total) != len(page) {
			t.Fatalf("list %q: %d of %d, %v", status, len(page), total, err)
		}
		read := map[uuid.UUID]entities.DeviationRequestStatus{}
		for _, request := range page {
			read[request.ID] = request.Status
		}
		return read
	}
	for status, want := range map[entities.DeviationRequestStatus]map[uuid.UUID]entities.DeviationRequestStatus{
		"":                               {waits: entities.DeviationRequestPending},
		entities.DeviationRequestPending: {waits: entities.DeviationRequestPending},
		entities.DeviationRequestApplied: {approved: entities.DeviationRequestApplied},
		entities.DeviationRequestExpired: {overdue: entities.DeviationRequestExpired},
		entities.DeviationRequestStale:   {},
	} {
		if got := listed(status); !reflect.DeepEqual(got, want) {
			t.Errorf("the requests listed as %q are %v, want %v", status, got, want)
		}
	}
	if got, err := w.approvals.GetDeviationRequest(w.as("budi"), overdue); err != nil || got.Status != entities.DeviationRequestExpired {
		t.Fatalf("the overdue request reads %q (%v), want expired before anything has written it", got.Status, err)
	}
	_, _, err := w.approvals.ListDeviationRequests(w.as("budi"), entities.DeviationRequestQuery{Status: "waiting"})
	want := apierr.Invalidf("status must be one of pending_approval, approved, applied, interrupted, stale, rejected, expired")
	if !errors.Is(err, apierr.ErrInvalidArgument) || err.Error() != want.Error() {
		t.Fatalf("a status that is not one: %v, want %v", err, want)
	}
	if _, err := w.approvals.GetDeviationRequest(w.as("budi"), uuid.Must(uuid.NewV7())); !errors.Is(err, apierr.ErrNotFound) {
		t.Fatalf("a request that does not exist: %v, want not found", err)
	}
	if _, err := w.approvals.ApproveDeviationRequest(w.as("budi"), uuid.Must(uuid.NewV7()), ""); !errors.Is(err, apierr.ErrNotFound) {
		t.Fatalf("approving a request that does not exist: %v, want not found", err)
	}
	// Rejecting and the sweep are the next change's; until then they say so
	// and do nothing.
	before := everyRow(t, h)
	if _, err := w.approvals.RejectDeviationRequest(w.as("budi"), waits, "no"); !errors.Is(err, apierr.ErrInvalidArgument) {
		t.Fatalf("rejecting: %v", err)
	}
	if n, err := w.approvals.ExpireDeviationRequests(entities.WithSystemContext(h.Ctx()), time.Now()); !errors.Is(err, apierr.ErrInvalidArgument) || n != 0 {
		t.Fatalf("the sweep: %d, %v", n, err)
	}
	if changed := tablesThatDiffer(before, everyRow(t, h)); len(changed) != 0 {
		t.Fatalf("what is not built yet changed %v", changed)
	}
}
