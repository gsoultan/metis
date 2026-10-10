package bpmn_test

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/gsoultan/metis/internal/pkg/apierr"
	pkgauth "github.com/gsoultan/metis/internal/pkg/auth"
	"github.com/gsoultan/metis/server/domains/entities"
	servicecontracts "github.com/gsoultan/metis/server/domains/services/contracts"
	serviceimpl "github.com/gsoultan/metis/server/domains/services/impl"
	"github.com/gsoultan/metis/server/repositories/models"
	"github.com/gsoultan/metis/tests/testutils"
)

func refusalMentions(plan entities.DeviationPlan, words ...string) bool {
	joined := strings.ToLower(strings.Join(plan.Refusals, " | "))
	for _, word := range words {
		if !strings.Contains(joined, strings.ToLower(word)) {
			return false
		}
	}
	return true
}

// Each case is a place a waive would have to guess — a branch, a value, work
// that is not a person's — or would record a step nobody can account for.
// The preview says so in words and the apply is refused.
func TestAWaiveIsRefusedWhereItWouldGuess(t *testing.T) {
	h := newEngineHarness(t, "Waive Refusals Project")
	w := newWaiver(h)
	ctx := h.Ctx()

	h.deploy(t, opsApproval(h.projID, "refusals-ops"))
	parked, err := h.svc.StartProcess(ctx, h.projID, "refusals-ops", nil)
	if err != nil {
		t.Fatalf("start: %v", err)
	}
	h.deploy(t, &entities.ProcessDefinition{
		Project: &entities.Project{ID: h.projID}, Key: "refusals-fork",
		Nodes: []*entities.Node{
			{ID: "start", Type: entities.StartEvent},
			{ID: "pick", Type: entities.UserTask, Name: "Pick a supplier", Assignee: "rita", Properties: testutils.FormDeclaring("supplier")},
			{ID: "a", Type: entities.UserTask, Name: "Order from A"},
			{ID: "b", Type: entities.UserTask, Name: "Order from B"},
		},
		Flows: []*entities.SequenceFlow{
			{ID: "f1", SourceRef: "start", TargetRef: "pick"},
			{ID: "fa", SourceRef: "pick", TargetRef: "a"},
			{ID: "fb", SourceRef: "pick", TargetRef: "b"},
		},
	})
	forked, err := h.svc.StartProcess(ctx, h.projID, "refusals-fork", nil)
	if err != nil {
		t.Fatalf("start the fork: %v", err)
	}
	h.deploy(t, &entities.ProcessDefinition{
		Project: &entities.Project{ID: h.projID}, Key: "refusals-worker",
		Nodes: []*entities.Node{
			{ID: "start", Type: entities.StartEvent},
			{ID: "screen", Type: entities.ServiceTask, Name: "Screen the supplier", ExternalTopic: "screening"},
			{ID: "end", Type: entities.EndEvent},
		},
		Flows: []*entities.SequenceFlow{
			{ID: "f1", SourceRef: "start", TargetRef: "screen"},
			{ID: "f2", SourceRef: "screen", TargetRef: "end"},
		},
	})
	worker, err := h.svc.StartProcess(ctx, h.projID, "refusals-worker", nil)
	if err != nil {
		t.Fatalf("start the worker process: %v", err)
	}

	cases := []struct {
		name     string
		command  entities.DeviationCommand
		mentions []string
	}{
		{"no reason", func() entities.DeviationCommand {
			c := deviationCommand(entities.DeviationWaive, parked, "opsApprove", nil)
			c.Reason = "   "
			return c
		}(), []string{"Say why: a reason is required, and it is kept with the record."}},
		{"a step the instance is not waiting at", deviationCommand(entities.DeviationWaive, parked, "salesApprove", nil), []string{"not waiting at", "Sales approve"}},
		{"a step the process does not have", deviationCommand(entities.DeviationWaive, parked, "nowhere", nil), []string{"no step"}},
		{"a step with two ways out", deviationCommand(entities.DeviationWaive, forked, "pick", nil), []string{"2 ways out", "Pick a supplier"}},
		{"work done by an outside worker", deviationCommand(entities.DeviationWaive, worker, "screen", nil), []string{"Screen the supplier", "retry"}},
	}
	before := everyRow(t, h)
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			plan := w.preview(t, c.command)
			if plan.Applicable() || !refusalMentions(plan, c.mentions...) {
				t.Fatalf("the plan's refusals %q do not refuse it with %q", plan.Refusals, c.mentions)
			}
			if c.name == "no reason" && !reflect.DeepEqual(plan.Refusals, c.mentions) {
				t.Errorf("a waive with no reason is refused with %q, want only %q", plan.Refusals, c.mentions)
			}
			// And the apply is refused in the preview's words, as something
			// the caller can fix, with nothing changed.
			out, err := w.apply(t, c.command)
			if !errors.Is(err, apierr.ErrInvalidArgument) || out.Applied || out.Deviation != nil {
				t.Fatalf("applied: %+v, %v; want it refused as something the caller can fix", out, err)
			}
			for _, refusal := range plan.Refusals {
				if !strings.Contains(err.Error(), refusal) {
					t.Errorf("the apply was told %q; it does not say what the preview said: %q", err, refusal)
				}
			}
		})
	}
	if changed := tablesThatDiffer(before, everyRow(t, h)); len(changed) != 0 {
		t.Fatalf("refused applies changed %v", changed)
	}

	// Finished: nothing to waive, and the words say why.
	open := openIterationTasks(ctx, t, h, parked, "opsApprove")
	if err := h.svc.CompleteTask(ctx, open[0].ID, "ollie", map[string]any{"approved": true}); err != nil {
		t.Fatalf("complete the operations approval: %v", err)
	}
	completeTaskAt(ctx, t, h, parked, "salesApprove", nil)
	if plan := w.preview(t, deviationCommand(entities.DeviationWaive, parked, "salesApprove", nil)); !refusalMentions(plan, "completed") {
		t.Fatalf("a finished instance: refusals %q", plan.Refusals)
	}
	if rows := w.ledger(t, parked); len(rows) != 0 {
		t.Fatalf("refused commands wrote %d ledger row(s)", len(rows))
	}
}

