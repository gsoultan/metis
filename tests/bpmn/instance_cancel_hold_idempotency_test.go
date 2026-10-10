package bpmn_test

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"sync"
	"testing"

	"github.com/google/uuid"
	"github.com/gsoultan/metis/internal/pkg/apierr"
	"github.com/gsoultan/metis/server/domains/entities"
	serviceimpl "github.com/gsoultan/metis/server/domains/services/impl"
	"github.com/gsoultan/metis/server/repositories"
	repocontracts "github.com/gsoultan/metis/server/repositories/contracts"
	"github.com/gsoultan/metis/server/repositories/models"
)

// Review Focus 2, for a cancel. A double click, or a client retrying a
// response it lost: two applies of one preview at once. They take turns at
// the instance's lock; the second finds the first's ledger row for the same
// visit and replays it.
//
// The order is made, not hoped for: the first apply is stopped where it holds
// the instance and waits for the task's row, and the second is sent only then.
func TestTwoAppliesOfOnePreviewCancelOnce(t *testing.T) {
	h := newEngineHarness(t, "Cancel Race Project")
	events := &eventLog{}
	h.dispatcher.Register(events)
	w := newWaiver(h)
	id := w.start(t, opsApproval(h.projID, "ops-cancel-race"), nil)
	task := theOpenTask(t, h, id, "opsApprove")
	cmd := w.previewed(t, deviationCommand(entities.DeviationCancel, id, "opsApprove", nil))

	held := h.holdingTheTask(t, task.ID)
	first := w.sendApply(cmd)
	h.waitForWaiters(t, held, 1, first.answered)
	second := w.sendApply(cmd)
	h.waitForWaiters(t, held, 2, first.answered, second.answered)
	held.letGo(t, false)

	acted, err := first.answer(t, "the first apply")
	if err != nil {
		t.Fatalf("the first apply: %v", err)
	}
	replayed, err := second.answer(t, "the second apply")
	if err != nil {
		t.Fatalf("the second apply: %v", err)
	}
	if !acted.Applied || acted.Replayed || acted.Deviation == nil {
		t.Fatalf("the apply that got there first answered %+v, want it to have acted", acted)
	}
	if !replayed.Applied || !replayed.Replayed || replayed.Deviation == nil {
		t.Fatalf("the apply that waited answered %+v, want a replay", replayed)
	}
	if acted.Deviation.ID != replayed.Deviation.ID {
		t.Fatal("the replay answered a different row from the one the apply wrote")
	}
	if said, wrote := rowAsAnswered(t, *replayed.Deviation), rowAsAnswered(t, *acted.Deviation); said != wrote {
		t.Errorf("the replay answered\n %s\nand the apply answered\n %s", said, wrote)
	}
	if replayed.Plan.VisitKey != cmd.VisitKey || replayed.Plan.NodeID != "opsApprove" || replayed.Plan.Kind != entities.DeviationCancel {
		t.Errorf("the replay's plan %+v does not say what was replayed", replayed.Plan)
	}
	if rows := w.ledger(t, id); len(rows) != 1 {
		t.Fatalf("two applies of one preview wrote %d ledger rows", len(rows))
	}
	if told := toldOfWithdrawal(events); !reflect.DeepEqual(told, map[string]int{"ollie": 1}) {
		t.Fatalf("withdrawals were announced to %v, want ollie told once", told)
	}
	if entries := entriesOfType(t, h, id, serviceimpl.EventInstanceCancelled); len(entries) != 1 {
		t.Fatalf("the trail says the instance was ended %d times", len(entries))
	}
	requireInstanceStatus(h.Ctx(), t, h, id, entities.ProcessCancelled)
}

