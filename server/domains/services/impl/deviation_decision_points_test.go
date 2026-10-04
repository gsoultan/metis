package impl

import (
	"fmt"
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/gsoultan/metis/server/domains/entities"
	"github.com/gsoultan/metis/server/domains/logic"
)

func flow(id, from, to, condition string) *entities.SequenceFlow {
	return &entities.SequenceFlow{ID: id, SourceRef: from, TargetRef: to, Condition: condition}
}

func pointAt(points []entities.DecisionPoint, nodeID string, kind entities.DecisionPointKind) (entities.DecisionPoint, bool) {
	for _, p := range points {
		if p.NodeID == nodeID && p.Kind == kind {
			return p, true
		}
	}
	return entities.DecisionPoint{}, false
}

// declares is the set of fields a step's form declares.
func declares(names ...string) map[string]struct{} {
	set := make(map[string]struct{}, len(names))
	for _, name := range names {
		set[name] = struct{}{}
	}
	return set
}

// versionsOf is a decision lookup over what each version of each decision
// reads; version 0 is the live one.
func versionsOf(reads map[string]map[int][]string) decisionReads {
	return func(key string, version int) ([]string, bool, bool) {
		names, found := reads[key][version]
		return names, true, found
	}
}

// missesOnly reports whether the point at nodeID of that kind is listed and is
// missing exactly names.
func missesOnly(t *testing.T, points []entities.DecisionPoint, nodeID string, kind entities.DecisionPointKind, names ...string) entities.DecisionPoint {
	t.Helper()
	point, listed := pointAt(points, nodeID, kind)
	if !listed {
		t.Errorf("%s (%s) is not listed", nodeID, kind)
		return point
	}
	if !slices.Equal(point.Missing, names) {
		t.Errorf("%s (%s) is missing %v, want %v: %+v", nodeID, kind, point.Missing, names, point)
	}
	return point
}

func TestDecisionPointsReading(t *testing.T) {
	t.Parallel()
	def := &entities.ProcessDefinition{
		Nodes: []*entities.Node{
			{ID: "start", Type: entities.StartEvent},
			{ID: "review", Type: entities.UserTask, Name: "Review"},
			{ID: "g1", Type: entities.ExclusiveGateway, Name: "Approved?", DefaultFlow: "g1-other"},
			{ID: "check", Type: entities.BusinessRuleTask, Name: "Refund policy", Properties: map[string]any{"decision_key": "refundPolicy"}},
			{ID: "g2", Type: entities.ExclusiveGateway, Name: "Route by script"},
			{ID: "work", Type: entities.UserTask, Name: "Do the work"},
			{ID: "watch", Type: entities.BoundaryEvent, Name: "Approval withdrawn", AttachedToRef: "work",
				Properties: map[string]any{"condition_expression": "approved"}},
			{ID: "sub", Type: entities.SubProcess, Name: "Pay out", Nodes: []*entities.Node{
				{ID: "sub-start", Type: entities.StartEvent, ParentID: "sub"},
				{ID: "inner", Type: entities.ExclusiveGateway, Name: "Inside", ParentID: "sub"},
				{ID: "sub-end", Type: entities.EndEvent, ParentID: "sub"},
			}, Flows: []*entities.SequenceFlow{
				flow("s1", "sub-start", "inner", ""), flow("s2", "inner", "sub-end", "amount > 10"),
			}},
			{ID: "each", Type: entities.UserTask, Name: "Each reviewer signs", MultiInstanceType: "parallel",
				Collection: "reviewers", CompletionCondition: "nrOfCompletedInstances >= 2 or approved = false"},
			{ID: "settle", Type: entities.CallActivity, Name: "Settle", Properties: map[string]any{"called_process_key": "settlement"}},
			{ID: "unrelated", Type: entities.ExclusiveGateway, Name: "Unrelated"},
			{ID: "end", Type: entities.EndEvent},
			{ID: "stopped", Type: entities.EndEvent},
			{ID: "escalate", Type: entities.SubProcess, IsEventSubProcess: true, Nodes: []*entities.Node{
				{ID: "esc-start", Type: entities.StartEvent, ParentID: "escalate", Properties: map[string]any{"condition_expression": "approved and amount > 900"}},
			}},
		},
		Flows: []*entities.SequenceFlow{
			flow("f1", "start", "review", ""), flow("f2", "review", "g1", ""),
			flow("g1-yes", "g1", "check", "approved"), flow("g1-other", "g1", "check", ""),
			flow("f3", "check", "g2", ""),
			flow("g2-js", "g2", "work", "js:approved === true"), flow("g2-plain", "g2", "work", "approved = false"),
			flow("f4", "work", "sub", ""), flow("w1", "watch", "stopped", ""),
			flow("f5", "sub", "each", ""), flow("f6", "each", "settle", ""), flow("f7", "settle", "unrelated", ""),
			flow("u1", "unrelated", "end", "region = 'EU'"), flow("loop", "unrelated", "review", "retry"),
		},
	}
	reads := versionsOf(map[string]map[int][]string{"refundPolicy": {0: {"amount", "approved"}}})
	taken := declares("approved", "reviewers")
	points := decisionPointsReading(def, "review", taken, map[string]any{}, reads).points

	g1 := missesOnly(t, points, "g1", entities.DecisionPointGateway, "approved")
	if !g1.HasDefaultFlow || !g1.Analysed || g1.NodeName != "Approved?" || !slices.Equal(g1.Reads, []string{"approved"}) || g1.ReadsInAll != 1 {
		t.Errorf("g1: %+v", g1)
	}
	table := missesOnly(t, points, "check", entities.DecisionPointDecisionTable, "approved")
	// Reads is what the point takes of the step's fields; the table reads
	// amount too, which is counted and not listed.
	if table.NodeName != "Refund policy" || !table.Analysed || !slices.Equal(table.Reads, []string{"approved"}) || table.ReadsInAll != 2 {
		t.Errorf("the decision table: %+v", table)
	}
	if p := missesOnly(t, points, "g2", entities.DecisionPointGateway, "approved"); p.Analysed || p.HasDefaultFlow ||
		!slices.Equal(p.Reads, []string{"approved"}) || p.ReadsInAll != 1 {
		t.Errorf("a js: gateway must be listed as not analysable, with what could be read of it: %+v", p)
	}
	// A point lists what it takes of the step's fields and counts everything
	// it reads: the completion condition reads the engine's own counter too.
	if p := missesOnly(t, points, "each", entities.DecisionPointCompletionCondition, "approved"); !slices.Equal(p.Reads, []string{"approved"}) || p.ReadsInAll != 2 {
		t.Errorf("the completion condition: %+v", p)
	}
	missesOnly(t, points, "watch", entities.DecisionPointConditionalEvent, "approved")
	missesOnly(t, points, "esc-start", entities.DecisionPointConditionalEvent, "approved")
	missesOnly(t, points, "each", entities.DecisionPointCollection, "reviewers")
	missesOnly(t, points, "each", entities.DecisionPointCompletionCondition, "approved")
	// A called process is handed the values and is not read: a warning, with
	// nothing known to be missing.
	if p := missesOnly(t, points, "settle", entities.DecisionPointCalledProcess); p.Analysed || !slices.Equal(p.Reads, []string{"approved", "reviewers"}) || p.ReadsInAll != 2 {
		t.Errorf("the called process: %+v", p)
	}
	for _, id := range []string{"inner", "unrelated"} {
		if _, ok := pointAt(points, id, entities.DecisionPointGateway); ok {
			t.Errorf("%s reads nothing the step sets and is listed", id)
		}
	}
	if len(points) != 8 {
		t.Errorf("%d points listed, want 8: %+v", len(points), points)
	}
	if !slices.IsSortedFunc(points, byNodeThenKind) {
		t.Errorf("not sorted by node id, then kind: %+v", points)
	}

	// Supplying the value clears what is missing and keeps the point listed.
	supplied := decisionPointsReading(def, "review", taken, map[string]any{"approved": false, "reviewers": []any{"ann"}}, reads).points
	if p := missesOnly(t, supplied, "g1", entities.DecisionPointGateway); !slices.Equal(p.Supplied, []string{"approved"}) {
		t.Errorf("with approved supplied, g1 is %+v", p)
	}
	if p := missesOnly(t, supplied, "settle", entities.DecisionPointCalledProcess); !slices.Equal(p.Supplied, []string{"approved", "reviewers"}) {
		t.Errorf("with both supplied, the called process is %+v", p)
	}
	missesOnly(t, supplied, "each", entities.DecisionPointCollection)
	if len(supplied) != len(points) {
		t.Errorf("%d points listed with the values supplied, %d without", len(supplied), len(points))
	}

	// A decision nobody can find cannot be read; it is a warning, not a guess.
	for name, lookup := range map[string]decisionReads{"no such decision": versionsOf(nil), "no lookup": nil} {
		unknown := decisionPointsReading(def, "review", taken, map[string]any{}, lookup).points
		if p, ok := pointAt(unknown, "check", entities.DecisionPointDecisionTable); !ok || p.Analysed {
			t.Errorf("%s: %+v (found %v)", name, p, ok)
		}
	}
}