// engineThatCannotEndAStep is the engine without the one method a waive needs:
// what a wiring that wraps the engine hands the service.
type engineThatCannotEndAStep struct {
	servicecontracts.ExecutionEngine
}

// Every refusal is a sentence a process owner can read and act on, and names
// the step by its name. They are pinned word for word: they are the product.
func TestEveryRefusalSaysWhatIsWrongInWordsSomebodyCanActOn(t *testing.T) {
	h := newEngineHarness(t, "Waive Sentences Project")
	w := newWaiver(h)
	ctx := h.Ctx()

	parked := w.start(t, opsApproval(h.projID, "sentences-ops"), nil)
	forked := w.start(t, &entities.ProcessDefinition{
		Project: &entities.Project{ID: h.projID}, Key: "sentences-fork",
		Nodes: []*entities.Node{
			{ID: "start", Type: entities.StartEvent},
			{ID: "pick", Type: entities.UserTask, Name: "Pick a supplier", Assignee: "rita", Properties: testutils.FormDeclaring("supplier")},
			{ID: "a", Type: entities.UserTask, Name: "Order from A"},
			{ID: "b", Type: entities.UserTask, Name: "Order from B"},
		},
		Flows: []*entities.SequenceFlow{
			{ID: "f1", SourceRef: "start", TargetRef: "pick"},
			{ID: "fa", SourceRef: "pick", TargetRef: "a"},
			{ID: "fb", SourceRef: "pick", TargetRef: "b"},
		},
	}, nil)
	worker := w.start(t, &entities.ProcessDefinition{
		Project: &entities.Project{ID: h.projID}, Key: "sentences-worker",
		Nodes: []*entities.Node{
			{ID: "start", Type: entities.StartEvent},
			{ID: "screen", Type: entities.ServiceTask, Name: "Screen the supplier", ExternalTopic: "screening"},
			{ID: "end", Type: entities.EndEvent},
		},
		Flows: []*entities.SequenceFlow{{ID: "f1", SourceRef: "start", TargetRef: "screen"}, {ID: "f2", SourceRef: "screen", TargetRef: "end"}},
	}, nil)
	h.deploy(t, &entities.ProcessDefinition{
		Project: &entities.Project{ID: h.projID}, Key: "sentences-called",
		Nodes: []*entities.Node{
			{ID: "start", Type: entities.StartEvent},
			{ID: "review", Type: entities.UserTask, Name: "Review the supplier", Assignee: "rita"},
			{ID: "end", Type: entities.EndEvent},
		},
		Flows: []*entities.SequenceFlow{{ID: "c1", SourceRef: "start", TargetRef: "review"}, {ID: "c2", SourceRef: "review", TargetRef: "end"}},
	})
	caller := w.start(t, &entities.ProcessDefinition{
		Project: &entities.Project{ID: h.projID}, Key: "sentences-caller",
		Nodes: []*entities.Node{
			{ID: "start", Type: entities.StartEvent},
			{ID: "check", Type: entities.CallActivity, Name: "Check the supplier", Properties: map[string]any{"called_process_key": "sentences-called"}},
			{ID: "end", Type: entities.EndEvent},
		},
		Flows: []*entities.SequenceFlow{{ID: "p1", SourceRef: "start", TargetRef: "check"}, {ID: "p2", SourceRef: "check", TargetRef: "end"}},
	}, nil)
	called, err := h.svc.ListSubProcesses(ctx, caller)
	if err != nil || len(called) != 1 {
		t.Fatalf("called processes: %d (err %v)", len(called), err)
	}
	waitingForASignal := w.start(t, &entities.ProcessDefinition{
		Project: &entities.Project{ID: h.projID}, Key: "sentences-signal",
		Nodes: []*entities.Node{
			{ID: "start", Type: entities.StartEvent},
			{ID: "await", Type: entities.IntermediateCatchEvent, Name: "Wait for the board", Properties: map[string]any{"signal_name": "BoardMet"}},
			{ID: "end", Type: entities.EndEvent},
		},
		Flows: []*entities.SequenceFlow{{ID: "s1", SourceRef: "start", TargetRef: "await"}, {ID: "s2", SourceRef: "await", TargetRef: "end"}},
	}, nil)
	// A step nothing follows: the validator accepts it.
	noWayOut := w.start(t, &entities.ProcessDefinition{
		Project: &entities.Project{ID: h.projID}, Key: "sentences-no-way-out",
		Nodes: []*entities.Node{
			{ID: "start", Type: entities.StartEvent},
			{ID: "check", Type: entities.UserTask, Name: "Check the order", Assignee: "rita"},
		},
		Flows: []*entities.SequenceFlow{{ID: "d1", SourceRef: "start", TargetRef: "check"}},
	}, nil)

	done := w.start(t, opsApproval(h.projID, "sentences-finished"), nil)
	// A call activity the instance has not reached: nothing is running under
	// it to point at.
	notYetCalling := w.start(t, &entities.ProcessDefinition{
		Project: &entities.Project{ID: h.projID}, Key: "sentences-not-yet-calling",
		Nodes: []*entities.Node{
			{ID: "start", Type: entities.StartEvent},
			{ID: "prepare", Type: entities.UserTask, Name: "Prepare the check", Assignee: "rita"},
			{ID: "check", Type: entities.CallActivity, Name: "Check the supplier", Properties: map[string]any{"called_process_key": "sentences-called"}},
			{ID: "end", Type: entities.EndEvent},
		},
		Flows: []*entities.SequenceFlow{
			{ID: "n1", SourceRef: "start", TargetRef: "prepare"}, {ID: "n2", SourceRef: "prepare", TargetRef: "check"}, {ID: "n3", SourceRef: "check", TargetRef: "end"},
		},
	}, nil)
	// A step the instance waits at with no task open on it. Nothing in the
	// product withdraws one task and leaves its token, so the row is written
	// through the repository; the plan must still not waive a step nobody has.
	taskless := w.start(t, opsApproval(h.projID, "sentences-taskless"), nil)
	if err := h.repo.Task().UpdateStatus(ctx, openIterationTasks(ctx, t, h, taskless, "opsApprove")[0].ID, models.TaskCanceled); err != nil {
		t.Fatalf("withdraw the task: %v", err)
	}
	suspended := w.start(t, opsApproval(h.projID, "sentences-suspended"), nil)
	suspend(t, h, suspended)

	saying := func(reason string) func(entities.DeviationCommand) entities.DeviationCommand {
		return func(c entities.DeviationCommand) entities.DeviationCommand {
			c.Reason = reason
			return c
		}
	}
	asIs := func(c entities.DeviationCommand) entities.DeviationCommand { return c }
	cases := []struct {
		name    string
		command entities.DeviationCommand
		edit    func(entities.DeviationCommand) entities.DeviationCommand
		want    string
	}{
		{"no reason", deviationCommand(entities.DeviationWaive, parked, "opsApprove", nil), saying(" \n\t "),
			"Say why: a reason is required, and it is kept with the record."},
		{"a hold with no reason", deviationCommand(entities.DeviationHold, parked, "opsApprove", nil), saying(""),
			"Say why: a reason is required, and it is kept with the record."},
		{"a cancel with no reason", deviationCommand(entities.DeviationCancel, parked, "opsApprove", nil), saying(""),
			"Say why: a reason is required, and it is kept with the record."},
		{"a reason of 2001 characters", deviationCommand(entities.DeviationWaive, parked, "opsApprove", nil), saying(strings.Repeat("é", 2001)),
			"The reason is longer than 2000 characters; say it more briefly."},
		{"a step the instance is not waiting at", deviationCommand(entities.DeviationWaive, parked, "salesApprove", nil), asIs,
			"This instance is not waiting at “Sales approve”."},
		{"a hold at a step the instance is not waiting at", deviationCommand(entities.DeviationHold, parked, "salesApprove", nil), asIs,
			"This instance is not waiting at “Sales approve”."},
		{"a cancel at a step the instance is not waiting at", deviationCommand(entities.DeviationCancel, parked, "salesApprove", nil), asIs,
			"This instance is not waiting at “Sales approve”."},
		{"a step nobody has to do", deviationCommand(entities.DeviationWaive, parked, "salesApprove", nil), asIs,
			"Nobody has “Sales approve” to do, so there is nothing to waive."},
		{"a step the process does not have", deviationCommand(entities.DeviationWaive, parked, "nowhere", nil), asIs,
			`This process has no step "nowhere".`},
		{"a step with two ways out", deviationCommand(entities.DeviationWaive, forked, "pick", nil), asIs,
			"“Pick a supplier” has 2 ways out, so waiving it would choose a branch on the business's behalf."},
		{"a step with no way out", deviationCommand(entities.DeviationWaive, noWayOut, "check", nil), asIs,
			"“Check the order” has no way out, so there is nowhere for the instance to go once it is waived."},
		{"work done by an outside worker", deviationCommand(entities.DeviationWaive, worker, "screen", nil), asIs,
			"“Screen the supplier” is work for a system, not a person; retry it or resolve its incident instead of waiving it."},
		{"a step that runs another process", deviationCommand(entities.DeviationWaive, caller, "check", nil), asIs,
			"“Check the supplier” runs another process; waive the step inside that process (instance " + called[0].ID.String() + ") instead."},
		{"a step that waits for something to happen", deviationCommand(entities.DeviationWaive, waitingForASignal, "await", nil), asIs,
			"“Wait for the board” is not work somebody does; hold the instance instead."},
		{"values the step's form does not declare", deviationCommand(entities.DeviationWaive, parked, "opsApprove",
			map[string]any{"zeta": 1, "approved": true, "amount": 900}), asIs,
			"“Operations approve”'s form does not declare amount, zeta, so a waiver cannot set them."},
		{"one value the step's form does not declare", deviationCommand(entities.DeviationWaive, parked, "opsApprove",
			map[string]any{"approved": true, "amount": 900}), asIs,
			"“Operations approve”'s form does not declare amount, so a waiver cannot set it."},
		{"a step that runs another process, before the instance reaches it", deviationCommand(entities.DeviationWaive, notYetCalling, "check", nil), asIs,
			"“Check the supplier” runs another process; waive the step inside that process instead."},
		{"a step the instance waits at with no task open on it", deviationCommand(entities.DeviationWaive, taskless, "opsApprove", nil), asIs,
			"Nobody has “Operations approve” to do, so there is nothing to waive."},
		{"a hold of an instance that is suspended", deviationCommand(entities.DeviationHold, suspended, "opsApprove", nil), asIs,
			"This instance is suspended; only a running instance can be held."},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			plan := w.preview(t, c.edit(c.command))
			if plan.Applicable() || !said(plan.Refusals, c.want) {
				t.Fatalf("the plan does not refuse with\n  %s\nits refusals:%s", c.want, lines(plan.Refusals))
			}
			if plan.VisitKey == "" {
				t.Error("a refused plan has no visit key, so the apply that would be refused the same way cannot name one")
			}
		})
	}
	// The step the instance waits at with no task: waiting there is not what
	// is wrong with it.
	if plan := w.preview(t, deviationCommand(entities.DeviationWaive, taskless, "opsApprove", nil)); said(plan.Refusals, "This instance is not waiting at “Operations approve”.") {
		t.Errorf("an instance waiting at a step with no task was told it is not waiting there:%s", lines(plan.Refusals))
	}

	t.Run("a reason of exactly 2000 characters, counted as characters", func(t *testing.T) {
		cmd := deviationCommand(entities.DeviationWaive, parked, "opsApprove", map[string]any{"approved": true})
		cmd.Reason = "  " + strings.Repeat("é", 2000) + "  "
		if plan := w.preview(t, cmd); !plan.Applicable() {
			t.Fatalf("refused:%s", lines(plan.Refusals))
		}
	})
	t.Run("a hold and a cancel ask nothing of the kind of step", func(t *testing.T) {
		for _, kind := range []entities.DeviationKind{entities.DeviationHold, entities.DeviationCancel} {
			if plan := w.preview(t, deviationCommand(kind, worker, "screen", nil)); !plan.Applicable() {
				t.Errorf("a %s at a step done by a worker was refused:%s", kind, lines(plan.Refusals))
			}
		}
	})
	t.Run("an engine that cannot end a step", func(t *testing.T) {
		wrapped := w
		wrapped.svc = serviceimpl.NewInstanceDeviationService(h.repo, engineThatCannotEndAStep{h.engine})
		plan := wrapped.preview(t, deviationCommand(entities.DeviationWaive, parked, "opsApprove", map[string]any{"approved": true}))
		if want := "Waiving needs the execution engine, and this server was wired without it."; !said(plan.Refusals, want) {
			t.Fatalf("the plan does not refuse with\n  %s\nits refusals:%s", want, lines(plan.Refusals))
		}
		for _, kind := range []entities.DeviationKind{entities.DeviationHold, entities.DeviationCancel} {
			if plan := wrapped.preview(t, deviationCommand(kind, parked, "opsApprove", nil)); !plan.Applicable() {
				t.Errorf("a %s needs no step ended, and was refused:%s", kind, lines(plan.Refusals))
			}
		}
	})
	t.Run("an instance that has finished", func(t *testing.T) {
		open := openIterationTasks(ctx, t, h, done, "opsApprove")
		if err := h.svc.CompleteTask(ctx, open[0].ID, "ollie", map[string]any{"approved": true}); err != nil {
			t.Fatalf("complete the operations approval: %v", err)
		}
		completeTaskAt(ctx, t, h, done, "salesApprove", nil)
		for kind, want := range map[entities.DeviationKind]string{
			entities.DeviationWaive:  "This instance is completed; only a running instance can be waived.",
			entities.DeviationCancel: "This instance is completed; only a running instance can be cancelled.",
			entities.DeviationHold:   "This instance is completed; only a running instance can be held.",
		} {
			if plan := w.preview(t, deviationCommand(kind, done, "salesApprove", nil)); !said(plan.Refusals, want) {
				t.Errorf("the plan does not refuse with\n  %s\nits refusals:%s", want, lines(plan.Refusals))
			}
		}
		// A cancel that names no step is refused for the same reason, and is
		// not told that cancelling closes it: it is closed.
		plan := w.preview(t, deviationCommand(entities.DeviationCancel, done, "", nil))
		if !said(plan.Refusals, "This instance is completed; only a running instance can be cancelled.") || len(plan.Warnings) != 0 {
			t.Errorf("a cancel of a finished instance: refusals:%s\nwarnings:%s", lines(plan.Refusals), lines(plan.Warnings))
		}
	})
}

