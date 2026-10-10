package definition

import (
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"slices"
	"strings"
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
//
// One thing was added since, and it is the only bytes that moved:
// `,"passed_over_in_all":0` after `"passed_over":[]` — how many instances the
// run passed over in all, beside the list, which now shows at most two
// hundred of them.

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
			`{"plan":` + emptyPlanWritten + `,"applied":false,"passed_over":[],"passed_over_in_all":0}`},
		{"an apply of a plan with something in every field",
			MigrateInstancesResponse{Plan: fullPlan(), Applied: true, PassedOver: passedOverViews(nil)},
			`{"plan":` + fullPlanWritten + `,"applied":true,"passed_over":[],"passed_over_in_all":0}`},
		{"a plan whose lists are there and empty",
			MigrateInstancesResponse{Plan: emptied, PassedOver: passedOverViews(nil)},
			`{"plan":{"source_key":"","source_version":0,"target_version":0,"target_id":"00000000-0000-0000-0000-000000000000","instances":0,` +
				`"moves":[],"refusals":[],"warnings":[],"compliance_holds":[],"actions":[],"removed_nodes":[],` +
				`"requires_second_approver":false,"second_approver_reasons":[]},"applied":false,"passed_over":[],"passed_over_in_all":0}`},
		{"an apply sent to a second administrator",
			MigrateInstancesResponse{Plan: fullPlan(), PassedOver: passedOverViews(nil), PendingApproval: &entities.PendingApproval{
				RequestID: requestID, Status: entities.DeviationRequestPending, RequestedBy: "boss", ExpiresAt: until,
				Because: []string{"a reason"},
			}},
			`{"plan":` + fullPlanWritten + `,"applied":false,"passed_over":[],"passed_over_in_all":0,"pending_approval":{"request_id":"0198f3a0-0000-7000-8000-00000000000a",` +
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

// passedOverBy is n instances a run left alone, each for a cause about one
// step, in an order that can be read back from their ids.
func passedOverBy(n int) []entities.PassedOverInstance {
	passed := make([]entities.PassedOverInstance, 0, n)
	for i := range n {
		passed = append(passed, entities.PassedOverInstance{
			Instance: &entities.ProcessInstance{ID: uuid.MustParse(fmt.Sprintf("0198f3a0-0000-7000-8000-%012d", i))},
			Cause:    entities.PassedOverLeftTheStep,
			Steps:    []entities.PassedOverStep{{NodeID: "opsApprove", Name: "Operations approve"}}, StepsInAll: 1,
			Reason: "It had left.",
		})
	}
	return passed
}

// A run over a great many instances can pass every one of them over, and the
// reply named them all. It lists the first two hundred, in the order the run
// came to them, and says beside the list how many there were. Whether
// anything was applied is still asked of them all: a run that wrote to none
// and passed over more than the list shows applied nothing.
func TestTheReplyListsTwoHundredOfThoseARunPassedOverAndSaysHowManyThereWere(t *testing.T) {
	t.Parallel()
	apply := false
	request := MigrateInstancesRequest{SourceDefinitionID: uuid.NewString(), TargetDefinitionID: uuid.NewString(), DryRun: &apply}
	cases := []struct {
		name            string
		changed, passed int
		listed          int
		applied         bool
	}{
		{"nobody", 3, 0, 0, true},
		{"exactly as many as the list shows", 0, 200, 200, false},
		{"more than the list shows, and nothing written", 0, 250, 200, false},
		{"more than the list shows, and something written", 1, 250, 200, true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			svc := &askedOfTheService{result: entities.MigrationResult{Changed: c.changed, PassedOver: passedOverBy(c.passed)}}
			reply, err := MakeMigrateInstancesEndpoint(svc)(signedInAs("dita"), request)
			if err != nil {
				t.Fatalf("migrate: %v", err)
			}
			written, err := json.Marshal(reply)
			if err != nil {
				t.Fatalf("write the reply: %v", err)
			}
			var read struct {
				Applied    *bool `json:"applied"`
				PassedOver []struct {
					InstanceID string `json:"instance_id"`
				} `json:"passed_over"`
				PassedOverInAll *int `json:"passed_over_in_all"`
			}
			if err := json.Unmarshal(written, &read); err != nil {
				t.Fatalf("read the reply: %v (%s)", err, written)
			}
			if read.PassedOverInAll == nil || *read.PassedOverInAll != c.passed || len(read.PassedOver) != c.listed {
				t.Fatalf("the reply lists %d of %v passed over, want %d of %d", len(read.PassedOver), read.PassedOverInAll, c.listed, c.passed)
			}
			if read.Applied == nil || *read.Applied != c.applied {
				t.Errorf("applied is %v for a run that wrote to %d and passed over %d, want %v", read.Applied, c.changed, c.passed, c.applied)
			}
			for i, entry := range read.PassedOver {
				if want := fmt.Sprintf("0198f3a0-0000-7000-8000-%012d", i); entry.InstanceID != want {
					t.Fatalf("entry %d is %s, want %s: the first the run came to, in its order", i, entry.InstanceID, want)
				}
			}
			if !strings.Contains(string(written), `"passed_over":[`) || strings.Contains(string(written), `"passed_over":null`) {
				t.Errorf("passed_over is not a list: %.200s", written)
			}
		})
	}
}

