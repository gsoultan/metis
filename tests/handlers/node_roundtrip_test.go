package handlers_test

import (
	"reflect"
	"testing"

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