// A command that is not one of the three, or carries outputs a cancel cannot
// use, is a malformed request rather than a plan.
func TestAMalformedCommandIsRefusedBeforeAnythingIsPlanned(t *testing.T) {
	h := newEngineHarness(t, "Waive Malformed Project")
	w := newWaiver(h)
	h.deploy(t, opsApproval(h.projID, "malformed-ops"))
	id, err := h.svc.StartProcess(h.Ctx(), h.projID, "malformed-ops", nil)
	if err != nil {
		t.Fatalf("start: %v", err)
	}
	skip := deviationCommand("skip", id, "opsApprove", nil)
	skip.DryRun = true
	if _, err := w.svc.DeviateInstance(w.ctx, skip); !errors.Is(err, apierr.ErrInvalidArgument) {
		t.Fatalf("kind skip: %v", err)
	}
	cancelWithOutputs := deviationCommand(entities.DeviationCancel, id, "opsApprove", map[string]any{"approved": true})
	cancelWithOutputs.DryRun = true
	if _, err := w.svc.DeviateInstance(w.ctx, cancelWithOutputs); !errors.Is(err, apierr.ErrInvalidArgument) {
		t.Fatalf("a cancel carrying outputs: %v", err)
	}
	tooMany := map[string]any{}
	for i := range 51 {
		tooMany[uuid.NewString()[:8]+string(rune('a'+i%26))] = i
	}
	big := deviationCommand(entities.DeviationWaive, id, "opsApprove", tooMany)
	big.DryRun = true
	if _, err := w.svc.DeviateInstance(w.ctx, big); !errors.Is(err, apierr.ErrInvalidArgument) {
		t.Fatalf("51 outputs: %v", err)
	}
	holdWithOutputs := deviationCommand(entities.DeviationHold, id, "opsApprove", map[string]any{"approved": true})
	holdWithOutputs.DryRun = true
	if _, err := w.svc.DeviateInstance(w.ctx, holdWithOutputs); !errors.Is(err, apierr.ErrInvalidArgument) {
		t.Fatalf("a hold carrying outputs: %v", err)
	}
	// A waive and a hold act on a step, so one that names none is malformed.
	for _, kind := range []entities.DeviationKind{entities.DeviationWaive, entities.DeviationHold} {
		noStep := deviationCommand(kind, id, "", nil)
		noStep.DryRun = true
		if _, err := w.svc.DeviateInstance(w.ctx, noStep); !errors.Is(err, apierr.ErrInvalidArgument) {
			t.Fatalf("a %s that names no step: %v", kind, err)
		}
	}
	// A cancel may name none: that is a plan, not a malformed request. On an
	// instance that is waiting somewhere the plan refuses it and says where.
	if plan := w.preview(t, deviationCommand(entities.DeviationCancel, id, "", nil)); plan.Applicable() || !refusalMentions(plan, "waiting at", "Operations approve") {
		t.Fatalf("a cancel that names no step, on an instance that is waiting: refusals %q", plan.Refusals)
	}

	// A value given as nothing is not a value: a gateway reading it would
	// decide on nothing, with the plan saying it had been supplied.
	null := deviationCommand(entities.DeviationWaive, id, "opsApprove", map[string]any{"approved": nil})
	null.DryRun = true
	if _, err := w.svc.DeviateInstance(w.ctx, null); !errors.Is(err, apierr.ErrInvalidArgument) || !strings.Contains(err.Error(), "approved") {
		t.Fatalf("an output given as null: %v, want it refused naming the field", err)
	}
	heavy := deviationCommand(entities.DeviationWaive, id, "opsApprove", map[string]any{"approved": strings.Repeat("y", 64<<10)})
	heavy.DryRun = true
	if _, err := w.svc.DeviateInstance(w.ctx, heavy); !errors.Is(err, apierr.ErrInvalidArgument) {
		t.Fatalf("outputs over 64 KiB: %v", err)
	}
	// An apply names the plan it previewed. Without one it is refused whatever
	// else is true of it, and nothing is changed.
	noKey := deviationCommand(entities.DeviationWaive, id, "opsApprove", map[string]any{"approved": true})
	if _, err := w.svc.DeviateInstance(w.ctx, noKey); !errors.Is(err, apierr.ErrInvalidArgument) || !strings.Contains(err.Error(), "preview first") {
		t.Fatalf("an apply that names no visit key: %v, want it told to preview first", err)
	}
	if open := openIterationTasks(h.Ctx(), t, h, id, "opsApprove"); len(open) != 1 {
		t.Fatalf("a malformed command left %d open task(s) on the step, want the one it started with", len(open))
	}
	if rows := w.ledger(t, id); len(rows) != 0 {
		t.Fatalf("malformed commands wrote %d ledger row(s)", len(rows))
	}
}