// The same, with nothing arranged: several applies of one preview let go
// together, over and over, for a cancel and for a hold. Whatever order they
// land in, one acts and the rest answer its row.
func TestAppliesOfOnePreviewSentTogetherActOnce(t *testing.T) {
	for _, kind := range []entities.DeviationKind{entities.DeviationCancel, entities.DeviationHold} {
		t.Run(string(kind), func(t *testing.T) {
			h := newEngineHarness(t, "Free Race Project")
			events := &eventLog{}
			h.dispatcher.Register(events)
			w := newWaiver(h)
			h.deploy(t, opsApproval(h.projID, "ops-free-race"))
			const rounds, together = 5, 3
			for round := range rounds {
				id, err := h.svc.StartProcess(h.Ctx(), h.projID, "ops-free-race", nil)
				if err != nil {
					t.Fatalf("round %d: start: %v", round, err)
				}
				cmd := w.previewed(t, deviationCommand(kind, id, "opsApprove", nil))

				var start sync.WaitGroup
				start.Add(1)
				applies := make([]*sent[entities.DeviationOutcome], together)
				for i := range applies {
					applies[i] = send(func() (entities.DeviationOutcome, error) {
						start.Wait()
						inTime, stop := context.WithTimeout(w.ctx, lockWait)
						defer stop()
						return w.svc.DeviateInstance(inTime, cmd)
					})
				}
				start.Done()

				acted, row := 0, uuid.Nil
				for i, apply := range applies {
					out, err := apply.answer(t, "an apply")
					if err != nil {
						t.Fatalf("round %d, apply %d: %v", round, i, err)
					}
					if !out.Applied || out.Deviation == nil {
						t.Fatalf("round %d, apply %d answered %+v", round, i, out)
					}
					if !out.Replayed {
						acted++
					}
					if row != uuid.Nil && out.Deviation.ID != row {
						t.Fatalf("round %d: two applies of one preview answered different rows", round)
					}
					row = out.Deviation.ID
				}
				if acted != 1 {
					t.Fatalf("round %d: %d of %d applies acted, want exactly one", round, acted, together)
				}
				if rows := w.ledger(t, id); len(rows) != 1 {
					t.Fatalf("round %d: %d applies of one preview wrote %d ledger rows", round, together, len(rows))
				}
				incidents, err := h.svc.ListIncidents(h.Ctx(), id)
				if err != nil {
					t.Fatalf("round %d: read the incidents: %v", round, err)
				}
				wantIncidents, wantWithdrawals := 0, round+1
				if kind == entities.DeviationHold {
					wantIncidents, wantWithdrawals = 1, 0
				}
				if len(incidents) != wantIncidents {
					t.Fatalf("round %d: the instance has %d incident(s), want %d", round, len(incidents), wantIncidents)
				}
				if withdrawn := events.ofType(entities.EventTaskCanceled); len(withdrawn) != wantWithdrawals {
					t.Fatalf("round %d: %d withdrawal(s) announced in all, want %d", round, len(withdrawn), wantWithdrawals)
				}
			}
		})
	}
}

// Two holds of one preview, both waiting for the instance while somebody else
// has it. A hold takes no row but the instance's, so that is where they take
// turns: one raises the incident, the other is answered with its row.
func TestTwoAppliesOfOnePreviewHoldOnce(t *testing.T) {
	h := newEngineHarness(t, "Hold Race Project")
	w := newWaiver(h)
	id := w.start(t, opsApproval(h.projID, "ops-hold-race"), nil)
	cmd := w.previewed(t, deviationCommand(entities.DeviationHold, id, "opsApprove", nil))

	busy := h.holding(t, `UPDATE process_instances SET status = status WHERE id = ?`, id)
	first := w.sendApply(cmd)
	second := w.sendApply(cmd)
	h.waitForWaiters(t, busy, 2, first.answered, second.answered)
	busy.letGo(t, false)

	acted := 0
	var row uuid.UUID
	for i, apply := range []*sent[entities.DeviationOutcome]{first, second} {
		out, err := apply.answer(t, "a hold")
		if err != nil || !out.Applied || out.Deviation == nil {
			t.Fatalf("hold %d: %+v, %v", i+1, out, err)
		}
		if !out.Replayed {
			acted++
		}
		if row != uuid.Nil && out.Deviation.ID != row {
			t.Fatal("two holds of one preview answered different rows")
		}
		row = out.Deviation.ID
	}
	if acted != 1 {
		t.Fatalf("%d of two holds of one preview acted, want exactly one", acted)
	}
	if incidents, err := h.svc.ListIncidents(h.Ctx(), id); err != nil || len(incidents) != 1 {
		t.Fatalf("two holds of one preview left %d incident(s) (err %v), want one", len(incidents), err)
	}
	if rows := w.ledger(t, id); len(rows) != 1 {
		t.Fatalf("two holds of one preview wrote %d ledger rows", len(rows))
	}
	if entries := entriesOfType(t, h, id, serviceimpl.EventInstanceHeld); len(entries) != 1 {
		t.Fatalf("the trail says the instance was held %d times", len(entries))
	}
}

