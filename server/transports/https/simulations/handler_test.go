package simulations

import (
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gsoultan/metis/server/endpoints/simulation"
)

// The designer sends its canvas exactly as below — every field a node carries,
// people and groups as objects. The decoder refuses unknown fields, so a field
// the designer sends and the entity does not declare would refuse every draft
// run with "this request body could not be read". This pins the two together.
const designerDraft = `{
  "project_id": "0199a000-0000-7000-8000-000000000001",
  "definition_key": "expense-approval",
  "definition": {
    "key": "expense-approval",
    "name": "Expense approval",
    "nodes": [{
      "id": "approve", "name": "Approve", "type": "userTask",
      "x": 100, "y": 80, "width": 0, "height": 0, "is_expanded": false,
      "assignee": "", "candidate_users": [{"username": "sarah"}], "candidate_groups": [{"name": "finance"}],
      "priority": 0, "due_date": "", "form_key": "", "default_flow": "",
      "script": "", "script_format": "", "external_topic": "", "documentation": "",
      "attached_to_ref": "", "parent_id": "", "cancel_activity": false, "error_code": "",
      "multi_instance_type": "", "loop_cardinality": 0, "collection": "", "element_variable": "",
      "completion_condition": "", "is_event_sub_process": false, "condition": "",
      "properties": {"decision_key": "limits"}
    }],
    "flows": [{"id": "f1", "source_ref": "start", "target_ref": "approve", "condition": "", "documentation": ""}]
  },
  "variables": {"amount": 4200},
  "answers": [],
  "clock_start": "2026-09-22T09:00:00Z",
  "seed": 42,
  "max_steps": 500
}`

func TestDecodeAcceptsTheDesignersDraft(t *testing.T) {
	r := httptest.NewRequestWithContext(t.Context(), "POST", "/api/v1/simulations", strings.NewReader(designerDraft))
	decoded, err := decodeSimulateRequest(t.Context(), r)
	if err != nil {
		t.Fatalf("the designer's draft was refused: %v", err)
	}
	req := decoded.(simulation.SimulateRequest)
	if req.Definition == nil || len(req.Definition.Nodes) != 1 {
		t.Fatalf("draft decoded as %+v", req.Definition)
	}
	node := req.Definition.Nodes[0]
	if len(node.CandidateUsers) != 1 || node.CandidateUsers[0].Username != "sarah" {
		t.Errorf("candidate users decoded as %+v", node.CandidateUsers)
	}
	if len(node.CandidateGroups) != 1 || node.CandidateGroups[0].Name != "finance" {
		t.Errorf("candidate groups decoded as %+v", node.CandidateGroups)
	}
}