// The route is administrators' only, and so is the service: a fast path that
// skips the check is a vulnerability (AGENTS.md §2.3).
func TestTheServiceRefusesAnyoneButAnAdministrator(t *testing.T) {
	h := newEngineHarness(t, "Waive Authority Project")
	w := newWaiver(h)
	h.deploy(t, opsApproval(h.projID, "authority-ops"))
	id, err := h.svc.StartProcess(h.Ctx(), h.projID, "authority-ops", nil)
	if err != nil {
		t.Fatalf("start: %v", err)
	}
	cmd := deviationCommand(entities.DeviationWaive, id, "opsApprove", map[string]any{"approved": true})
	cmd.DryRun = true
	for name, ctx := range map[string]context.Context{
		"nobody signed in": h.Ctx(),
		"an operator":      testutils.AsOperator(h.Ctx(), "olga"),
	} {
		if _, err := w.svc.DeviateInstance(ctx, cmd); !errors.Is(err, apierr.ErrForbidden) {
			t.Errorf("%s: got %v, want forbidden", name, err)
		}
	}
}

// signedInAs is ctx as the auth interceptor leaves a request from account.
func signedInAs(ctx context.Context, account entities.User) context.Context {
	return context.WithValue(ctx, pkgauth.UserContextKey, account)
}

