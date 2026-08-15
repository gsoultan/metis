package handlers_test

import (
	"encoding/json"
	"reflect"
	"testing"

	domainadapters "github.com/gsoultan/gobpm/server/domains/adapters"
	"github.com/gsoultan/gobpm/server/domains/entities"
	"github.com/gsoultan/gobpm/server/transports/adapters"
)

// A node must survive the trip to protobuf and back with everything it carries.
//
// The protobuf Node used to stop at candidate_groups, so the settings that make
// a node do anything — decision_key on a business rule task, http_url and the
// input_/output_ mappings on a service task — had no field to travel in. The
// definition still saved and still came back; it just came back inert, and the
// loss surfaced much later as a process that ran and did nothing.
//
// The flows had the same shape of problem from the other end: protobuf modelled
// an arrow as holding whole copies of the nodes it joins, while the UI sends
// source_ref and target_ref, so every flow arrived with both ends empty and the
// engine rejected the definition with "Sequence flow f1 has no source
// reference".
//
// This is written as a whole-struct comparison rather than a list of assertions
// so that a field added to the domain node fails here until it is mapped,
// rather than being dropped in silence for someone to find in production.
func TestNode_SurvivesTheRoundTripToProtobuf(t *testing.T) {
	src := fullyPopulatedNode()

	// Guard the guard: if a new field was added to entities.Node and not set
	// below, the comparison would pass by comparing two zero values.
	if zero := zeroFields(reflect.ValueOf(*src)); len(zero) > 0 {
		t.Fatalf("this test no longer covers every field of entities.Node.\n"+
			"Set %v in fullyPopulatedNode, and map it in adapters/node.go.", zero)
	}

	got := adapters.NodeFromProto(adapters.NodeToProto(src))
	if got == nil {
		t.Fatal("round trip produced nil")
	}

	for _, name := range differingFields(reflect.ValueOf(*src), reflect.ValueOf(*got)) {
		t.Errorf("field %s did not survive the round trip:\n  sent %#v\n  got  %#v",
			name,
			reflect.ValueOf(*src).FieldByName(name).Interface(),
			reflect.ValueOf(*got).FieldByName(name).Interface())
	}
}

func TestFlow_SurvivesTheRoundTripToProtobuf(t *testing.T) {
	src := &entities.SequenceFlow{
		ID:            "f4",
		SourceRef:     "route",
		TargetRef:     "ask-director",
		Condition:     "approvalLevel = director",
		Documentation: "Taken when the decision asked for a director.",
	}

	if zero := zeroFields(reflect.ValueOf(*src)); len(zero) > 0 {
		t.Fatalf("this test no longer covers every field of entities.SequenceFlow: %v", zero)
	}

	got := adapters.FlowFromProto(adapters.FlowToProto(src))
	if got == nil {
		t.Fatal("round trip produced nil")
	}
	if *got != *src {
		t.Errorf("flow changed in transit:\n  sent %#v\n  got  %#v", *src, *got)
	}
}

// Properties are free-form, so they travel as a protobuf Struct. That is a JSON
// value type: it has one number kind, and an integer comes back as a float.
// Worth pinning rather than discovering — a property read with a type assertion
// to int would start failing.
func TestNodeProperties_KeepTheirValuesWithJSONNumberTypes(t *testing.T) {
	src := &entities.Node{
		ID: "lookup",
		Properties: map[string]any{
			"http_url":            "https://api.example.com/companies/lookup",
			"input_companyNumber": "registration_id",
			"retries":             3,
			"follow_redirects":    true,
		},
	}

	got := adapters.NodeFromProto(adapters.NodeToProto(src))

	if got.Properties["http_url"] != "https://api.example.com/companies/lookup" {
		t.Errorf("http_url = %#v", got.Properties["http_url"])
	}
	if got.Properties["input_companyNumber"] != "registration_id" {
		t.Errorf("input_companyNumber = %#v", got.Properties["input_companyNumber"])
	}
	if got.Properties["follow_redirects"] != true {
		t.Errorf("follow_redirects = %#v", got.Properties["follow_redirects"])
	}
	if n, ok := got.Properties["retries"].(float64); !ok || n != 3 {
		t.Errorf("retries = %#v, want float64(3) — Struct has a single number kind", got.Properties["retries"])
	}
}