// The record names who held the work when it was taken, not who the preview
// showed. carol's claim of the task is written and not yet committed when the
// cancel arrives: the cancel has the instance and waits for the task's row,
// and what it then records and announces is what the row says once it has it.
// The plan in its answer was made before that and shows nobody holding the
// task, which is why the record is not made from it.
func TestACancelRecordsWhoHeldTheWorkWhenItWasTaken(t *testing.T) {
	h := newEngineHarness(t, "Cancel Holder Project")
	events := &eventLog{}
	h.dispatcher.Register(events)
	h.recordsAsProductionDoes()
	w := newWaiver(h)
	id := h.startWaiting(t, "cancel-holder")
	task := theOpenTask(t, h, id, "review")
	if task.AssigneeUsername() != "" {
		t.Fatalf("the task is %s's before anybody claimed it; this test needs it to be nobody's", task.AssigneeUsername())
	}
	cmd := w.previewed(t, deviationCommand(entities.DeviationCancel, id, "review", nil))

	claim := h.holding(t, `UPDATE tasks SET status = 'claimed', assignee = 'carol' WHERE id = ?`, task.ID)
	apply := w.sendApply(cmd)
	h.waitForWaiters(t, claim, 1, apply.answered)
	claim.letGo(t, true)

	out, err := apply.answer(t, "the apply")
	if err != nil || !out.Applied || out.Replayed {
		t.Fatalf("the cancel, once the claim was let go: %+v, %v", out, err)
	}
	if len(out.Plan.OpenWork) != 1 || out.Plan.OpenWork[0].Assignee != "" {
		t.Fatalf("the plan the apply made shows %+v; it was made before the claim landed, and this test rests on that", out.Plan.OpenWork)
	}
	requireInstanceStatus(h.Ctx(), t, h, id, entities.ProcessCancelled)
	rows := w.ledger(t, id)
	if len(rows) != 1 {
		t.Fatalf("the ledger holds %d rows, want one", len(rows))
	}
	was := tasksSection(t, rows[0].Before)
	if was[task.ID.String()]["assignee"] != "carol" || was[task.ID.String()]["status"] != string(entities.TaskClaimed) {
		t.Errorf("the ledger says the task was %v when it was withdrawn; carol had claimed it", was[task.ID.String()])
	}
	// The task's own row says the same: withdrawn, and carol's.
	if now, err := h.svc.GetTask(h.Ctx(), task.ID); err != nil || now.Status != entities.TaskCanceled || now.AssigneeUsername() != "carol" {
		t.Errorf("the task is %q with %q (%v), want it withdrawn and carol's", now.Status, now.AssigneeUsername(), err)
	}
	if told := toldOfWithdrawal(events); !reflect.DeepEqual(told, map[string]int{"carol": 1}) {
		t.Errorf("the withdrawal was announced to %v, want carol alone, once", told)
	}
	if notices := withdrawalNotices(t, h, "carol"); notices != 1 {
		t.Errorf("carol has %d notice(s) that a task was withdrawn, want one", notices)
	}
}

// storeFailing is the repository with one of its stores replaced by one that
// fails: how a test has the database fail in the middle of an act.
type storeFailing struct {
	repositories.Repository
	incidents     repocontracts.IncidentRepository
	subscriptions repocontracts.SubscriptionRepository
	deviations    repocontracts.DeviationRepository
}

func (r storeFailing) Incident() repocontracts.IncidentRepository {
	if r.incidents != nil {
		return r.incidents
	}
	return r.Repository.Incident()
}

func (r storeFailing) Subscription() repocontracts.SubscriptionRepository {
	if r.subscriptions != nil {
		return r.subscriptions
	}
	return r.Repository.Subscription()
}

func (r storeFailing) Deviation() repocontracts.DeviationRepository {
	if r.deviations != nil {
		return r.deviations
	}
	return r.Repository.Deviation()
}

// incidentsNotRaised reads incidents and fails to raise one.
type incidentsNotRaised struct {
	repocontracts.IncidentRepository
	err error
}

func (r incidentsNotRaised) Create(context.Context, models.IncidentModel) (models.IncidentModel, error) {
	return models.IncidentModel{}, r.err
}