// Roles are held in one organization or in all of them, and the command is an
// administrator's of the organization the instance belongs to. Everybody else
// is refused before anything is read, with the same answer whether or not the
// instance exists — and an administrator of another organization, who may ask,
// is told there is no such instance, which is what they would be told of one
// that never existed.
func TestOnlyAnAdministratorOfTheInstancesOrganizationIsAnswered(t *testing.T) {
	h := newEngineHarness(t, "Waive Organizations Project")
	w := newWaiver(h)
	id := w.start(t, opsApproval(h.projID, "organizations-ops"), nil)
	here := entities.ActingOrganization(h.Ctx())
	elsewhere, err := h.svc.CreateOrganization(t.Context(), "Another Organization", "")
	if err != nil {
		t.Fatalf("create the other organization: %v", err)
	}
	inTheOther := entities.WithTenantContext(t.Context(), entities.TenantContext{TenantID: elsewhere.ID.String()})
	before := everyRow(t, h)

	preview := deviationCommand(entities.DeviationWaive, id, "opsApprove", map[string]any{"approved": true})
	preview.DryRun = true
	apply := preview
	apply.DryRun, apply.VisitKey = false, w.preview(t, preview).VisitKey
	nowhere := preview
	nowhere.InstanceID = uuid.Must(uuid.NewV7())

	refused := map[string]context.Context{
		"nobody signed in": h.Ctx(),
		"a member":         signedInAs(h.Ctx(), entities.User{Username: "mia", Roles: []string{entities.RoleUser}}),
		"an operator":      testutils.AsOperator(h.Ctx(), "olga"),
		"a designer":       signedInAs(h.Ctx(), entities.User{Username: "dina", Roles: []string{entities.RoleDesigner}}),
		"an operator and designer of this organization alone": signedInAs(h.Ctx(), entities.User{Username: "odile",
			RolesByOrganization: map[uuid.UUID][]string{here: {entities.RoleOperator, entities.RoleDesigner}}}),
		"an administrator of another organization, asking in this one": signedInAs(h.Ctx(), entities.User{Username: "otto",
			RolesByOrganization: map[uuid.UUID][]string{elsewhere.ID: {entities.RoleAdmin}}}),
		"an administrator whose request is for no organization": signedInAs(t.Context(),
			entities.User{Username: "gail", Roles: []string{entities.RoleAdmin}}),
		"an administrator's account with no name": signedInAs(h.Ctx(), entities.User{Roles: []string{entities.RoleAdmin}}),
	}
	for who, ctx := range refused {
		var answers []string
		for what, command := range map[string]entities.DeviationCommand{"preview": preview, "apply": apply, "preview of no such instance": nowhere} {
			out, err := w.svc.DeviateInstance(ctx, command)
			if !errors.Is(err, apierr.ErrForbidden) {
				t.Errorf("%s, %s: got %v, want forbidden", who, what, err)
				continue
			}
			if !reflect.DeepEqual(out, entities.DeviationOutcome{}) {
				t.Errorf("%s, %s: refused, and still answered %+v", who, what, out)
			}
			answers = append(answers, err.Error())
		}
		for _, answer := range answers {
			if answer != answers[0] {
				t.Errorf("%s is answered %q for one command and %q for another: the refusal says something about the instance", who, answers[0], answer)
			}
		}
	}

	outsider := signedInAs(inTheOther, entities.User{Username: "otto",
		RolesByOrganization: map[uuid.UUID][]string{elsewhere.ID: {entities.RoleAdmin}}})
	_, errTheirs := w.svc.DeviateInstance(outsider, preview)
	_, errNone := w.svc.DeviateInstance(outsider, nowhere)
	if !errors.Is(errTheirs, apierr.ErrNotFound) || !errors.Is(errNone, apierr.ErrNotFound) {
		t.Fatalf("another organization's administrator: got %v for the instance and %v for one that does not exist, want not found for both", errTheirs, errNone)
	}
	if errTheirs.Error() != errNone.Error() {
		t.Errorf("another organization's administrator is told %q of this instance and %q of one that does not exist; the difference says it exists",
			errTheirs, errNone)
	}
	if _, err := w.svc.DeviateInstance(outsider, apply); errors.Is(err, apierr.ErrForbidden) || err == nil {
		t.Errorf("another organization's administrator applying: got %v, want neither done nor a refusal that confirms the instance", err)
	}

	// An administrator of this organization alone is one.
	local := signedInAs(h.Ctx(), entities.User{Username: "lena",
		RolesByOrganization: map[uuid.UUID][]string{here: {entities.RoleAdmin}}})
	out, err := w.svc.DeviateInstance(local, preview)
	if err != nil || !out.Plan.Applicable() {
		t.Fatalf("an administrator of this organization alone: %v, refusals:%s", err, lines(out.Plan.Refusals))
	}
	if changed := tablesThatDiffer(before, everyRow(t, h)); len(changed) != 0 {
		t.Fatalf("refused callers and previews changed %v", changed)
	}
}