// The reply is written by hand (MarshalJSON), so a field added to it is not
// written until it is added there too — and nothing but this test would say
// so. A reply with something in every field is written with every field: one
// that is added to the struct and not to this test fails here for being
// empty, and one that is added here and not to the writing fails for being
// missing or for being written as nothing.
func TestEveryFieldOfTheMigrateReplyIsWritten(t *testing.T) {
	t.Parallel()
	full := MigrateInstancesResponse{
		Plan: fullPlan(), Applied: true, PassedOver: passedOverViews(passedOverBy(1)), PassedOverInAll: 1,
		PendingApproval: &entities.PendingApproval{RequestID: uuid.Must(uuid.NewV7()), Status: entities.DeviationRequestPending,
			RequestedBy: "boss", ExpiresAt: time.Now(), Because: []string{"a reason"}},
		Err: errors.New("refused"),
	}
	written, err := json.Marshal(full)
	if err != nil {
		t.Fatalf("write the reply: %v", err)
	}
	var keys map[string]json.RawMessage
	if err := json.Unmarshal(written, &keys); err != nil {
		t.Fatalf("read the reply: %v (%s)", err, written)
	}
	reply := reflect.TypeOf(full)
	var named []string
	for i := range reply.NumField() {
		field := reply.Field(i)
		if !field.IsExported() {
			continue
		}
		name, _, _ := strings.Cut(field.Tag.Get("json"), ",")
		if name == "" || name == "-" {
			t.Errorf("the field %s of the reply has no name to be written under", field.Name)
			continue
		}
		named = append(named, name)
		if reflect.ValueOf(full).Field(i).IsZero() {
			t.Errorf("this test leaves %s empty: give it a value, so that it is seen to be written", field.Name)
			continue
		}
		value, isWritten := keys[name]
		if !isWritten {
			t.Errorf("%s (%s) is in the reply and is not written: %s", field.Name, name, written)
			continue
		}
		// An error is written as an empty object, as it always was; a reply
		// that failed is answered as its failure and never written at all.
		if nothing := []string{"null", "false", "0", `""`, "[]", "{}"}; name != "err" && slices.Contains(nothing, string(value)) {
			t.Errorf("%s (%s) holds something and is written as %s", field.Name, name, value)
		}
	}
	if len(keys) != len(named) {
		t.Errorf("the reply is written with %d keys and has %d fields (%v): %s", len(keys), len(named), named, written)
	}
}