// An empty node must not gain fields on the way through, or every definition
// read back would differ from the one that was written.
func TestNode_EmptyStaysEmpty(t *testing.T) {
	got := adapters.NodeFromProto(adapters.NodeToProto(&entities.Node{ID: "start", Type: "startEvent"}))
	if got.Properties != nil {
		t.Errorf("properties = %#v, want nil", got.Properties)
	}
	if got.Nodes != nil || got.Flows != nil {
		t.Errorf("children = %#v / %#v, want nil", got.Nodes, got.Flows)
	}
	if len(got.Incoming) != 0 || len(got.Outgoing) != 0 {
		t.Errorf("incoming/outgoing = %#v / %#v, want empty", got.Incoming, got.Outgoing)
	}
}

func fullyPopulatedNode() *entities.Node {
	return &entities.Node{
		ID:                  "lookup",
		Name:                "Look up the company",
		Type:                entities.NodeType("serviceTask"),
		Assignee:            "carol",
		CandidateUsers:      []*entities.User{{Username: "dave", FullName: "Dave"}},
		CandidateGroups:     []*entities.Group{{Name: "compliance"}},
		Priority:            7,
		DueDate:             "2026-01-31",
		FormKey:             "supplier-review",
		DefaultFlow:         "f9",
		Script:              "return 1;",
		ScriptFormat:        "javascript",
		ExternalTopic:       "supplier-lookup",
		Documentation:       "Calls the registry.",
		AttachedToRef:       "risk",
		ParentID:            "sub",
		CancelActivity:      true,
		MultiInstanceType:   "parallel",
		LoopCardinality:     3,
		Collection:          "suppliers",
		ElementVariable:     "supplier",
		CompletionCondition: "done",
		ErrorCode:           "LOOKUP_FAILED",
		IsAdHoc:             true,
		IsEventSubProcess:   true,
		Incoming:            []string{"f1"},
		Outgoing:            []string{"f2"},
		X:                   640,
		Y:                   300,
		Condition:           "creditScore = low",
		Properties:          map[string]any{"http_url": "https://example.invalid", "input_companyNumber": "registration_id"},
		Nodes:               []*entities.Node{{ID: "inner", Name: "Inner", Type: "task"}},
		Flows:               []*entities.SequenceFlow{{ID: "if1", SourceRef: "inner", TargetRef: "inner-end"}},
	}
}

// zeroFields names the exported fields left at their zero value.
func zeroFields(v reflect.Value) []string {
	var out []string
	t := v.Type()
	for i := range t.NumField() {
		if t.Field(i).PkgPath != "" {
			continue // unexported
		}
		if v.Field(i).IsZero() {
			out = append(out, t.Field(i).Name)
		}
	}
	return out
}

// differingFields names the exported fields that are not deeply equal.
func differingFields(a, b reflect.Value) []string {
	var out []string
	t := a.Type()
	for i := range t.NumField() {
		if t.Field(i).PkgPath != "" {
			continue
		}
		if !reflect.DeepEqual(a.Field(i).Interface(), b.Field(i).Interface()) {
			out = append(out, t.Field(i).Name)
		}
	}
	return out
}

