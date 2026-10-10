package impl

import (
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/gsoultan/metis/internal/pkg/apierr"
	"github.com/gsoultan/metis/server/domains/entities"
)

// asStored is a document as a request's row gives it back: written as JSON
// and read again, so every number is a float and every list a list of any.
func asStored(t *testing.T, doc map[string]any) map[string]any {
	t.Helper()
	written, err := json.Marshal(doc)
	if err != nil {
		t.Fatalf("write the document: %v", err)
	}
	var read map[string]any
	if err := json.Unmarshal(written, &read); err != nil {
		t.Fatalf("read the document: %v", err)
	}
	return read
}

// What was asked for is what an approval applies: the command a request
// stores reads back as the command that was stored — the instance, the step,
// the reason, the visit and the values — and as an apply, never a dry run.
func TestAStoredWaiveReadsBackAsTheCommandThatWasAsked(t *testing.T) {
	instance := uuid.Must(uuid.NewV7())
	asked := entities.DeviationCommand{InstanceID: instance, Kind: entities.DeviationWaive, NodeID: "approve",
		Reason: "the CFO agreed", VisitKey: "dv1-the-visit", DryRun: true,
		Outputs: map[string]any{"approved": true, "amount": 900, "who": map[string]any{"name": "ana"}, "lines": []any{1, "two"}}}
	doc := waiveCommandDocument(asked)
	if doc["outputs"].(map[string]any)["amount"] = 901; asked.Outputs["amount"] != 900 {
		t.Fatal("the stored command shares its values with the command it was made from")
	}
	doc = waiveCommandDocument(asked)

	got, err := waiveCommandFrom(asStored(t, doc))
	if err != nil {
		t.Fatalf("read the stored waive: %v", err)
	}
	want := asked
	want.DryRun = false
	// As JSON keeps them: a number is a float, as the route reads one.
	want.Outputs = map[string]any{"approved": true, "amount": float64(900), "who": map[string]any{"name": "ana"}, "lines": []any{float64(1), "two"}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("the stored waive reads back as\n  %+v\nwant\n  %+v", got, want)
	}
	// The same values, to the comparison a retry is judged by.
	if same, err := sameRequest(entities.Deviation{Kind: entities.DeviationWaive, Node: &entities.Node{ID: "approve"}, Reason: "the CFO agreed",
		After: map[string]any{"variables": got.Outputs}}, asked); err != nil || !same {
		t.Fatalf("the values read back are not the values asked for: %v, %v", same, err)
	}

	// A waive that sets nothing stores no outputs, and reads back with none.
	bare := asked
	bare.Outputs = nil
	if _, has := waiveCommandDocument(bare)["outputs"]; has {
		t.Fatal("a waive that sets nothing stores outputs")
	}
	if got, err := waiveCommandFrom(asStored(t, waiveCommandDocument(bare))); err != nil || got.Outputs != nil {
		t.Fatalf("a waive that sets nothing reads back with %v (%v)", got.Outputs, err)
	}
}

// A stored command that cannot be read whole is never read as partly empty:
// it is the server's trouble, said plainly, and nothing is approved on it.
func TestAStoredWaiveThatCannotBeReadIsRefusedNotReadAsEmpty(t *testing.T) {
	instance := uuid.Must(uuid.NewV7()).String()
	whole := func(change func(map[string]any)) map[string]any {
		doc := map[string]any{"instance_id": instance, "kind": "waive", "node_id": "approve", "reason": "why", "visit_key": "dv1-k"}
		change(doc)
		return doc
	}
	if _, err := waiveCommandFrom(whole(func(map[string]any) {})); err != nil {
		t.Fatalf("a whole document: %v", err)
	}
	for name, doc := range map[string]map[string]any{
		"nothing stored":            nil,
		"an empty document":         {},
		"no instance":               whole(func(d map[string]any) { delete(d, "instance_id") }),
		"an instance that is no id": whole(func(d map[string]any) { d["instance_id"] = "the-first-one" }),
		"the nil instance":          whole(func(d map[string]any) { d["instance_id"] = uuid.Nil.String() }),
		"a cancel":                  whole(func(d map[string]any) { d["kind"] = "cancel" }),
		"no kind":                   whole(func(d map[string]any) { delete(d, "kind") }),
		"no step":                   whole(func(d map[string]any) { d["node_id"] = "" }),
		"no visit":                  whole(func(d map[string]any) { delete(d, "visit_key") }),
		"outputs that are a list":   whole(func(d map[string]any) { d["outputs"] = []any{"approved"} }),
		"a step that is a number":   whole(func(d map[string]any) { d["node_id"] = 7 }),
		"a field nothing wrote":     whole(func(d map[string]any) { d["dry_run"] = true }),
	} {
		got, err := waiveCommandFrom(doc)
		if err == nil {
			t.Errorf("%s: read as %+v, want it refused", name, got)
			continue
		}
		for class, kind := range map[string]error{"invalid": apierr.ErrInvalidArgument, "not found": apierr.ErrNotFound, "forbidden": apierr.ErrForbidden} {
			if errors.Is(err, kind) {
				t.Errorf("%s: %v is answered as %s, want a plain error", name, err, class)
			}
		}
	}
}