// Following the flows from the step misses what the engine reaches another
// way, so nothing is followed: a decision point is listed wherever in the
// definition it is.
func TestADecisionPointIsListedWhereverTheInstanceCouldMeetIt(t *testing.T) {
	t.Parallel()
	def := &entities.ProcessDefinition{
		Nodes: []*entities.Node{
			{ID: "start", Type: entities.StartEvent},
			{ID: "before", Type: entities.ExclusiveGateway, Name: "Already passed"},
			{ID: "split", Type: entities.ParallelGateway},
			{ID: "handle", Type: entities.SubProcess, Name: "Handle the claim", Nodes: []*entities.Node{
				{ID: "h-start", Type: entities.StartEvent, ParentID: "handle"},
				{ID: "review", Type: entities.UserTask, ParentID: "handle"},
				{ID: "h-end", Type: entities.EndEvent, ParentID: "handle"},
			}, Flows: []*entities.SequenceFlow{flow("h1", "h-start", "review", ""), flow("h2", "review", "h-end", "")}},
			// On the sub-process the step is inside: armed when the instance
			// entered it, and still armed after the waive.
			{ID: "timeout", Type: entities.BoundaryEvent, AttachedToRef: "handle", Properties: map[string]any{"timer_duration": "PT1H"}},
			{ID: "late", Type: entities.ExclusiveGateway, Name: "Late"},
			// On a branch running beside the step.
			{ID: "wait", Type: entities.IntermediateCatchEvent, Properties: map[string]any{"timer_duration": "P1D"}},
			{ID: "side", Type: entities.InclusiveGateway, Name: "Beside"},
			// In an event sub-process a message starts at any time.
			{ID: "withdrawn", Type: entities.SubProcess, IsEventSubProcess: true},
			{ID: "w-start", Type: entities.StartEvent, ParentID: "withdrawn", Properties: map[string]any{"message_name": "customer_withdraws"}},
			{ID: "w-gate", Type: entities.ExclusiveGateway, ParentID: "withdrawn", Name: "Was it approved?"},
			// In an ad-hoc sub-process, where a step is started by hand.
			{ID: "research", Type: entities.SubProcess, IsAdHoc: true, Nodes: []*entities.Node{
				{ID: "r-task", Type: entities.UserTask, ParentID: "research"},
				{ID: "r-gate", Type: entities.ExclusiveGateway, ParentID: "research"},
			}, Flows: []*entities.SequenceFlow{flow("r1", "r-task", "r-gate", ""), flow("r2", "r-gate", "r-task", "approved")}},
			{ID: "end", Type: entities.EndEvent},
		},
		Flows: []*entities.SequenceFlow{
			flow("f0", "start", "before", ""), flow("b1", "before", "split", "approved"),
			flow("f1", "split", "handle", ""), flow("f2", "split", "wait", ""), flow("f3", "wait", "side", ""),
			flow("s1", "side", "end", "not(approved)"),
			flow("t1", "timeout", "late", ""), flow("l1", "late", "end", "approved = true"),
			flow("w1", "w-start", "w-gate", ""), flow("w2", "w-gate", "end", "approved"),
			flow("f4", "handle", "research", ""), flow("f5", "research", "end", ""),
		},
	}
	points := decisionPointsReading(def, "review", declares("approved"), nil, nil).points
	for _, id := range []string{"late", "side", "w-gate", "r-gate", "before"} {
		missesOnly(t, points, id, entities.DecisionPointGateway, "approved")
	}
	if len(points) != 5 {
		t.Errorf("%d points listed, want 5: %+v", len(points), points)
	}
}