// A node must survive being stored and read back with everything it carries.
//
// The database model is a second hand-written copy of the domain node, and it
// had no column for the error code an error boundary event catches. So the code
// was dropped on save and every boundary came back as a catch-all: the path
// meant for "the card was declined" was taken for a timeout, a bad URL, or
// anything else that went wrong.
//
// Whole-struct comparison for the same reason as the protobuf one above — a
// field added to the domain node fails here until it is stored, rather than
// being dropped in silence.
func TestNode_SurvivesBeingStoredAndReadBack(t *testing.T) {
	src := fullyPopulatedNode()

	if zero := zeroFields(reflect.ValueOf(*src)); len(zero) > 0 {
		t.Fatalf("this test no longer covers every field of entities.Node.\n"+
			"Set %v in fullyPopulatedNode, and store it in adapters/definition.go.", zero)
	}

	stored := domainadapters.DefinitionModelAdapter{Definition: &entities.ProcessDefinition{
		Key:   "round-trip",
		Name:  "Round trip",
		Nodes: []*entities.Node{src},
	}}.ToModel()

	read := domainadapters.DefinitionEntityAdapter{Model: stored}.ToEntity()
	if len(read.Nodes) != 1 {
		t.Fatalf("the definition came back with %d nodes", len(read.Nodes))
	}

	got := read.Nodes[0]

	// A candidate user is stored as a username: the definition refers to a
	// person, it does not keep a copy of their name, which would go stale the
	// moment they changed it. So compare what is meant to be stored.
	if len(got.CandidateUsers) != len(src.CandidateUsers) {
		t.Fatalf("candidate users: stored %d, read %d", len(src.CandidateUsers), len(got.CandidateUsers))
	}
	for i := range src.CandidateUsers {
		if got.CandidateUsers[i].Username != src.CandidateUsers[i].Username {
			t.Errorf("candidate user %d = %q, want %q", i, got.CandidateUsers[i].Username, src.CandidateUsers[i].Username)
		}
	}

	// Compared as JSON: the entity's fields are omitzero, so a nil slice and an
	// empty one read the same, which is the difference a trip through the
	// database introduces and the one thing it is not worth failing over.
	// Candidate users are excluded, having been compared above by what is
	// actually stored.
	saved, loaded := *src, *got
	saved.CandidateUsers, loaded.CandidateUsers = nil, nil
	if asJSON(t, saved) == asJSON(t, loaded) {
		return
	}

	for _, name := range differingFieldsIgnoringEmptiness(reflect.ValueOf(saved), reflect.ValueOf(loaded)) {
		t.Errorf("field %s did not survive being stored:\n  saved %s\n  read  %s",
			name,
			asJSON(t, reflect.ValueOf(saved).FieldByName(name).Interface()),
			asJSON(t, reflect.ValueOf(loaded).FieldByName(name).Interface()))
	}
}

// asJSON renders a value with anything empty removed, at every depth.
//
// Storing a node writes JSON and reading it back builds fresh slices, so a
// child that was saved with no candidate users comes back with none rather than
// with nil. That is the same node either way, and not what this test is for.
func asJSON(t *testing.T, v any) string {
	t.Helper()
	raw, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var decoded any
	if err := json.Unmarshal(raw, &decoded); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	out, err := json.Marshal(withoutEmpties(decoded))
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	return string(out)
}

func withoutEmpties(v any) any {
	switch value := v.(type) {
	case map[string]any:
		out := make(map[string]any, len(value))
		for k, item := range value {
			cleaned := withoutEmpties(item)
			if isEmptyJSON(cleaned) {
				continue
			}
			out[k] = cleaned
		}
		return out
	case []any:
		out := make([]any, 0, len(value))
		for _, item := range value {
			out = append(out, withoutEmpties(item))
		}
		return out
	default:
		return v
	}
}

func isEmptyJSON(v any) bool {
	switch value := v.(type) {
	case map[string]any:
		return len(value) == 0
	case []any:
		return len(value) == 0
	case nil:
		return true
	default:
		return false
	}
}

// differingFieldsIgnoringEmptiness is differingFields, treating a nil slice or
// map and an empty one as the same thing.
//
// Going through JSON turns one into the other and back, which says nothing
// about whether the value was kept.
func differingFieldsIgnoringEmptiness(a, b reflect.Value) []string {
	var out []string
	t := a.Type()
	for i := range t.NumField() {
		if t.Field(i).PkgPath != "" {
			continue
		}
		x, y := a.Field(i), b.Field(i)
		if bothEmpty(x, y) || reflect.DeepEqual(x.Interface(), y.Interface()) {
			continue
		}
		out = append(out, t.Field(i).Name)
	}
	return out
}

func bothEmpty(a, b reflect.Value) bool {
	switch a.Kind() {
	case reflect.Slice, reflect.Map:
		return a.Len() == 0 && b.Len() == 0
	default:
		return false
	}
}