// A request is approved only as its own command: one that names another
// instance than the request does, or another visit, is refused.
func TestARequestIsApprovedOnlyAsItsOwnCommand(t *testing.T) {
	instance := uuid.Must(uuid.NewV7())
	command := entities.DeviationCommand{InstanceID: instance, Kind: entities.DeviationWaive, NodeID: "approve", Reason: "why", VisitKey: "dv1-k"}
	request := entities.DeviationRequest{ID: uuid.Must(uuid.NewV7()), Instance: &entities.ProcessInstance{ID: instance},
		Fingerprint: "dv1-k", Command: asStored(t, waiveCommandDocument(command))}
	got, err := commandOf(request)
	if err != nil || got.DryRun || got.VisitKey != "dv1-k" || got.InstanceID != instance {
		t.Fatalf("its own command: %+v, %v", got, err)
	}
	for name, change := range map[string]func(*entities.DeviationRequest){
		"another instance": func(r *entities.DeviationRequest) {
			r.Instance = &entities.ProcessInstance{ID: uuid.Must(uuid.NewV7())}
		},
		"no instance":   func(r *entities.DeviationRequest) { r.Instance = nil },
		"another visit": func(r *entities.DeviationRequest) { r.Fingerprint = "dv1-another" },
		"no command":    func(r *entities.DeviationRequest) { r.Command = nil },
	} {
		other := request
		change(&other)
		_, err := commandOf(other)
		if err == nil || !strings.Contains(err.Error(), request.ID.String()) || errors.Is(err, apierr.ErrInvalidArgument) {
			t.Errorf("%s: %v, want a plain refusal naming the request", name, err)
		}
	}
}

// The plan a request keeps is the plan as shown: every list beside its count,
// the warnings, whether it could be applied — and why it needs somebody else.
// It shares nothing with the plan it was made from.
func TestAStoredPlanIsACopyThatSaysHowMuchThereIs(t *testing.T) {
	task, called := uuid.Must(uuid.NewV7()), uuid.Must(uuid.NewV7())
	plan := entities.DeviationPlan{
		InstanceID: uuid.Must(uuid.NewV7()), Kind: entities.DeviationWaive, Scope: entities.DeviationScopeTask,
		NodeID: "approve", NodeName: "Approve", VisitKey: "dv1-k",
		OpenWork: []entities.DeviationOpenWork{
			{TaskID: task, Name: "Approve", NodeID: "approve", NodeName: "Approve", Status: entities.TaskUnclaimed},
			{TaskID: task, Name: "Approve", NodeID: "approve", NodeName: "Approve", Status: entities.TaskUnclaimed, Assignee: "ollie", IterationID: "2"},
		},
		OpenWorkInAll: 5000,
		Outputs:       map[string]any{"approved": true},
		DecisionPoints: []entities.DecisionPoint{{NodeID: "decide", NodeName: "Approved?", Kind: "gateway",
			Reads: []string{"approved"}, ReadsInAll: 12, Supplied: []string{"approved"}, MissingInAll: 3, Analysed: true}},
		DecisionPointsInAll: 140, Missing: []string{"amount"}, MissingInAll: 60,
		CalledInstances: []uuid.UUID{called}, CalledInstancesInAll: 300, RequiresSecondApprover: true,
		Warnings: []string{"“Approve” is marked as a control. Waiving it is recorded as a control that was not performed."},
	}
	because := []string{"“Approve” would be waived: nobody performs it, and the process moves on"}
	doc := waivePlanDocument(plan, because)

	for key, want := range map[string]any{
		"open_work_in_all": 5000, "decision_points_in_all": 140, "missing_in_all": 60, "called_instances_in_all": 300,
		"requires_second_approver": true, "applicable": true, "node_name": "Approve", "visit_key": "dv1-k", "kind": "waive", "scope": "task",
	} {
		if doc[key] != want {
			t.Errorf("%s is %v, want %v", key, doc[key], want)
		}
	}
	stored := asStored(t, doc)
	work := stored["open_work"].([]any)
	if _, held := work[0].(map[string]any)["assignee"]; held || work[1].(map[string]any)["assignee"] != "ollie" || work[1].(map[string]any)["iteration_id"] != "2" {
		t.Errorf("the open work is stored as %v; nobody's task names no holder, and ollie's names him", work)
	}
	point := stored["decision_points"].([]any)[0].(map[string]any)
	if point["reads_in_all"] != float64(12) || point["missing_in_all"] != float64(3) || len(point["missing"].([]any)) != 0 || point["analysed"] != true {
		t.Errorf("the decision point is stored as %v", point)
	}
	if !reflect.DeepEqual(stored["called_instances"], []any{called.String()}) || !reflect.DeepEqual(stored["refusals"], []any{}) {
		t.Errorf("called instances %v, refusals %v", stored["called_instances"], stored["refusals"])
	}
	if got := (entities.DeviationRequest{Plan: stored}).Because(); !reflect.DeepEqual(got, because) {
		t.Errorf("the request reads why as %v, want %v", got, because)
	}

	// Changing the plan afterwards changes nothing that was kept.
	plan.Warnings[0], plan.Missing[0], plan.Outputs["approved"], because[0] = "changed", "changed", false, "changed"
	plan.DecisionPoints[0].Reads[0], plan.OpenWork[0].Name = "changed", "changed"
	if written := mustMarshal(t, doc); strings.Contains(written, "changed") || !strings.Contains(written, `"approved":true`) {
		t.Fatalf("the stored plan changed with the plan it was made from: %s", written)
	}
	// A plan that refuses says so.
	plan.Refusals = []string{"no"}
	if doc := waivePlanDocument(plan, nil); doc["applicable"] != false || len(doc["because"].([]string)) != 0 {
		t.Errorf("a refused plan is stored as applicable %v, because %v", doc["applicable"], doc["because"])
	}
}

func mustMarshal(t *testing.T, value any) string {
	t.Helper()
	written, err := json.Marshal(value)
	if err != nil {
		t.Fatalf("write %v: %v", value, err)
	}
	return string(written)
}