// A step inside an ad-hoc sub-process has no way out of its own: finishing it
// re-reads the sub-process's completion condition, which is the decision point.
func TestTheCompletionConditionOfTheAdHocSubProcessIsADecisionPoint(t *testing.T) {
	t.Parallel()
	def := &entities.ProcessDefinition{
		Nodes: []*entities.Node{
			{ID: "research", Type: entities.SubProcess, Name: "Research the claim", IsAdHoc: true, CompletionCondition: "reviewsDone >= 2",
				Nodes: []*entities.Node{{ID: "call", Type: entities.UserTask, ParentID: "research"}}},
			{ID: "decide", Type: entities.UserTask},
		},
		Flows: []*entities.SequenceFlow{flow("f", "research", "decide", "")},
	}
	points := decisionPointsReading(def, "call", declares("reviewsDone"), nil, nil).points
	if p := missesOnly(t, points, "research", entities.DecisionPointCompletionCondition, "reviewsDone"); p.NodeName != "Research the claim" {
		t.Fatalf("the ad-hoc completion condition: %+v", p)
	}
}

// The waived step is ended whole: what it repeats over and when it stops
// repeating are not asked again for this visit. A completion condition nothing
// evaluates — on a step that neither repeats nor is ad-hoc — is not one.
func TestWhatTheWaivedStepRepeatsOverIsNotListed(t *testing.T) {
	t.Parallel()
	def := &entities.ProcessDefinition{Nodes: []*entities.Node{
		{ID: "review", Type: entities.UserTask, MultiInstanceType: "parallel", Collection: "reviewers", CompletionCondition: "approved"},
		{ID: "again", Type: entities.UserTask, MultiInstanceType: "sequential", Collection: "reviewers", CompletionCondition: "approved"},
		{ID: "once", Type: entities.UserTask, MultiInstanceType: "none", Collection: "reviewers", CompletionCondition: "approved"},
		{ID: "counted", Type: entities.ServiceTask, MultiInstanceType: "parallel", Collection: "suppliers"},
	}}
	taken := declares("approved", "reviewers")
	points := decisionPointsReading(def, "review", taken, nil, nil).points
	missesOnly(t, points, "again", entities.DecisionPointCollection, "reviewers")
	missesOnly(t, points, "again", entities.DecisionPointCompletionCondition, "approved")
	if len(points) != 2 {
		t.Errorf("only the other repeating step is listed; got %+v", points)
	}
	// Waiving the other one lists this one instead.
	if other := decisionPointsReading(def, "again", taken, nil, nil).points; len(other) != 2 || other[0].NodeID != "review" || other[1].NodeID != "review" {
		t.Errorf("waiving the other step: %+v", other)
	}
}

// The waived step is told from the others by its id. When two nodes carry
// that id, which of them the instance is at cannot be told, so neither is
// taken for the step: the list and the completion condition of both are
// asked for. One value more than might be needed, never one fewer.
func TestAWaivedStepWhoseIdIsUsedTwiceIsAskedForItsOwnListToo(t *testing.T) {
	t.Parallel()
	def := &entities.ProcessDefinition{Nodes: []*entities.Node{
		{ID: "review", Type: entities.UserTask, Name: "Review"},
		{ID: "review", Type: entities.UserTask, Name: "Review again", MultiInstanceType: "parallel", Collection: "reviewers", CompletionCondition: "approved"},
	}}
	points := decisionPointsReading(def, "review", declares("approved", "reviewers"), nil, nil).points
	missesOnly(t, points, "review", entities.DecisionPointCollection, "reviewers")
	missesOnly(t, points, "review", entities.DecisionPointCompletionCondition, "approved")
	if len(points) != 2 {
		t.Errorf("%d points listed, want the list and the completion condition: %+v", len(points), points)
	}
}

// The engine evaluates the version a step pins and feeds the table through
// the step's input mapping, so that is what is read here.
func TestADecisionTableIsReadAsTheEngineEvaluatesIt(t *testing.T) {
	t.Parallel()
	rule := func(id, key string, more map[string]any) *entities.Node {
		properties := map[string]any{"decision_key": key}
		for name, value := range more {
			properties[name] = value
		}
		return &entities.Node{ID: id, Type: entities.BusinessRuleTask, Properties: properties}
	}
	def := &entities.ProcessDefinition{Nodes: []*entities.Node{
		rule("live", "refundPolicy", nil),
		rule("pinned", "refundPolicy", map[string]any{"decision_version": 3.0}),
		rule("pinned-int", "refundPolicy", map[string]any{"decision_version": 3}),
		rule("pinned-text", "refundPolicy", map[string]any{"decision_version": "3"}),
		rule("gone", "refundPolicy", map[string]any{"decision_version": 9.0}),
		rule("renamed", "limits", map[string]any{"input_mapping": map[string]any{"ok": "approved", "sum": "price * quantity"}}),
		rule("fed", "refundPolicy", map[string]any{"decision_version": 3.0, "input_mapping": map[string]any{"approved": "manager_ok"}}),
		rule("broken", "limits", map[string]any{"input_mapping": map[string]any{"x": "price *"}}),
		rule("unmapped", "refundPolicy", map[string]any{"decision_version": 3.0, "input_mapping": map[string]any{}}),
		rule("opaque", "scripted", nil),
		rule("keyless", "", nil),
		{ID: "assign", Type: entities.UserTask, Properties: map[string]any{"assignment_decision_key": " approvalMatrix ", "assignment_decision_version": 2.0}},
		{ID: "assign-live", Type: entities.UserTask, Properties: map[string]any{"assignment_decision_key": "approvalMatrix"}},
		// A key on a step whose handler consults no decision.
		{ID: "service", Type: entities.ServiceTask, Properties: map[string]any{"decision_key": "refundPolicy", "decision_version": 3.0}},
		{ID: "manual", Type: entities.ManualTask, Properties: map[string]any{"assignment_decision_key": "approvalMatrix", "assignment_decision_version": 2.0}},
	}}
	versions := versionsOf(map[string]map[int][]string{
		"refundPolicy":   {0: {"amount"}, 3: {"amount", "approved"}},
		"limits":         {0: {"ok", "sum"}},
		"approvalMatrix": {0: {"amount"}, 2: {"approved"}},
	})
	reads := func(key string, version int) ([]string, bool, bool) {
		if key == "scripted" {
			return []string{"approved"}, false, true
		}
		return versions(key, version)
	}
	points := decisionPointsReading(def, "review", declares("approved"), nil, reads).points

	for _, id := range []string{"pinned", "pinned-int", "unmapped", "assign"} {
		if p := missesOnly(t, points, id, entities.DecisionPointDecisionTable, "approved"); !p.Analysed {
			t.Errorf("%s: %+v", id, p)
		}
	}
	if p := missesOnly(t, points, "renamed", entities.DecisionPointDecisionTable, "approved"); !p.Analysed ||
		!slices.Equal(p.Reads, []string{"approved"}) || p.ReadsInAll != 4 {
		t.Errorf("a mapping that renames: the step's value reaches the table under another name: %+v", p)
	}
	if p := missesOnly(t, points, "broken", entities.DecisionPointDecisionTable); p.Analysed {
		t.Errorf("a mapping that cannot be read: %+v", p)
	}
	if p := missesOnly(t, points, "gone", entities.DecisionPointDecisionTable); p.Analysed {
		t.Errorf("a pinned version nobody can find: %+v", p)
	}
	if p := missesOnly(t, points, "opaque", entities.DecisionPointDecisionTable, "approved"); p.Analysed {
		t.Errorf("a decision its lookup could not read: %+v", p)
	}
	if len(points) != 8 {
		t.Errorf("listed %d, want 8 — live, pinned-text, fed, keyless, assign-live, service and manual read nothing from the step: %+v",
			len(points), points)
	}
}

