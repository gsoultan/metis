package bpmn_test

import (
	"sync"
	"testing"

	"github.com/gsoultan/metis/server/domains/entities"
	repocontracts "github.com/gsoultan/metis/server/repositories/contracts"
	"github.com/gsoultan/metis/server/repositories/models"
)

// waitingForTheSignal is more instances than one read of the generated store
// returns: storm starts every query with a limit of 1000.
const waitingForTheSignal = 1050

// A signal is owed to every instance waiting for it.
//
// Its audience was read with a query that looked unbounded and was not: the
// store caps a read at a thousand rows unless it is told otherwise. So a
// broadcast to more than a thousand waiting instances woke a thousand of them,
// reported success, and left the rest waiting for a signal that had already
// been sent — with nothing in any log to say so. An uncorrelated message fans
// out through the same read.
func TestASignalReachesEveryWaitingInstanceNotOnlyTheFirstThousand(t *testing.T) {
	h := newEngineHarness(t, "Signal Audience Project")
	ctx := h.Ctx()
	h.deploy(t, &entities.ProcessDefinition{
		Project: &entities.Project{ID: h.projID},
		Key:     "quarter-close",
		Nodes: []*entities.Node{
			{ID: "start", Type: entities.StartEvent},
			{ID: "await-close", Type: entities.IntermediateCatchEvent, Properties: map[string]any{
				"signal_name": "QuarterClosed",
			}},
			{ID: "end", Type: entities.EndEvent},
		},
		Flows: []*entities.SequenceFlow{
			{ID: "f1", SourceRef: "start", TargetRef: "await-close"},
			{ID: "f2", SourceRef: "await-close", TargetRef: "end"},
		},
	})

	// Started eight at a time: a thousand in a row is most of this test's
	// running time and none of its point.
	starts := make(chan struct{})
	var wg sync.WaitGroup
	for range 8 {
		wg.Go(func() {
			for range starts {
				if _, err := h.svc.StartProcess(ctx, h.projID, "quarter-close", nil); err != nil {
					t.Errorf("start: %v", err)
				}
			}
		})
	}
	for range waitingForTheSignal {
		starts <- struct{}{}
	}
	close(starts)
	wg.Wait()
	if t.Failed() {
		t.FailNow()
	}

	if err := h.engine.BroadcastSignal(ctx, h.projID, "QuarterClosed", nil); err != nil {
		t.Fatalf("broadcast: %v", err)
	}

	counts, err := h.engine.CountInstancesByStatus(ctx, h.projID, repocontracts.InstanceFilter{})
	if err != nil {
		t.Fatalf("count instances: %v", err)
	}
	if still := counts[models.ProcessActive]; still > 0 {
		t.Fatalf("the broadcast reported success and %d of %d instances are still waiting for the signal it sent",
			still, waitingForTheSignal)
	}
	if done := counts[models.ProcessCompleted]; done != waitingForTheSignal {
		t.Fatalf("%d of %d instances finished after the signal", done, waitingForTheSignal)
	}
}