// Review Focus 5, at the service. A preview reads and does nothing else: no
// row is written anywhere, nothing is announced, and it does not wait for —
// so it does not take — the rows an apply or a completion holds.
func TestAPreviewChangesNothingAndWaitsForNobody(t *testing.T) {
	h := newEngineHarness(t, "Waive Preview Project")
	events := &eventLog{}
	h.dispatcher.Register(events)
	w := newWaiver(h)
	id := w.start(t, opsApproval(h.projID, "preview-ops"), nil)

	before := everyRow(t, h)
	instanceBefore, err := h.repo.Process().Get(h.Ctx(), id)
	if err != nil {
		t.Fatalf("read the instance: %v", err)
	}
	raised := events.count()

	// Somebody else holds the instance and its task, as an apply does and a
	// completion does. A preview that asked for either would wait here.
	holder := h.db.Begin()
	defer holder.Rollback()
	var heldInstances, heldTasks []string
	if err := holder.Raw(`SELECT id::text FROM process_instances WHERE id = ? FOR UPDATE`, id).Scan(&heldInstances).Error; err != nil || len(heldInstances) != 1 {
		t.Fatalf("hold the instance: %d row(s), %v", len(heldInstances), err)
	}
	if err := holder.Raw(`SELECT id::text FROM tasks WHERE instance_id = ? FOR UPDATE`, id).Scan(&heldTasks).Error; err != nil || len(heldTasks) != 1 {
		t.Fatalf("hold the task: %d row(s), %v", len(heldTasks), err)
	}

	applicable := map[string]entities.DeviationCommand{
		"a waive":  deviationCommand(entities.DeviationWaive, id, "opsApprove", map[string]any{"approved": true}),
		"a cancel": deviationCommand(entities.DeviationCancel, id, "opsApprove", nil),
		"a hold":   deviationCommand(entities.DeviationHold, id, "opsApprove", nil),
	}
	refusedOnes := map[string]entities.DeviationCommand{
		"a waive of a step it has not reached": deviationCommand(entities.DeviationWaive, id, "salesApprove", nil),
		"a cancel that names no step":          deviationCommand(entities.DeviationCancel, id, "", nil),
		"a waive of no such step":              deviationCommand(entities.DeviationWaive, id, "nowhere", nil),
	}
	inTime, stop := context.WithTimeout(w.ctx, 15*time.Second)
	defer stop()
	for _, set := range []struct {
		commands   map[string]entities.DeviationCommand
		applicable bool
	}{{applicable, true}, {refusedOnes, false}} {
		for what, command := range set.commands {
			command.DryRun = true
			out, err := w.svc.DeviateInstance(inTime, command)
			if err != nil {
				t.Fatalf("previewing %s while the rows are held: %v", what, err)
			}
			if out.Applied || out.Replayed || out.Deviation != nil {
				t.Errorf("previewing %s answered as though it had acted: %+v", what, out)
			}
			if out.Plan.Applicable() != set.applicable {
				t.Errorf("previewing %s: applicable %v, refusals:%s", what, out.Plan.Applicable(), lines(out.Plan.Refusals))
			}
		}
	}
	if err := holder.Rollback().Error; err != nil {
		t.Fatalf("let the rows go: %v", err)
	}

	if changed := tablesThatDiffer(before, everyRow(t, h)); len(changed) != 0 {
		t.Fatalf("previews changed %v", changed)
	}
	instanceAfter, err := h.repo.Process().Get(h.Ctx(), id)
	if err != nil {
		t.Fatalf("read the instance again: %v", err)
	}
	if !reflect.DeepEqual(instanceBefore, instanceAfter) {
		t.Fatalf("the instance after the previews is not the instance before them:\n before %+v\n after  %+v", instanceBefore, instanceAfter)
	}
	if now := events.count(); now != raised {
		t.Fatalf("previews raised %d event(s)", now-raised)
	}
	if told := events.ofType(entities.EventTaskCanceled); len(told) != 0 {
		t.Fatalf("previews told %d holder(s) their work was withdrawn", len(told))
	}
	if open := openIterationTasks(h.Ctx(), t, h, id, "opsApprove"); len(open) != 1 || open[0].AssigneeUsername() != "ollie" {
		t.Fatalf("the step's work after the previews: %+v, want ollie's one task", open)
	}
}

