package bpmn_test

import (
	"testing"

	"github.com/google/uuid"
	"github.com/gsoultan/metis/server/domains/entities"
	"github.com/gsoultan/metis/server/repositories/models"
)

// Message and signal start events.
//
// The trigger walked every stored version of every process and started one
// instance for each version whose start event matched — so a message started
// as many instances as the process had versions, all of the live one, even
// where the live version no longer listened for that message. And each began
// at the definition's first start event, whichever that was, not at the one
// the message named.

// orderIntake has two ways in: someone starts it by hand, or an order message
// arrives. Each leads to its own first step, so where an instance began is
// visible in where it waits.
func orderIntake(projectID uuid.UUID, listens bool) *entities.ProcessDefinition {
	nodes := []*entities.Node{
		{ID: "byHand", Type: entities.StartEvent, Name: "Started by hand"},
		{ID: "enter", Type: entities.UserTask, Name: "Enter the order"},
		{ID: "check", Type: entities.UserTask, Name: "Check the order"},
		{ID: "end", Type: entities.EndEvent},
	}
	flows := []*entities.SequenceFlow{
		{ID: "f1", SourceRef: "byHand", TargetRef: "enter"},
		{ID: "f2", SourceRef: "enter", TargetRef: "end"},
		{ID: "f3", SourceRef: "check", TargetRef: "end"},
	}
	if listens {
		nodes = append(nodes, &entities.Node{ID: "byMessage", Type: entities.StartEvent, Name: "Order placed",
			Properties: map[string]any{"message_name": "OrderPlaced"}})
		flows = append(flows, &entities.SequenceFlow{ID: "f4", SourceRef: "byMessage", TargetRef: "check"})
	}
	return &entities.ProcessDefinition{Project: &entities.Project{ID: projectID}, Key: "order-intake", Nodes: nodes, Flows: flows}
}

func (h engineHarness) deploy(t *testing.T, def *entities.ProcessDefinition) {
	t.Helper()
	if _, err := h.svc.CreateDefinition(h.Ctx(), def); err != nil {
		t.Fatalf("deploy %s: %v", def.Key, err)
	}
}

func (h engineHarness) instances(t *testing.T) []models.ProcessInstanceModel {
	t.Helper()
	list, err := h.repo.Process().ListByProject(h.Ctx(), h.projID)
	if err != nil {
		t.Fatalf("list instances: %v", err)
	}
	return list
}

func TestAMessageStartsOneInstanceOfAProcessHoweverManyVersionsItHas(t *testing.T) {
	h := newEngineHarness(t, "Message Start Versions Project")
	for range 3 {
		h.deploy(t, orderIntake(h.projID, true))
	}

	if err := h.svc.SendMessage(h.Ctx(), h.projID, "OrderPlaced", "", map[string]any{"order": "A-1"}); err != nil {
		t.Fatalf("send: %v", err)
	}

	if got := len(h.instances(t)); got != 1 {
		t.Fatalf("one order message started %d instances", got)
	}
}

func TestAMessageStartEventBeginsTheProcessAtItself(t *testing.T) {
	h := newEngineHarness(t, "Message Start Node Project")
	h.deploy(t, orderIntake(h.projID, true))

	if err := h.svc.SendMessage(h.Ctx(), h.projID, "OrderPlaced", "", nil); err != nil {
		t.Fatalf("send: %v", err)
	}

	started := h.instances(t)
	if len(started) != 1 {
		t.Fatalf("expected one instance, got %d", len(started))
	}
	id := uuid.UUID(started[0].ID)
	if h.waitingAt(h.Ctx(), t, id, "enter") || !h.waitingAt(h.Ctx(), t, id, "check") {
		t.Fatal("the order message started the process at its manual start event, not at the message's")
	}
}

// The live version stopped listening; an older one still does. Nothing should
// start — and certainly not the live version from its manual start event.
func TestAStartEventOnlyAnOlderVersionHadStartsNothing(t *testing.T) {
	h := newEngineHarness(t, "Message Start Retired Project")
	h.deploy(t, orderIntake(h.projID, true))
	h.deploy(t, orderIntake(h.projID, false))

	if err := h.svc.SendMessage(h.Ctx(), h.projID, "OrderPlaced", "", nil); err != nil {
		t.Fatalf("send: %v", err)
	}

	if got := len(h.instances(t)); got != 0 {
		t.Fatalf("a message only a retired version listened for started %d instances", got)
	}
}

// A signal is a broadcast: every process that listens starts, once each.
func TestASignalStartsEachListeningProcessOnce(t *testing.T) {
	h := newEngineHarness(t, "Signal Start Project")
	for _, key := range []string{"close-books", "archive-day"} {
		for range 2 {
			h.deploy(t, &entities.ProcessDefinition{
				Project: &entities.Project{ID: h.projID},
				Key:     key,
				Nodes: []*entities.Node{
					{ID: "dayClosed", Type: entities.StartEvent, Properties: map[string]any{"signal_name": "DayClosed"}},
					{ID: "work", Type: entities.UserTask, Name: "Do the end-of-day work"},
					{ID: "end", Type: entities.EndEvent},
				},
				Flows: []*entities.SequenceFlow{
					{ID: "f1", SourceRef: "dayClosed", TargetRef: "work"},
					{ID: "f2", SourceRef: "work", TargetRef: "end"},
				},
			})
		}
	}

	if err := h.svc.BroadcastSignal(h.Ctx(), h.projID, "DayClosed", nil); err != nil {
		t.Fatalf("broadcast: %v", err)
	}

	if got := len(h.instances(t)); got != 2 {
		t.Fatalf("a signal two processes listen for started %d instances, want one each", got)
	}
}