// tablesByKey is a decision lookup over a map of tables, composed as the one
// over the repository is: a table's own reads (logic.DecisionTableReads) and
// those of every decision it requires, each visited once.
func tablesByKey(tables map[string]entities.DecisionDefinition) decisionReads {
	return func(key string, _ int) ([]string, bool, bool) {
		if _, found := tables[key]; !found {
			return nil, false, false
		}
		names, analysable := map[string]struct{}{}, true
		visited, pending := map[string]bool{key: true}, []string{key}
		for len(pending) > 0 {
			table, found := tables[pending[0]]
			pending = pending[1:]
			own, requires, readable := logic.DecisionTableReads(table)
			analysable = analysable && readable && found
			for _, name := range own {
				names[name] = struct{}{}
			}
			for _, required := range requires {
				if !visited[required] {
					visited[required] = true
					pending = append(pending, required)
				}
			}
		}
		return sortedKeys(names), analysable, true
	}
}

// A table reads more than its columns: a cell may name any variable, and a
// decision it requires is evaluated with the same variables.
func TestACellOrARequiredDecisionThatReadsTheStepIsFound(t *testing.T) {
	t.Parallel()
	input := func(expression string) []entities.DecisionInput {
		return []entities.DecisionInput{{Expression: expression}}
	}
	tables := map[string]entities.DecisionDefinition{
		"byCell":     {Inputs: input("amount"), Rules: []entities.DecisionRule{{Inputs: []string{"> approved_limit"}}}},
		"byRequired": {Inputs: input("risk"), RequiredDecisions: []string{"risk"}},
		"risk":       {Inputs: input("score"), RequiredDecisions: []string{"byRequired", "history"}},
		"history":    {Inputs: input("approved_limit")},
		"byColumn":   {Inputs: input("amount"), Rules: []entities.DecisionRule{{Inputs: []string{"approved_limit"}}}},
		"dangling":   {Inputs: input("amount"), RequiredDecisions: []string{"nowhere"}},
	}
	node := func(id string) *entities.Node {
		return &entities.Node{ID: id, Type: entities.BusinessRuleTask, Properties: map[string]any{"decision_key": id}}
	}
	def := &entities.ProcessDefinition{Nodes: []*entities.Node{
		node("byCell"), node("byRequired"), node("byColumn"), node("dangling"),
	}}
	points := decisionPointsReading(def, "review", declares("approved_limit"), nil, tablesByKey(tables)).points

	missesOnly(t, points, "byCell", entities.DecisionPointDecisionTable, "approved_limit")
	missesOnly(t, points, "byRequired", entities.DecisionPointDecisionTable, "approved_limit")
	if _, listed := pointAt(points, "byColumn", entities.DecisionPointDecisionTable); listed {
		t.Error("a lone word in a cell is text, not the variable of that name, and the table is listed")
	}
	if p := missesOnly(t, points, "dangling", entities.DecisionPointDecisionTable); p.Analysed {
		t.Errorf("a table requiring a decision nobody can find: %+v", p)
	}
}

// A called process routes on what it is handed as surely as a gateway does,
// and it is not read here: it is listed when it is handed a field the step
// declares, as something to check, never as reading nothing.
func TestACalledProcessIsListedWhenItIsHandedWhatTheStepSets(t *testing.T) {
	t.Parallel()
	call := func(id string, mapping any) *entities.Node {
		node := &entities.Node{ID: id, Type: entities.CallActivity, Properties: map[string]any{"called_element": "settlement"}}
		if mapping != nil {
			node.Properties["in_mapping"] = mapping
		}
		return node
	}
	def := &entities.ProcessDefinition{Nodes: []*entities.Node{
		call("everything", nil),
		call("empty-mapping", map[string]any{}),
		// Not the shape the engine reads a mapping from, so it passes everything.
		call("mistyped-mapping", map[string]string{"x": "amount"}),
		call("some", map[string]any{"decision": "approved", "sum": "amount", "flag": true}),
		call("none", map[string]any{"sum": "amount", "text": "approved = true"}),
	}}
	points := decisionPointsReading(def, "review", declares("approved", "comment"), map[string]any{"comment": "waived"}, nil).points

	for _, id := range []string{"everything", "empty-mapping", "mistyped-mapping"} {
		p := missesOnly(t, points, id, entities.DecisionPointCalledProcess)
		if p.Analysed || !slices.Equal(p.Reads, []string{"approved", "comment"}) || !slices.Equal(p.Supplied, []string{"comment"}) {
			t.Errorf("%s hands over every variable: %+v", id, p)
		}
	}
	if p := missesOnly(t, points, "some", entities.DecisionPointCalledProcess); p.Analysed || !slices.Equal(p.Reads, []string{"approved"}) {
		t.Errorf("a mapping that hands over one of them: %+v", p)
	}
	if _, listed := pointAt(points, "none", entities.DecisionPointCalledProcess); listed {
		t.Error("a mapping that hands over nothing the step declares is listed")
	}
	if nothing := decisionPointsReading(def, "review", nil, nil, nil).points; len(nothing) != 0 {
		t.Errorf("a step that declares nothing hands a called process nothing: %+v", nothing)
	}
}

