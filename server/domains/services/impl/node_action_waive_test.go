package impl

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"

	"github.com/gsoultan/metis/internal/pkg/apierr"
	"github.com/gsoultan/metis/server/domains/entities"
	servicecontracts "github.com/gsoultan/metis/server/domains/services/contracts"
	"github.com/gsoultan/metis/server/repositories"
	repocontracts "github.com/gsoultan/metis/server/repositories/contracts"
)

// untouchable is a repository that what is under test must not reach for: a
// refusal that opens a transaction, or reads or writes a task or an instance,
// fails the test and says which.
type untouchable struct {
	repositories.Repository
	t *testing.T
}

func (r untouchable) UnitOfWork() repocontracts.UnitOfWork {
	r.t.Fatal("a transaction was opened by something that should have refused first")
	return nil
}

func (r untouchable) Task() repocontracts.TaskRepository {
	r.t.Fatal("the tasks were read or written by something that should have refused first")
	return nil
}

func (r untouchable) Process() repocontracts.ProcessRepository {
	r.t.Fatal("the instance was read or written by something that should have refused first")
	return nil
}

// advanceOnly is an engine that can advance an instance past a step and
// cannot end a step whole: it has Proceed and no FinishActivity. Everything
// else an engine does is left out, so a test that reaches for it fails.
type advanceOnly struct {
	servicecontracts.ExecutionEngine
	advanced int
}

func (e *advanceOnly) Proceed(context.Context, *entities.ProcessInstance, *entities.ProcessDefinition, string) error {
	e.advanced++
	return nil
}

// wholeFinisher stands in for an engine that can end a step whole. Nothing is
// asked of it here but that it is one.
type wholeFinisher struct{}

func (wholeFinisher) FinishActivity(context.Context, *entities.ProcessInstance, *entities.ProcessDefinition, string) error {
	return nil
}

// How a skipped step is ended is decided from what the step is and what the
// engine can do — and where neither way is right, it is refused.
func TestHowASkippedStepIsEnded(t *testing.T) {
	const whole, advanced, refused = "ended whole", "withdrawn from and advanced past", "refused"
	finisher := wholeFinisher{}
	for _, c := range []struct {
		name     string
		node     *entities.Node
		finisher servicecontracts.ActivityFinisher
		want     string
	}{
		{"a user task", &entities.Node{ID: "a", Type: entities.UserTask}, finisher, whole},
		{"a manual task", &entities.Node{ID: "a", Type: entities.ManualTask}, finisher, whole},
		{"a repeating user task", &entities.Node{ID: "a", Type: entities.UserTask, MultiInstanceType: "parallel"}, finisher, whole},
		{"a repeating manual task", &entities.Node{ID: "a", Type: entities.ManualTask, MultiInstanceType: "sequential"}, finisher, whole},
		{"a service call", &entities.Node{ID: "a", Type: entities.ServiceTask}, finisher, advanced},
		{"a repeating service call", &entities.Node{ID: "a", Type: entities.ServiceTask, MultiInstanceType: "parallel"}, finisher, advanced},
		{"a repeating sub-process", &entities.Node{ID: "a", Type: entities.SubProcess, MultiInstanceType: "parallel"}, finisher, advanced},
		{"a repeating call activity", &entities.Node{ID: "a", Type: entities.CallActivity, MultiInstanceType: "parallel"}, finisher, advanced},
		{"a step the definition does not have", nil, finisher, advanced},

		{"a user task, and an engine that only advances", &entities.Node{ID: "a", Type: entities.UserTask}, nil, advanced},
		{"a manual task, and an engine that only advances", &entities.Node{ID: "a", Type: entities.ManualTask}, nil, advanced},
		{"a parallel user task, and an engine that only advances", &entities.Node{ID: "a", Type: entities.UserTask, MultiInstanceType: "parallel"}, nil, refused},
		{"a sequential user task, and an engine that only advances", &entities.Node{ID: "a", Type: entities.UserTask, MultiInstanceType: "sequential"}, nil, refused},
		{"a repeating manual task, and an engine that only advances", &entities.Node{ID: "a", Type: entities.ManualTask, MultiInstanceType: "parallel"}, nil, refused},
		{"a repeating service call, and an engine that only advances", &entities.Node{ID: "a", Type: entities.ServiceTask, MultiInstanceType: "parallel"}, nil, advanced},
		{"a step the definition does not have, and an engine that only advances", nil, nil, advanced},
	} {
		t.Run(c.name, func(t *testing.T) {
			wholly, err := nodeActions{finisher: c.finisher}.endsWhole(c.node, "a")
			got := advanced
			switch {
			case err != nil:
				got = refused
			case wholly:
				got = whole
			}
			if got != c.want {
				t.Fatalf("the step is %s (%v), want %s", got, err, c.want)
			}
			if err != nil && errors.Is(err, apierr.ErrInvalidArgument) {
				t.Errorf("the refusal is answered as something the caller sent wrong: %v; it is how the server was wired", err)
			}
		})
	}
}

// A repeating approval skipped by an engine that can only advance was counted
// as one run finished: the other runs kept their tokens with their tasks
// withdrawn, and nothing said so. It is refused instead, before anything is
// read or written.
func TestASkipOfARepeatingApprovalIsRefusedByAnEngineThatCannotEndItWhole(t *testing.T) {
	approve := &entities.Node{ID: "approve", Name: "Approve the purchase", Type: entities.UserTask, MultiInstanceType: "parallel"}
	def := &entities.ProcessDefinition{Nodes: []*entities.Node{approve}}
	live := &entities.ProcessInstance{ID: uuid.New(), Status: entities.ProcessActive}
	live.AddTokenWithIteration(approve, "0")
	live.AddTokenWithIteration(approve, "1")
	live.StartMultiInstance("approve", 2)

	engine := &advanceOnly{}
	actions := newNodeActions(nil, engine)
	if actions.finisher != nil {
		t.Fatal("an engine with no FinishActivity was taken for one that can end a step whole")
	}
	actions.repo = untouchable{t: t}

	withdrawn, err := actions.waive(context.Background(), live, def, "approve")

	if err == nil {
		t.Fatal("a repeating approval was skipped by an engine that can only advance: one run counted, the rest left")
	}
	if errors.Is(err, apierr.ErrInvalidArgument) {
		t.Errorf("the refusal is answered as something the caller sent wrong: %v", err)
	}
	if engine.advanced != 0 {
		t.Errorf("the instance was advanced %d time(s) by a skip that was refused", engine.advanced)
	}
	if len(withdrawn) != 0 {
		t.Errorf("a refused skip says it withdrew %d task(s)", len(withdrawn))
	}
	if len(live.Tokens) != 2 || !live.IsMultiInstanceActive("approve") {
		t.Errorf("a refused skip changed the instance: %d token(s), counting runs: %v", len(live.Tokens), live.IsMultiInstanceActive("approve"))
	}
}
