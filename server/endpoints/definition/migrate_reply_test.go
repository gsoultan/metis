package definition

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/gsoultan/metis/server/domains/entities"
)

// What the migrate route writes, to the letter.
//
// The reply is written through views now — the plan, the request that waits,
// each instance passed over — where it used to write the plan and the waiting
// request as the entities encode themselves. A client must not be able to
// tell: these are the bytes the route wrote before, for a plan that holds
// nothing, for one that holds something of everything, and for an apply that
// was sent to a second administrator. A list that is empty is left out, one
// that holds something is a list, and none is ever null.

// fullPlan is a plan with something in every field.
func fullPlan() entities.MigrationPlan {
	return entities.MigrationPlan{
		SourceKey: "quotation", SourceVersion: 1, TargetVersion: 2,
		TargetID:  uuid.MustParse("0198f3a0-0000-7000-8000-000000000002"),
		Instances: 3,
		Moves: []entities.NodeMove{
			{From: "review", To: "review", Tokens: 2, Tasks: 2, Jobs: 1},
			{From: "legal", To: "review", Tokens: 1, Tasks: 1, TasksClaimed: 1, TasksDelegated: 1, Events: 2, Mapped: true},
		},
		Refusals: []string{"a refusal"},
		Warnings: []string{"a warning <with> marks & \"quotes\""},
		ComplianceHolds: []entities.ComplianceHold{
			{NodeID: "opsApprove", Name: "Operations approve", Note: "SOX over $50k", Instances: 2},
			{NodeID: "bare"},
		},
		Actions: []entities.PlannedNodeAction{
			{NodeID: "opsApprove", Name: "Operations approve", Kind: "skip", Reason: "the role was eliminated"},
			{NodeID: "bare", Kind: "cancel"},
		},
		RemovedNodes:           []string{"opsApprove", "legal"},
		RequiresSecondApprover: true,
		SecondApproverReasons:  []string{"a reason"},
	}
}

const (
	emptyPlanWritten = `{"source_key":"","source_version":0,"target_version":0,"target_id":"00000000-0000-0000-0000-000000000000",` +
		`"instances":0,"requires_second_approver":false}`
	fullPlanWritten = `{"source_key":"quotation","source_version":1,"target_version":2,"target_id":"0198f3a0-0000-7000-8000-000000000002",` +
		`"instances":3,` +
		`"moves":[{"from":"review","to":"review","tokens":2,"tasks":2,"jobs":1,"mapped":false},` +
		`{"from":"legal","to":"review","tokens":1,"tasks":1,"jobs":0,"tasks_claimed":1,"tasks_delegated":1,"events":2,"mapped":true}],` +
		`"refusals":["a refusal"],"warnings":["a warning \u003cwith\u003e marks \u0026 \"quotes\""],` +
		`"compliance_holds":[{"node_id":"opsApprove","name":"Operations approve","note":"SOX over $50k","instances":2},{"node_id":"bare","instances":0}],` +
		`"actions":[{"node_id":"opsApprove","name":"Operations approve","kind":"skip","reason":"the role was eliminated"},{"node_id":"bare","kind":"cancel"}],` +
		`"removed_nodes":["opsApprove","legal"],"requires_second_approver":true,"second_approver_reasons":["a reason"]}`
)

func TestTheMigrateReplyIsWrittenAsItAlwaysWas(t *testing.T) {
	t.Parallel()
	requestID := uuid.MustParse("0198f3a0-0000-7000-8000-00000000000a")
	until := time.Date(2026, 10, 12, 9, 30, 0, 0, time.UTC)
	// Lists that are there and hold nothing are written as empty lists; lists
	// that are not there are left out. Neither is null.
	emptied := entities.MigrationPlan{
		Moves: []entities.NodeMove{}, Refusals: []string{}, Warnings: []string{}, ComplianceHolds: []entities.ComplianceHold{},
		Actions: []entities.PlannedNodeAction{}, RemovedNodes: []string{}, SecondApproverReasons: []string{},
	}
	cases := []struct {
		name  string
		reply MigrateInstancesResponse
		want  string
	}{
		{"a dry run of a plan that holds nothing",
			MigrateInstancesResponse{PassedOver: passedOverViews(nil)},
			`{"plan":` + emptyPlanWritten + `,"applied":false,"passed_over":[]}`},
		{"an apply of a plan with something in every field",
			MigrateInstancesResponse{Plan: fullPlan(), Applied: true, PassedOver: passedOverViews(nil)},
			`{"plan":` + fullPlanWritten + `,"applied":true,"passed_over":[]}`},
		{"a plan whose lists are there and empty",
			MigrateInstancesResponse{Plan: emptied, PassedOver: passedOverViews(nil)},
			`{"plan":{"source_key":"","source_version":0,"target_version":0,"target_id":"00000000-0000-0000-0000-000000000000","instances":0,` +
				`"moves":[],"refusals":[],"warnings":[],"compliance_holds":[],"actions":[],"removed_nodes":[],` +
				`"requires_second_approver":false,"second_approver_reasons":[]},"applied":false,"passed_over":[]}`},
		{"an apply sent to a second administrator",
			MigrateInstancesResponse{Plan: fullPlan(), PassedOver: passedOverViews(nil), PendingApproval: &entities.PendingApproval{
				RequestID: requestID, Status: entities.DeviationRequestPending, RequestedBy: "boss", ExpiresAt: until,
				Because: []string{"a reason"},
			}},
			`{"plan":` + fullPlanWritten + `,"applied":false,"passed_over":[],"pending_approval":{"request_id":"0198f3a0-0000-7000-8000-00000000000a",` +
				`"status":"pending_approval","requested_by":"boss","expires_at":"2026-10-12T09:30:00Z","because":["a reason"]}}`},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			written, err := json.Marshal(c.reply)
			if err != nil {
				t.Fatalf("write the reply: %v", err)
			}
			if string(written) != c.want {
				t.Errorf("the reply is written\n  %s\nwant\n  %s", written, c.want)
			}
		})
	}
}
