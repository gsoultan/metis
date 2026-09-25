package instancemigration

import (
	"testing"

	"github.com/gsoultan/metis/server/domains/entities"
	servicecontracts "github.com/gsoultan/metis/server/domains/services/contracts"
)

// A skip that cannot advance leaves the step as it was.
//
// skipNode withdrew the step's task, then asked the engine to advance past the
// step, each in a transaction of its own. When the advance failed, the task
// stayed withdrawn and the token stayed on the step: nobody could do the work,
// and nothing would move the instance on.

// routed has a review whose answer decides the way on. Without the review
// there is no answer, so skipping it leaves the gateway after it with no
// branch to take, and the advance fails.
func routed(f *fixture, withReview bool) *entities.ProcessDefinition {
	decideFrom := "in"
	if withReview {
		decideFrom = "out"
	}
	nodes := []*entities.Node{
		{ID: "start", Type: entities.StartEvent, Outgoing: []string{"in"}},
		{ID: "decide", Type: entities.ExclusiveGateway, Incoming: []string{decideFrom}, Outgoing: []string{"yes", "no"}},
		{ID: "accepted", Type: entities.EndEvent, Incoming: []string{"yes"}},
		{ID: "rejected", Type: entities.EndEvent, Incoming: []string{"no"}},
	}
	flows := []*entities.SequenceFlow{
		{ID: "yes", SourceRef: "decide", TargetRef: "accepted", Condition: "approved"},
		{ID: "no", SourceRef: "decide", TargetRef: "rejected", Condition: "rejected"},
	}
	if withReview {
		nodes = append(nodes, &entities.Node{ID: "review", Name: "Review the claim", Type: entities.UserTask, Assignee: "rita",
			Incoming: []string{"in"}, Outgoing: []string{"out"}})
		flows = append(flows,
			&entities.SequenceFlow{ID: "in", SourceRef: "start", TargetRef: "review"},
			&entities.SequenceFlow{ID: "out", SourceRef: "review", TargetRef: "decide"})
	} else {
		flows = append(flows, &entities.SequenceFlow{ID: "in", SourceRef: "start", TargetRef: "decide"})
	}
	return &entities.ProcessDefinition{
		Project: &entities.Project{ID: f.project},
		Key:     "claim",
		Name:    "Claim",
		Nodes:   nodes,
		Flows:   flows,
	}
}

func TestASkipThatCannotAdvanceLeavesTheStepToBeDone(t *testing.T) {
	f := newFixture(t)
	v1, err := f.svc.CreateDefinition(f.ctx, routed(f, true))
	if err != nil {
		t.Fatalf("deploy v1: %v", err)
	}
	if _, err := f.svc.StartProcess(f.ctx, f.project, "claim", nil); err != nil {
		t.Fatalf("start a claim: %v", err)
	}
	v2, err := f.svc.CreateDefinition(f.ctx, routed(f, false))
	if err != nil {
		t.Fatalf("deploy v2: %v", err)
	}

	err = f.svc.MigrateInstances(f.ctx, v1, v2, nil,
		servicecontracts.WithNodeActions(map[string]servicecontracts.NodeAction{
			"review": {Kind: servicecontracts.NodeActionSkip, Reason: "reviews are no longer needed"},
		}),
		servicecontracts.WithActor("dita"))
	if err == nil {
		t.Fatal("the skip reported success, though nothing after the review could be chosen")
	}

	open := f.openTasks(t)
	if len(open) != 1 || open[0].NodeID() != "review" {
		nodes := make([]string, 0, len(open))
		for _, task := range open {
			nodes = append(nodes, task.NodeID())
		}
		t.Fatalf("after a skip that could not advance, the open tasks are on %v; the review should still be there to do", nodes)
	}

	// And doing it moves the claim on, as it would have before the migration.
	if err := f.svc.CompleteTask(f.ctx, open[0].ID, "rita", map[string]any{"approved": true}); err != nil {
		t.Fatalf("complete the review: %v", err)
	}
	if instance := f.onlyInstance(t); instance.Status != entities.ProcessCompleted {
		t.Fatalf("the claim is %q after its review was done", instance.Status)
	}
}