// The evaluator chain looks a condition's whole text up as a variable before
// it parses it (SimpleVariableEvaluator), and a form field may be named
// anything.
func TestAConditionNamingADeclaredFieldByItsWholeTextReadsIt(t *testing.T) {
	t.Parallel()
	def := &entities.ProcessDefinition{
		Nodes: []*entities.Node{
			{ID: "worded", Type: entities.ExclusiveGateway},
			{ID: "parsed", Type: entities.ExclusiveGateway},
			{ID: "catch", Type: entities.IntermediateCatchEvent, Properties: map[string]any{"condition_expression": "first name"}},
		},
		Flows: []*entities.SequenceFlow{
			flow("a", "worded", "end", "first name"),
			flow("b", "parsed", "end", "amount > 100"),
		},
	}
	points := decisionPointsReading(def, "review", declares("first name", "amount > 100"), nil, nil).points
	if p := missesOnly(t, points, "worded", entities.DecisionPointGateway, "first name"); p.Analysed {
		t.Errorf("text FEEL cannot read is still not analysable: %+v", p)
	}
	if p := missesOnly(t, points, "parsed", entities.DecisionPointGateway, "amount > 100"); !p.Analysed ||
		!slices.Equal(p.Reads, []string{"amount > 100"}) || p.ReadsInAll != 2 {
		t.Errorf("a condition that parses and is also a field's name: %+v", p)
	}
	missesOnly(t, points, "catch", entities.DecisionPointConditionalEvent, "first name")
}

// The engine takes every flow out of a step that is not an exclusive or
// inclusive gateway, whatever the flow's condition says (followOutgoingFlows,
// and the parallel and event-based gateway handlers). A condition nothing
// evaluates decides nothing; a script computes and does not route.
func TestWhatTheEngineDoesNotEvaluateIsNotADecisionPoint(t *testing.T) {
	t.Parallel()
	def := &entities.ProcessDefinition{
		Nodes: []*entities.Node{
			{ID: "task", Type: entities.UserTask},
			{ID: "fork", Type: entities.ParallelGateway},
			{ID: "race", Type: entities.EventBasedGateway},
			{ID: "script", Type: entities.ScriptTask, Script: "approved ? 1 : 2", Condition: "approved"},
			{ID: "timer", Type: entities.IntermediateCatchEvent, Condition: "approved"},
			{ID: "structured", Type: entities.IntermediateCatchEvent, Properties: map[string]any{"condition_expression": map[string]any{"body": "approved"}}},
			{ID: "bare", Type: entities.ExclusiveGateway},
		},
		Flows: []*entities.SequenceFlow{
			flow("a", "task", "end", "approved"), flow("b", "fork", "end", "approved"), flow("c", "race", "end", "approved"),
			flow("d", "nowhere", "end", "approved"), flow("e", "bare", "end", ""),
		},
	}
	if points := decisionPointsReading(def, "review", declares("approved"), nil, nil).points; len(points) != 0 {
		t.Errorf("listed %+v", points)
	}
}

// A definition is somebody's input. Nothing here follows a parent or a flow,
// so a sub-process that is its own parent, a ring of parents, a node nested in
// itself and a loop of flows are each read once.
func TestADefinitionThatLoopsOnItselfIsReadOnce(t *testing.T) {
	t.Parallel()
	nested := &entities.Node{ID: "nested", Type: entities.SubProcess}
	nested.Nodes = []*entities.Node{nested, nil, {ID: "deep", Type: entities.ExclusiveGateway, ParentID: "nested"}}
	def := &entities.ProcessDefinition{
		Nodes: []*entities.Node{
			{ID: "self", Type: entities.SubProcess, ParentID: "self"},
			{ID: "review", Type: entities.UserTask, ParentID: "self"},
			{ID: "ring-a", Type: entities.SubProcess, ParentID: "ring-b"},
			{ID: "ring-b", Type: entities.SubProcess, ParentID: "ring-a"},
			{ID: "gate", Type: entities.ExclusiveGateway, ParentID: "ring-a"},
			nested,
			nil,
			// The same id twice: the engine may run either, so both are read.
			{ID: "twice", Type: entities.IntermediateCatchEvent, Name: "Twice", Properties: map[string]any{"condition_expression": "approved"}},
			{ID: "twice", Type: entities.IntermediateCatchEvent, Properties: map[string]any{"condition_expression": "js:comment"}},
		},
		Flows: []*entities.SequenceFlow{
			flow("g1", "gate", "review", "approved"), flow("g2", "review", "gate", ""), nil,
			flow("d1", "deep", "deep", "not(approved)"),
		},
	}
	points := decisionPointsReading(def, "review", declares("approved", "comment"), nil, nil).points
	missesOnly(t, points, "gate", entities.DecisionPointGateway, "approved")
	missesOnly(t, points, "deep", entities.DecisionPointGateway, "approved")
	if p := missesOnly(t, points, "twice", entities.DecisionPointConditionalEvent, "approved"); p.Analysed || p.NodeName != "Twice" {
		t.Errorf("two nodes of one id are one point, analysable only if both are: %+v", p)
	}
	if len(points) != 3 {
		t.Errorf("%d points listed, want 3: %+v", len(points), points)
	}
	if got := decisionPointsReading(nil, "review", declares("approved"), nil, nil).points; got != nil {
		t.Errorf("no definition: %+v", got)
	}
}