// subscriptionsNotRead fails to read what an instance is waiting for.
type subscriptionsNotRead struct {
	repocontracts.SubscriptionRepository
	err error
}

func (r subscriptionsNotRead) ListByInstance(context.Context, uuid.UUID) ([]models.Subscription, error) {
	return nil, r.err
}

// ledgerNotWritten reads the ledger and fails to write to it.
type ledgerNotWritten struct {
	repocontracts.DeviationRepository
	err error
}

func (r ledgerNotWritten) Create(context.Context, entities.Deviation) (entities.Deviation, error) {
	return entities.Deviation{}, r.err
}

// By the time a cancel or a hold acts, the caller has been let in, the
// instance found and the plan accepted: whatever fails after that is the
// server's, and is answered as that — never as "no such instance" or as
// something the caller typed, whatever class the failure came with. And it is
// undone whole: a cancel that withdrew the task and then failed leaves the
// task open, the instance running, and no record of an act that was not made.
func TestACancelOrAHoldThatFailsIsAnsweredAsTheServersAndUndone(t *testing.T) {
	h := newEngineHarness(t, "Act Fails Project")
	h.recordsAsProductionDoes()
	w := newWaiver(h)
	ctx := h.Ctx()
	id := w.start(t, opsApproval(h.projID, "ops-act-fails"), nil)
	notFound := fmt.Errorf("the store lost it: %w", apierr.ErrNotFound)
	invalid := apierr.Invalidf("the store took it for something somebody typed")
	plain := errors.New("the ledger is not there")

	for what, broken := range map[string]struct {
		kind  entities.DeviationKind
		store repositories.Repository
		says  string
	}{
		"a hold whose incident cannot be raised": {entities.DeviationHold,
			storeFailing{Repository: h.repo, incidents: incidentsNotRaised{h.repo.Incident(), notFound}},
			"holding this instance at “Operations approve”: "},
		"a cancel that cannot read what the instance waits for": {entities.DeviationCancel,
			storeFailing{Repository: h.repo, subscriptions: subscriptionsNotRead{h.repo.Subscription(), invalid}},
			"cancelling this instance: "},
		"a cancel that cannot be recorded": {entities.DeviationCancel,
			storeFailing{Repository: h.repo, deviations: ledgerNotWritten{h.repo.Deviation(), plain}},
			"recording that this instance was cancelled: "},
		"a hold that cannot be recorded": {entities.DeviationHold,
			storeFailing{Repository: h.repo, deviations: ledgerNotWritten{h.repo.Deviation(), plain}},
			"recording that this instance was held at “Operations approve”: "},
	} {
		failing := waiver{h: h, svc: serviceimpl.NewInstanceDeviationService(broken.store, h.engine), ctx: w.ctx}
		cmd := failing.previewed(t, deviationCommand(broken.kind, id, "opsApprove", nil))
		before := everyRow(t, h)

		out, err := failing.svc.DeviateInstance(failing.ctx, cmd)
		if err == nil || out.Applied || out.Replayed || out.Deviation != nil {
			t.Fatalf("%s: %+v, %v; want it to fail", what, out, err)
		}
		for class, name := range map[error]string{apierr.ErrNotFound: "not found", apierr.ErrInvalidArgument: "invalid", apierr.ErrForbidden: "forbidden"} {
			if errors.Is(err, class) {
				t.Errorf("%s is answered as %s: %v", what, name, err)
			}
		}
		if !strings.HasPrefix(err.Error(), broken.says) {
			t.Errorf("%s is told as %q; want it to begin %q", what, err, broken.says)
		}
		if changed := tablesThatDiffer(before, everyRow(t, h)); len(changed) != 0 {
			t.Fatalf("%s changed %v", what, changed)
		}
	}
	requireInstanceStatus(ctx, t, h, id, entities.ProcessActive)
	if open := theOpenTask(t, h, id, "opsApprove"); open.AssigneeUsername() != "ollie" {
		t.Fatalf("after the failed acts the task is with %q, want ollie still", open.AssigneeUsername())
	}
	// Nothing of a failed act is left to be replayed: the same commands, with
	// a store that works, act.
	w.mustApply(t, deviationCommand(entities.DeviationHold, id, "opsApprove", nil))
	w.mustApply(t, deviationCommand(entities.DeviationCancel, id, "opsApprove", nil))
	requireInstanceStatus(ctx, t, h, id, entities.ProcessCancelled)
}
