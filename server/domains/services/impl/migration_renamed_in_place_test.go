package impl

import (
	"maps"
	"slices"
	"testing"

	"github.com/gsoultan/metis/server/repositories/models"
)

// line is a version whose steps run one after another, each to the next.
func line(ids ...string) models.ProcessDefinitionModel {
	var def models.ProcessDefinitionModel
	for i, id := range ids {
		def.Nodes = append(def.Nodes, models.FlowNode{ID: id})
		if i > 0 {
			def.Flows = append(def.Flows, models.SequenceFlow{ID: "f" + id, SourceRef: ids[i-1], TargetRef: id})
		}
	}
	return def
}

// A step renamed where it stands is a mapping onto a new id of a step whose
// own flows come from and go to the same steps in both versions. A new id is
// not enough: a step that stands somewhere else is another step.
func TestOnlyARenameWithTheSameNeighboursIsInPlace(t *testing.T) {
	t.Parallel()
	source := line("start", "prepare", "control", "sign", "end")
	cases := []struct {
		name    string
		target  models.ProcessDefinitionModel
		mapping map[string]string
		want    map[string]string
	}{
		{"the same place under a new id", line("start", "draft", "control", "sign", "end"),
			map[string]string{"prepare": "draft"}, map[string]string{"prepare": "draft"}},
		{"two neighbours renamed in place together", line("start", "draft", "check", "sign", "end"),
			map[string]string{"prepare": "draft", "control": "check"}, map[string]string{"prepare": "draft", "control": "check"}},
		{"the old step gone and the new one after the signature", line("start", "control", "sign", "file", "end"),
			map[string]string{"prepare": "file"}, map[string]string{}},
		{"the old step still there", line("start", "prepare", "control", "sign", "file", "end"),
			map[string]string{"prepare": "file"}, map[string]string{}},
		{"the same step before it and another after it", line("start", "draft", "sign", "end"),
			map[string]string{"prepare": "draft"}, map[string]string{}},
		{"another step before it and the same after it", line("start", "intake", "draft", "control", "sign", "end"),
			map[string]string{"prepare": "draft"}, map[string]string{}},
		{"a step the source has too", source, map[string]string{"prepare": "sign"}, map[string]string{}},
		{"a step mapped to itself", source, map[string]string{"prepare": "prepare"}, map[string]string{}},
		{"a step the new version does not have", line("start", "control", "sign", "end"),
			map[string]string{"prepare": "draft"}, map[string]string{}},
		{"no mapping", source, nil, map[string]string{}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := renamedInPlace(source, c.target, nodeIndex(source.Nodes), nodeIndex(c.target.Nodes), c.mapping)
			if !maps.Equal(got, c.want) {
				t.Errorf("renamedInPlace(%v) = %v, want %v", c.mapping, got, c.want)
			}
		})
	}
}

// A step inside a sub-process stands among that sub-process's flows, and is
// compared the same way.
func TestAStepRenamedInPlaceInsideASubProcess(t *testing.T) {
	t.Parallel()
	nested := func(inner ...string) models.ProcessDefinitionModel {
		def := line("start", "sub", "end")
		body := line(inner...)
		def.Nodes[1].Nodes, def.Nodes[1].Flows = body.Nodes, body.Flows
		return def
	}
	source := nested("in", "prepare", "control", "out")
	renamed := nested("in", "draft", "control", "out")
	moved := nested("in", "control", "draft", "out")
	mapping := map[string]string{"prepare": "draft"}
	if got := renamedInPlace(source, renamed, nodeIndex(source.Nodes), nodeIndex(renamed.Nodes), mapping); !maps.Equal(got, mapping) {
		t.Errorf("a nested step renamed where it stands: %v, want %v", got, mapping)
	}
	if got := renamedInPlace(source, moved, nodeIndex(source.Nodes), nodeIndex(moved.Nodes), mapping); len(got) != 0 {
		t.Errorf("a nested step given a new id beyond the control: %v, want none in place", got)
	}
}