// What a preview refuses, an apply refuses: the request is answered as one the
// caller can fix, and the instance is as it was. An apply of a plan nothing
// refuses acts (TestAWaivedStepIsWithdrawnItsHolderToldAndTheInstanceMovesOn);
// this pins that one of a refused plan does not.
func TestAnApplyOfAPlanThatRefusesIsRefusedAndChangesNothing(t *testing.T) {
	h := newEngineHarness(t, "Waive Refused Apply Project")
	w := newWaiver(h)
	id := w.start(t, opsApproval(h.projID, "refused-apply-ops"), nil)
	before := everyRow(t, h)

	for what, command := range map[string]entities.DeviationCommand{
		"a waive of a step the instance has not reached": deviationCommand(entities.DeviationWaive, id, "salesApprove", nil),
		"a waive setting what the form does not declare": deviationCommand(entities.DeviationWaive, id, "opsApprove", map[string]any{"amount": 900}),
		"a cancel that names no step, while it waits":    deviationCommand(entities.DeviationCancel, id, "", nil),
		"a hold of a step the process does not have":     deviationCommand(entities.DeviationHold, id, "nowhere", nil),
	} {
		if plan := w.preview(t, command); plan.Applicable() {
			t.Fatalf("%s: the preview does not refuse it, so this proves nothing", what)
		}
		out, err := w.apply(t, command)
		if !errors.Is(err, apierr.ErrInvalidArgument) {
			t.Errorf("%s, applied: got %v, want it refused as something the caller can fix", what, err)
		}
		if out.Applied || out.Replayed || out.Deviation != nil {
			t.Errorf("%s, applied: answered as though it had acted: %+v", what, out)
		}
	}
	if changed := tablesThatDiffer(before, everyRow(t, h)); len(changed) != 0 {
		t.Fatalf("refused applies changed %v", changed)
	}
	if rows := w.ledger(t, id); len(rows) != 0 {
		t.Fatalf("refused applies wrote %d ledger row(s)", len(rows))
	}
}

