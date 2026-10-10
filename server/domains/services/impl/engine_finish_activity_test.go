package impl

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"

	"github.com/gsoultan/metis/internal/pkg/apierr"
	"github.com/gsoultan/metis/server/domains/entities"
	observersimpl "github.com/gsoultan/metis/server/domains/observers/impl"
)

// everyEvent keeps whatever the engine raises.
type everyEvent struct {
	events []entities.ProcessEvent
}

func (w *everyEvent) OnEvent(_ context.Context, event entities.ProcessEvent) {
	w.events = append(w.events, event)
}

// FinishActivity ends a step and follows what comes after it. Asked of a step
// the instance is not waiting at, it would follow what comes after a second
// time: two tokens after a step that was performed once. It refuses instead,
// from the instance it was given, before anything is read or written.
func TestFinishActivityRefusesAStepTheInstanceIsNotAt(t *testing.T) {
	approve := &entities.Node{ID: "approve", Name: "Approve", Type: entities.UserTask}
	record := &entities.Node{ID: "record", Name: "Record", Type: entities.UserTask}
	def := &entities.ProcessDefinition{
		Nodes: []*entities.Node{approve, record, {ID: "end", Type: entities.EndEvent}},
		Flows: []*entities.SequenceFlow{
			{ID: "f1", SourceRef: "approve", TargetRef: "record"},
			{ID: "f2", SourceRef: "record", TargetRef: "end"},
		},
	}

	for _, c := range []struct {
		name   string
		nodeID string
	}{
		{"a step it has already left", "approve"},
		{"a step it has not reached", "end"},
		{"a step the process does not have", "nowhere"},
	} {
		t.Run(c.name, func(t *testing.T) {
			dispatcher := observersimpl.NewEventDispatcher()
			raised := &everyEvent{}
			dispatcher.Register(raised)
			engine := NewExecutionEngine(untouchable{t: t}, dispatcher)
			instance := &entities.ProcessInstance{ID: uuid.New(), Status: entities.ProcessActive}
			instance.MarkCompleted(approve)
			instance.AddToken(record)

			err := engine.FinishActivity(context.Background(), instance, def, c.nodeID)

			if err == nil {
				t.Fatal("a step the instance is not waiting at was ended, and what follows it was followed again")
			}
			if errors.Is(err, apierr.ErrInvalidArgument) {
				t.Errorf("the refusal is answered as something the caller sent wrong: %v; it is the calling code's mistake", err)
			}
			if len(instance.Tokens) != 1 || instance.Tokens[0].Node == nil || instance.Tokens[0].Node.ID != "record" {
				t.Errorf("a refused finish moved the instance: its tokens are %+v", instance.Tokens)
			}
			if len(instance.CompletedNodes) != 1 {
				t.Errorf("a refused finish marked a step completed: %d are", len(instance.CompletedNodes))
			}
			if len(raised.events) != 0 {
				t.Errorf("a refused finish raised %d event(s)", len(raised.events))
			}
		})
	}
}