// One pass over the definition, whatever its size: twenty thousand nodes in
// a ring of gateways and nested sub-processes, each gateway with a condition
// to parse.
func TestALargeDefinitionIsReadInOnePass(t *testing.T) {
	t.Parallel()
	const gateways, depth = 10_000, 10_000
	def := &entities.ProcessDefinition{}
	for i := range gateways {
		id, next := fmt.Sprintf("g%05d", i), fmt.Sprintf("g%05d", (i+1)%gateways)
		def.Nodes = append(def.Nodes, &entities.Node{ID: id, Type: entities.ExclusiveGateway, DefaultFlow: id + "-else"})
		def.Flows = append(def.Flows,
			flow(id+"-yes", id, next, fmt.Sprintf("approved and amount > %d", i)), flow(id+"-else", id, next, ""))
	}
	// Sub-processes nested one inside the next, ten thousand deep, with a
	// conditional event at the bottom.
	innermost := &entities.Node{ID: "bottom", Type: entities.IntermediateCatchEvent, Properties: map[string]any{"condition_expression": "approved"}}
	for i := range depth {
		innermost = &entities.Node{ID: fmt.Sprintf("s%05d", i), Type: entities.SubProcess, Nodes: []*entities.Node{innermost}}
	}
	def.Nodes = append(def.Nodes, innermost)

	started := time.Now()
	points := decisionPointsReading(def, "g00000", declares("approved"), map[string]any{"approved": true}, nil).points
	elapsed := time.Since(started)

	if len(points) != gateways+1 {
		t.Fatalf("%d points listed, want %d", len(points), gateways+1)
	}
	if first := points[1]; first.NodeID != "g00000" || !reflect.DeepEqual(first.Supplied, []string{"approved"}) || len(first.Missing) != 0 {
		t.Errorf("the first gateway: %+v", first)
	}
	t.Logf("%d nodes read in %s", gateways+depth+1, elapsed)
	// Generous, for a loaded machine under the race detector; a pass that
	// revisits nodes does not finish at all.
	if elapsed > 5*time.Second {
		t.Errorf("reading %d nodes took %s", gateways+depth+1, elapsed)
	}
}

// Each of these is read as the engine reads it, and each would go unnoticed
// if the reading were narrowed: the engine re-reads a condition_expression on
// any step a token rests on, takes a default flow only when the gateway has
// the flow it names, and treats a pinned version of zero or less as none.
func TestWhatTheEngineReadsIsListedAsItReadsIt(t *testing.T) {
	t.Parallel()
	rule := func(id string, version any) *entities.Node {
		return &entities.Node{ID: id, Type: entities.BusinessRuleTask, Properties: map[string]any{"decision_key": "policy", "decision_version": version}}
	}
	def := &entities.ProcessDefinition{
		Nodes: []*entities.Node{
			{ID: "task", Type: entities.UserTask, Properties: map[string]any{"condition_expression": "approved"}},
			{ID: "service", Type: entities.ServiceTask, Properties: map[string]any{"condition_expression": "approved"}},
			{ID: "borrowed", Type: entities.ExclusiveGateway, DefaultFlow: "own-else"},
			{ID: "own", Type: entities.ExclusiveGateway, DefaultFlow: "own-else"},
			{ID: "dangling", Type: entities.InclusiveGateway, DefaultFlow: "no-such-flow"},
			{ID: "nameless", Type: entities.ExclusiveGateway},
			rule("zero", 0.0), rule("negative", -1.0), rule("negative-int", -3),
			// Two gateways of one id: a default only if both name one they have.
			{ID: "twice", Type: entities.ExclusiveGateway, DefaultFlow: "twice-else"},
			{ID: "twice", Type: entities.ExclusiveGateway, DefaultFlow: "twice-else"},
			{ID: "half", Type: entities.ExclusiveGateway, DefaultFlow: "half-else"},
			{ID: "half", Type: entities.ExclusiveGateway},
			// A gateway's id on a step that is no gateway: it is the gateway
			// that decides, so the point is the gateway's.
			{ID: "mixed", Type: entities.UserTask, Name: "A task"},
			{ID: "mixed", Type: entities.ExclusiveGateway, Name: "A gateway", DefaultFlow: "mixed-else"},
		},
		Flows: []*entities.SequenceFlow{
			flow("borrowed-yes", "borrowed", "end", "approved"),
			flow("own-yes", "own", "end", "approved"), flow("own-else", "own", "end", ""),
			flow("dangling-yes", "dangling", "end", "approved"),
			flow("nameless-yes", "nameless", "end", "=done"),
			flow("twice-yes", "twice", "end", "approved"), flow("twice-else", "twice", "end", ""),
			flow("half-yes", "half", "end", "approved"), flow("half-else", "half", "end", ""),
			flow("mixed-yes", "mixed", "end", "approved"), flow("mixed-else", "mixed", "end", ""),
		},
	}
	// Only the version in force is there: a lookup by any other number finds
	// nothing, and the point would be listed as not analysed.
	reads := versionsOf(map[string]map[int][]string{"policy": {0: {"approved"}}})
	points := decisionPointsReading(def, "review", declares("approved", ""), nil, reads).points

	missesOnly(t, points, "task", entities.DecisionPointConditionalEvent, "approved")
	missesOnly(t, points, "service", entities.DecisionPointConditionalEvent, "approved")
	for id, hasDefault := range map[string]bool{"borrowed": false, "own": true, "dangling": false, "twice": true, "half": false, "mixed": true} {
		if p := missesOnly(t, points, id, entities.DecisionPointGateway, "approved"); p.HasDefaultFlow != hasDefault {
			t.Errorf("%s: has a default flow %v, want %v", id, p.HasDefaultFlow, hasDefault)
		}
	}
	for _, id := range []string{"zero", "negative", "negative-int"} {
		if p := missesOnly(t, points, id, entities.DecisionPointDecisionTable, "approved"); !p.Analysed {
			t.Errorf("%s pins no version, so the one in force is read: %+v", id, p)
		}
	}
	// A form field with no name is a name all the same, and `=done` reads it.
	missesOnly(t, points, "nameless", entities.DecisionPointGateway, "")
	if p, _ := pointAt(points, "mixed", entities.DecisionPointGateway); p.NodeName != "A gateway" {
		t.Errorf("a gateway sharing its id with a task is named %q, want the gateway's name", p.NodeName)
	}
	if len(points) != 12 {
		t.Errorf("%d points listed, want 12: %+v", len(points), points)
	}
}