// A refusal does not send an administrator to something that cannot be done.
// A call step whose called process has ended without resuming it — cancelled
// in place, or ended at a terminate end event, which resumes nobody — has no
// step inside it left to waive. The waive is refused in words that say so,
// and say what can be done: the instance can be cancelled or held, and the
// plan of each agrees.
func TestAWaiveOfACallStepWhoseCalledProcessHasEndedSaysWhatCanBeDone(t *testing.T) {
	h := newEngineHarness(t, "Waive Call Ended Project")
	w := newWaiver(h)
	ctx := h.Ctx()
	reviewThenEnd := func(key string, end entities.NodeType) *entities.ProcessDefinition {
		return &entities.ProcessDefinition{
			Project: &entities.Project{ID: h.projID}, Key: key,
			Nodes: []*entities.Node{
				{ID: "start", Type: entities.StartEvent},
				{ID: "review", Type: entities.UserTask, Name: "Review the supplier", Assignee: "rita"},
				{ID: "end", Type: end},
			},
			Flows: []*entities.SequenceFlow{{ID: "c1", SourceRef: "start", TargetRef: "review"}, {ID: "c2", SourceRef: "review", TargetRef: "end"}},
		}
	}
	const want = "“Have it checked” is waiting for a process that has ended and will not resume it; this instance can be cancelled or held instead."
	const sendsInside = "waive the step inside that process"

	h.deploy(t, reviewThenEnd("call-ended-cancelled", entities.EndEvent))
	cancelled := w.start(t, callerOf(h.projID, "call-ended-caller-a", "call-ended-cancelled"), nil)
	w.mustApply(t, deviationCommand(entities.DeviationCancel, theOneCalledBy(t, h, cancelled), "review", nil))

	h.deploy(t, reviewThenEnd("call-ended-terminated", entities.TerminateEndEvent))
	terminated := w.start(t, callerOf(h.projID, "call-ended-caller-b", "call-ended-terminated"), nil)
	called := theOneCalledBy(t, h, terminated)
	if err := completeAs(ctx, h, theOpenTask(t, h, called, "review"), "rita", nil); err != nil {
		t.Fatalf("rita completes the review: %v", err)
	}
	requireInstanceStatus(ctx, t, h, called, entities.ProcessCompleted)

	for what, caller := range map[string]uuid.UUID{"cancelled in place": cancelled, "ended at a terminate end event": terminated} {
		if waiting := requireInstanceStatus(ctx, t, h, caller, entities.ProcessActive); tokensOn(t, h, caller, "haveItChecked") != 1 || len(waiting.Tokens) != 1 {
			t.Fatalf("called process %s: its caller holds %d token(s); this test needs it still waiting at the call", what, len(waiting.Tokens))
		}
		plan := w.preview(t, deviationCommand(entities.DeviationWaive, caller, "haveItChecked", nil))
		if !reflect.DeepEqual(plan.Refusals, []string{want}) {
			t.Errorf("called process %s: the waive's refusals:%s\nwant only\n  %s", what, lines(plan.Refusals), want)
		}
		if strings.Contains(strings.Join(plan.Refusals, " "), sendsInside) {
			t.Errorf("called process %s: the refusal sends the administrator inside a process that has ended:%s", what, lines(plan.Refusals))
		}
		for _, kind := range []entities.DeviationKind{entities.DeviationCancel, entities.DeviationHold} {
			if can := w.preview(t, deviationCommand(kind, caller, "haveItChecked", nil)); !can.Applicable() {
				t.Errorf("called process %s: a %s of its caller, which the refusal points to, is refused:%s", what, kind, lines(can.Refusals))
			}
		}
	}
}
