package bpmn_test

import (
	"strings"
	"testing"

	"github.com/gsoultan/metis/server/domains/entities"
)

// noWayOut is start → a gateway none of whose flows fits and which declares
// no default, with whatever catches its failure attached to it.
func noWayOut(h engineHarness, key string, gateway entities.NodeType, catching map[string]any) *entities.ProcessDefinition {
	def := &entities.ProcessDefinition{
		Project: &entities.Project{ID: h.projID}, Key: key,
		Nodes: []*entities.Node{
			{ID: "start", Type: entities.StartEvent},
			{ID: "decide", Type: gateway, Name: "Verdict?"},
			{ID: "accepted", Type: entities.EndEvent},
			{ID: "rejected", Type: entities.EndEvent},
		},
		Flows: []*entities.SequenceFlow{
			{ID: "f1", SourceRef: "start", TargetRef: "decide"},
			{ID: "yes", SourceRef: "decide", TargetRef: "accepted", Condition: "verdict = accept"},
			{ID: "no", SourceRef: "decide", TargetRef: "rejected", Condition: "verdict = reject"},
		},
	}
	if catching != nil {
		def.Nodes = append(def.Nodes,
			&entities.Node{ID: "caught", Type: entities.BoundaryEvent, AttachedToRef: "decide", Properties: catching},
			&entities.Node{ID: "sort-out", Type: entities.UserTask, Name: "Sort it out"})
		def.Flows = append(def.Flows,
			&entities.SequenceFlow{ID: "e1", SourceRef: "caught", TargetRef: "sort-out"},
			&entities.SequenceFlow{ID: "e2", SourceRef: "sort-out", TargetRef: "accepted"})
	}
	return def
}

// BPMN 2.0.2 §13.3.2: an exclusive gateway none of whose conditions holds, and
// which has no default flow, throws an exception. Here that exception is an
// error whose text begins BPMN_ERROR: — which is how the engine tells a
// failure an error boundary event may catch from any other — and whose code is
// the rest of the text.
//
// The words are part of what a definition can rely on: a boundary event
// catching exactly this code is routed to by comparing them. This pins them,
// for both gateways that can fail this way, and that a boundary event catches
// the failure — by its code, and as anything.
func TestAGatewayThatCannotChooseFailsInWordsAnErrorBoundaryCatches(t *testing.T) {
	for kind, gateway := range map[string]entities.NodeType{
		"exclusive": entities.ExclusiveGateway,
		"inclusive": entities.InclusiveGateway,
	} {
		code := "no outgoing sequence flow could be selected at " + kind + ` gateway "decide": ` +
			"no condition evaluated true and no default flow is declared"

		t.Run(kind+", with nothing to catch it", func(t *testing.T) {
			h := newEngineHarness(t, "No Way Out "+kind)
			h.deploy(t, noWayOut(h, "no-way-out-"+kind, gateway, nil))
			_, err := h.svc.StartProcess(h.Ctx(), h.projID, "no-way-out-"+kind, map[string]any{"verdict": "maybe"})
			if err == nil || !strings.HasSuffix(err.Error(), "BPMN_ERROR:"+code) {
				t.Fatalf("a gateway with no way out: got %v\nwant an error ending\n  BPMN_ERROR:%s", err, code)
			}
		})
		for caught, errorCode := range map[string]string{"by its code": code, "as anything": "*"} {
			t.Run(kind+", caught "+caught, func(t *testing.T) {
				h := newEngineHarness(t, "No Way Out Caught "+kind)
				h.deploy(t, noWayOut(h, "no-way-out-caught-"+kind, gateway, map[string]any{"error_code": errorCode}))
				id, err := h.svc.StartProcess(h.Ctx(), h.projID, "no-way-out-caught-"+kind, map[string]any{"verdict": "maybe"})
				if err != nil {
					t.Fatalf("the error boundary did not catch the gateway's failure: %v", err)
				}
				if !h.waitingAt(h.Ctx(), t, id, "sort-out") {
					t.Fatal("the failure was caught and the instance did not go where the boundary event leads")
				}
			})
		}
		t.Run(kind+", with a boundary for another code", func(t *testing.T) {
			h := newEngineHarness(t, "No Way Out Other "+kind)
			h.deploy(t, noWayOut(h, "no-way-out-other-"+kind, gateway, map[string]any{"error_code": "charge-failed"}))
			if _, err := h.svc.StartProcess(h.Ctx(), h.projID, "no-way-out-other-"+kind, map[string]any{"verdict": "maybe"}); err == nil ||
				!strings.HasSuffix(err.Error(), "BPMN_ERROR:"+code) {
				t.Fatalf("a boundary for another code: got %v, want the gateway's failure", err)
			}
		})
	}
}