// A definition's size is bounded where it is deployed, and reading it must
// cost in proportion to that size whatever is written in it. Nothing refuses a
// definition that uses one id many times over, or that has many steps reading
// the same thing, so each of these is read once: the flows of a gateway id
// once however many nodes carry the id, a decision once however many steps
// consult it, and what every call activity is handed once.
//
// Nodes of one id are one point, reading what any of them reads.
func TestADefinitionThatRepeatsItselfIsReadInProportionToItsSize(t *testing.T) {
	t.Parallel()
	const copies = 20_000
	many := func(count int, name string) []string {
		names := make([]string, count)
		for i := range names {
			names[i] = fmt.Sprintf("%s%05d", name, i)
		}
		return names
	}
	nodes := func(build func(i int) *entities.Node) []*entities.Node {
		list := make([]*entities.Node, copies)
		for i := range list {
			list[i] = build(i)
		}
		return list
	}
	lookups := 0
	policy := func(key string, _ int) ([]string, bool, bool) {
		lookups++
		if strings.HasPrefix(key, "form") {
			return many(2_000, "field"), true, true
		}
		return append(many(5_000, key+"-input"), "approved"), true, true
	}
	shapes := []struct {
		name     string
		def      *entities.ProcessDefinition
		declared map[string]struct{}
		points   int
		// reads is how many names a point reads in all; of them it lists the
		// ones the step declares.
		reads   int
		listed  int
		lookups int
		// missing is how many fields are missing over the whole definition,
		// and missingAt how many points miss one.
		missing   int
		missingAt int
	}{
		{
			name: "one gateway id on every node, with as many flows",
			def: func() *entities.ProcessDefinition {
				def := &entities.ProcessDefinition{Nodes: nodes(func(int) *entities.Node {
					return &entities.Node{ID: "g", Type: entities.ExclusiveGateway}
				})}
				for i := range copies {
					def.Flows = append(def.Flows, flow(fmt.Sprintf("f%d", i), "g", "end", "approved"))
				}
				return def
			}(),
			declared: declares("approved"), points: 1, reads: 1, listed: 1, missing: 1, missingAt: 1,
		},
		{
			name: "one event id on every node, each with a condition of its own",
			def: &entities.ProcessDefinition{Nodes: nodes(func(i int) *entities.Node {
				return &entities.Node{ID: "c", Type: entities.IntermediateCatchEvent,
					Properties: map[string]any{"condition_expression": fmt.Sprintf("approved and v%05d", i)}}
			})},
			declared: declares("approved"), points: 1, reads: copies + 1, listed: 1, missing: 1, missingAt: 1,
		},
		{
			name: "a call activity on every node, each handed every field of a long form",
			def: &entities.ProcessDefinition{Nodes: nodes(func(i int) *entities.Node {
				return &entities.Node{ID: fmt.Sprintf("call%05d", i), Type: entities.CallActivity}
			})},
			// Handed over, not known to be read: nothing is missing.
			declared: declares(many(2_000, "field")...), points: copies, reads: 2_000, listed: maxNamesShown,
		},
		{
			name: "every node consulting one large decision",
			def: &entities.ProcessDefinition{Nodes: nodes(func(i int) *entities.Node {
				return &entities.Node{ID: fmt.Sprintf("rule%05d", i), Type: entities.BusinessRuleTask, Properties: map[string]any{"decision_key": "policy"}}
			})},
			declared: declares("approved"), points: copies, reads: 5_001, listed: 1, lookups: 1, missing: 1, missingAt: copies,
		},
		{
			name: "one rule id on every node, consulting two large decisions in turn",
			def: &entities.ProcessDefinition{Nodes: nodes(func(i int) *entities.Node {
				return &entities.Node{ID: "r", Type: entities.BusinessRuleTask, Properties: map[string]any{"decision_key": fmt.Sprintf("policy%d", i%2)}}
			})},
			// Each decision's count added up: approved is read by both.
			declared: declares("approved"), points: 1, reads: 10_002, listed: 1, lookups: 2, missing: 1, missingAt: 1,
		},
		{
			name: "ten thousand ids of two nodes each, one consulting one large decision and one the other",
			def: &entities.ProcessDefinition{Nodes: nodes(func(i int) *entities.Node {
				return &entities.Node{ID: fmt.Sprintf("pair%05d", i/2), Type: entities.BusinessRuleTask, Properties: map[string]any{"decision_key": fmt.Sprintf("policy%d", i%2)}}
			})},
			declared: declares("approved"), points: copies / 2, reads: 10_002, listed: 1, lookups: 2, missing: 1, missingAt: copies / 2,
		},
		{
			// Ids times the length of the form: every one of ten thousand ids
			// is two nodes consulting two decisions, and both decisions read
			// every field of a form of two thousand.
			name: "ten thousand ids of two nodes each, consulting two decisions that both read all of a long form",
			def: &entities.ProcessDefinition{Nodes: nodes(func(i int) *entities.Node {
				return &entities.Node{ID: fmt.Sprintf("long%05d", i/2), Type: entities.BusinessRuleTask, Properties: map[string]any{"decision_key": fmt.Sprintf("form%d", i%2)}}
			})},
			declared: declares(many(2_000, "field")...), points: copies / 2, reads: 4_000, listed: maxNamesShown, lookups: 2,
			missing: 2_000, missingAt: copies / 2,
		},
		{
			name: "ten thousand ids of two nodes each, one fed through a mapping and one consulting a large decision",
			def: &entities.ProcessDefinition{Nodes: nodes(func(i int) *entities.Node {
				properties := map[string]any{"decision_key": "policy"}
				if i%2 == 1 {
					properties["input_mapping"] = map[string]any{"x": "amount"}
				}
				return &entities.Node{ID: fmt.Sprintf("fed%05d", i/2), Type: entities.BusinessRuleTask, Properties: properties}
			})},
			declared: declares("approved"), points: copies / 2, reads: 5_002, listed: 1, lookups: 1, missing: 1, missingAt: copies / 2,
		},
	}
	for _, shape := range shapes {
		lookups = 0
		started := time.Now()
		found := decisionPointsReading(shape.def, "review", shape.declared, nil, policy)
		points := found.points
		elapsed := time.Since(started)
		t.Logf("%s: %d nodes read in %s", shape.name, copies, elapsed)
		if elapsed > 5*time.Second {
			t.Errorf("%s: reading %d nodes took %s", shape.name, copies, elapsed)
		}
		if len(points) != shape.points {
			t.Errorf("%s: %d points listed, want %d", shape.name, len(points), shape.points)
			continue
		}
		for _, point := range []entities.DecisionPoint{points[0], points[len(points)-1]} {
			if point.ReadsInAll != shape.reads || len(point.Reads) != shape.listed {
				t.Errorf("%s: %s reads %d names and lists %d of them, want %d and %d",
					shape.name, point.NodeID, point.ReadsInAll, len(point.Reads), shape.reads, shape.listed)
			}
		}
		if lookups != shape.lookups {
			t.Errorf("%s: a decision was looked up %d times, want %d", shape.name, lookups, shape.lookups)
		}
		if len(found.missing) != shape.missing || found.missingAt != shape.missingAt {
			t.Errorf("%s: %d fields missing over the definition at %d points, want %d at %d",
				shape.name, len(found.missing), found.missingAt, shape.missing, shape.missingAt)
		}
		// What is held: a few names for each point, counting a list they share
		// once, and one set of what is missing no bigger than the form. In
		// proportion to the points and to the form, never to one times the
		// other.
		if held, most := namesHeldBy(points), 3*maxNamesShown*len(points); held > most {
			t.Errorf("%s: the points hold %d names between them, want no more than %d", shape.name, held, most)
		}
		if len(found.missing) > len(shape.declared) {
			t.Errorf("%s: %d names are held as missing, more than the %d fields the form has", shape.name, len(found.missing), len(shape.declared))
		}
	}
}

