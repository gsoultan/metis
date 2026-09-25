package bpmn_test

import (
	"fmt"
	"sync"
	"testing"

	"github.com/gsoultan/metis/server/domains/entities"
)

// Starting a step read the instance, added the step's token and wrote the whole
// token list back, with nothing held in between. Two workers starting different
// steps at the same moment each wrote a list without the other's step in it, so
// the later write erased the earlier one: that step's task was offered, and the
// instance no longer knew it was running — finishing it found nothing to carry
// on from.
func TestAdHocStepsStartedTogetherAreAllKept(t *testing.T) {
	h := newEngineHarness(t, "AdHoc Concurrent Project")
	ctx := h.Ctx()

	const steps = 8
	def := adHocDefinition("claim-research-busy", "reviewsDone >= 99")
	def.Project = &entities.Project{ID: h.projID}
	research := def.Nodes[1]
	for i := range steps {
		research.Nodes = append(research.Nodes, &entities.Node{
			ID: fmt.Sprintf("step-%d", i), Type: entities.UserTask, Name: fmt.Sprintf("Step %d", i), ParentID: research.ID,
		})
	}
	if _, err := h.svc.CreateDefinition(ctx, &def); err != nil {
		t.Fatalf("create definition: %v", err)
	}
	instanceID, err := h.svc.StartProcess(ctx, h.projID, "claim-research-busy", map[string]any{"reviewsDone": 0})
	if err != nil {
		t.Fatalf("start process: %v", err)
	}

	start := make(chan struct{})
	errs := make(chan error, steps)
	var wg sync.WaitGroup
	for i := range steps {
		wg.Go(func() {
			<-start
			errs <- h.svc.ActivateTask(ctx, instanceID, "research", fmt.Sprintf("step-%d", i))
		})
	}
	close(start)
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatalf("activate: %v", err)
		}
	}

	instance, err := h.svc.GetInstance(ctx, instanceID)
	if err != nil {
		t.Fatalf("read the instance: %v", err)
	}
	var lost []string
	for i := range steps {
		step := fmt.Sprintf("step-%d", i)
		if len(instance.GetTokensByNode(&entities.Node{ID: step})) != 1 {
			lost = append(lost, step)
		}
	}
	if len(lost) > 0 {
		t.Fatalf("%d of %d steps started together are no longer known to the instance: %v", len(lost), steps, lost)
	}
}