// Three shapes have no place their own sequence flows describe. A mapping of
// one onto a new id is never counted as in place, so it is asked about like
// any other redirect.
func TestAShapeWithNoPlaceOfItsOwnIsNeverRenamedInPlace(t *testing.T) {
	t.Parallel()
	// Each on a copy: the versions of a case are built from one base, and
	// must not write into each other's lists.
	with := func(def models.ProcessDefinitionModel, nodes ...models.FlowNode) models.ProcessDefinitionModel {
		def.Nodes = append(slices.Clone(def.Nodes), nodes...)
		return def
	}
	flowed := func(def models.ProcessDefinitionModel, from, to string) models.ProcessDefinitionModel {
		def.Flows = append(slices.Clone(def.Flows), models.SequenceFlow{ID: from + "-" + to, SourceRef: from, TargetRef: to})
		return def
	}
	base := line("start", "prepare", "sign", "end")
	cases := []struct {
		name           string
		source, target models.ProcessDefinitionModel
		mapping        map[string]string
	}{
		{
			"a boundary event, attached to the same step and leading to the same one",
			flowed(with(base, models.FlowNode{ID: "late", Type: models.BoundaryEvent, AttachedToRef: "prepare"}), "late", "end"),
			flowed(with(base, models.FlowNode{ID: "overdue", Type: models.BoundaryEvent, AttachedToRef: "prepare"}), "overdue", "end"),
			map[string]string{"late": "overdue"},
		},
		{
			"an event sub-process",
			with(base, models.FlowNode{ID: "onCancel", Type: models.SubProcess, IsEventSubProcess: true}),
			with(base, models.FlowNode{ID: "onWithdrawn", Type: models.SubProcess, IsEventSubProcess: true}),
			map[string]string{"onCancel": "onWithdrawn"},
		},
		{
			// Nothing flows out of one. A definition that draws a flow from
			// it all the same has not given it a place.
			"an event sub-process a definition draws the same flow from",
			flowed(with(base, models.FlowNode{ID: "onCancel", Type: models.SubProcess, IsEventSubProcess: true}), "onCancel", "end"),
			flowed(with(base, models.FlowNode{ID: "onWithdrawn", Type: models.SubProcess, IsEventSubProcess: true}), "onWithdrawn", "end"),
			map[string]string{"onCancel": "onWithdrawn"},
		},
		{
			"a step with no flow in either version",
			with(base, models.FlowNode{ID: "adhoc"}),
			with(base, models.FlowNode{ID: "optional"}),
			map[string]string{"adhoc": "optional"},
		},
		{
			"a step with no flow in the source",
			with(base, models.FlowNode{ID: "adhoc"}),
			line("start", "prepare", "sign", "optional", "end"),
			map[string]string{"adhoc": "optional"},
		},
	}
	// The boundary event's fixture has the flows it says it has: without the
	// shape's own rule it would be taken as in place.
	if stood, stands := stepPlaces(cases[0].source)["late"], stepPlaces(cases[0].target)["overdue"]; len(stood.to) != 1 || len(stands.to) != 1 {
		t.Fatalf("the boundary event's fixture: flows from it in the source %v, in the target %v, want one each", stood.to, stands.to)
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			sourceNodes, targetNodes := nodeIndex(c.source.Nodes), nodeIndex(c.target.Nodes)
			for from, to := range c.mapping {
				if _, has := sourceNodes[from]; !has {
					t.Fatalf("the fixture's source has no %q", from)
				}
				if _, has := targetNodes[to]; !has {
					t.Fatalf("the fixture's target has no %q", to)
				}
			}
			if got := renamedSteps(sourceNodes, targetNodes, c.mapping); !maps.Equal(got, c.mapping) {
				t.Fatalf("the fixture is not a rename by ids: %v", got)
			}
			if got := renamedInPlace(c.source, c.target, sourceNodes, targetNodes, c.mapping); len(got) != 0 {
				t.Errorf("counted as renamed in place: %v", got)
			}
		})
	}
}