// namesHeldBy counts the names a set of points holds in its lists, a list
// that several points share counted once.
func namesHeldBy(points []entities.DecisionPoint) int {
	counted := map[*string]int{}
	held := 0
	for _, point := range points {
		for _, list := range [][]string{point.Reads, point.Supplied, point.Missing} {
			if len(list) == 0 || counted[&list[0]] >= len(list) {
				continue
			}
			held += len(list) - counted[&list[0]]
			counted[&list[0]] = len(list)
		}
	}
	return held
}

// A point names the first few of what it reads, supplies and misses, and
// counts the rest; the whole of what is missing is kept once, for all the
// points together. A process the instance calls is handed the step's fields
// and is not known to read them, so it adds nothing to what is missing.
func TestAPointNamesAFewAndTheScanKeepsWhatIsMissingOnce(t *testing.T) {
	t.Parallel()
	fields := make([]string, 12)
	for i := range fields {
		fields[i] = fmt.Sprintf("field%02d", i)
	}
	def := &entities.ProcessDefinition{
		Nodes: []*entities.Node{
			{ID: "all", Type: entities.ExclusiveGateway, Name: "Everything in order?"},
			{ID: "one", Type: entities.ExclusiveGateway, Name: "Approved?"},
			{ID: "given", Type: entities.ExclusiveGateway, Name: "Signed?"},
			{ID: "call", Type: entities.CallActivity, Name: "Check the supplier"},
		},
		Flows: []*entities.SequenceFlow{
			flow("a", "all", "end", strings.Join(fields, " and ")+" and somethingElse"),
			flow("b", "one", "end", "approved"),
			flow("c", "given", "end", "signed"),
		},
	}
	declared := declares(append(slices.Clone(fields), "approved", "signed")...)
	found := decisionPointsReading(def, "review", declared, map[string]any{"signed": true}, nil)

	all, _ := pointAt(found.points, "all", entities.DecisionPointGateway)
	if !slices.Equal(all.Missing, fields[:maxNamesShown]) || all.MissingInAll != 12 || !slices.Equal(all.Reads, fields[:maxNamesShown]) || all.ReadsInAll != 13 {
		t.Errorf("a point missing twelve values names %v of %d and reads %v of %d; want the first ten of twelve, and of thirteen", all.Missing, all.MissingInAll, all.Reads, all.ReadsInAll)
	}
	if given, _ := pointAt(found.points, "given", entities.DecisionPointGateway); given.MissingInAll != 0 || !slices.Equal(given.Supplied, []string{"signed"}) {
		t.Errorf("a point given its value: %+v", given)
	}
	called, listed := pointAt(found.points, "call", entities.DecisionPointCalledProcess)
	if !listed || called.MissingInAll != 0 || len(called.Missing) != 0 || len(called.Reads) != maxNamesShown || called.ReadsInAll != 14 {
		t.Errorf("the called process: %+v (listed %v); want ten of fourteen handed and nothing missing", called, listed)
	}
	if want := append([]string{"approved"}, fields...); !slices.Equal(sortedKeys(found.missing), want) {
		t.Errorf("missing over the definition: %v\nwant every field somebody reads and nobody gave: %v", sortedKeys(found.missing), want)
	}
	if found.missingAt != 2 {
		t.Errorf("%d points are missing a value, want the two gateways", found.missingAt)
	}
}

// A step's name is shown as the ledger keeps one: its first 255 characters,
// never half of one. A definition's author can call a step anything.
func TestAPointsStepIsNamedAsTheLedgerWouldKeepTheName(t *testing.T) {
	t.Parallel()
	long := strings.Repeat("é", 300)
	def := &entities.ProcessDefinition{
		Nodes: []*entities.Node{{ID: "g", Type: entities.ExclusiveGateway, Name: long}},
		Flows: []*entities.SequenceFlow{flow("a", "g", "end", "approved")},
	}
	point, _ := pointAt(decisionPointsReading(def, "review", declares("approved"), nil, nil).points, "g", entities.DecisionPointGateway)
	if point.NodeName != strings.Repeat("é", 255) {
		t.Errorf("the point is named with %d characters (%d bytes), want the first 255 whole", len([]rune(point.NodeName)), len(point.NodeName))
	}
}
