package impl

import (
	"fmt"
	"strings"
	"testing"

	"github.com/gsoultan/metis/server/domains/entities"
)

// camundaExport is a diagram as Camunda Modeler writes one: prefixed
// namespaces, a diagram interchange section, and an expression carrying
// xsi:type. Everything asserted below is something a real file contains and a
// user would notice the loss of.
const camundaExport = `<?xml version="1.0" encoding="UTF-8"?>
<bpmn:definitions xmlns:bpmn="http://www.omg.org/spec/BPMN/20100524/MODEL"
                  xmlns:bpmndi="http://www.omg.org/spec/BPMN/20100524/DI"
                  xmlns:dc="http://www.omg.org/spec/DD/20100524/DC"
                  xmlns:di="http://www.omg.org/spec/DD/20100524/DI"
                  xmlns:xsi="http://www.w3.org/2001/XMLSchema-instance"
                  id="Definitions_1" targetNamespace="http://bpmn.io/schema/bpmn">
  <bpmn:process id="claim" name="Claim" isExecutable="true">
    <bpmn:startEvent id="start" name="Claim filed"/>
    <bpmn:exclusiveGateway id="gw" name="Large?"/>
    <bpmn:userTask id="review" name="Review"/>
    <bpmn:endEvent id="end" name="Settled"/>
    <bpmn:sequenceFlow id="f1" sourceRef="start" targetRef="gw"/>
    <bpmn:sequenceFlow id="f2" sourceRef="gw" targetRef="review">
      <bpmn:conditionExpression xsi:type="bpmn:tFormalExpression">amount &gt; 1000</bpmn:conditionExpression>
    </bpmn:sequenceFlow>
    <bpmn:sequenceFlow id="f3" sourceRef="review" targetRef="end"/>
  </bpmn:process>
  <bpmndi:BPMNDiagram id="D1">
    <bpmndi:BPMNPlane id="P1" bpmnElement="claim">
      <bpmndi:BPMNShape id="s_start" bpmnElement="start">
        <dc:Bounds x="152" y="102" width="36" height="36"/>
      </bpmndi:BPMNShape>
      <bpmndi:BPMNShape id="s_gw" bpmnElement="gw">
        <dc:Bounds x="245.5" y="95" width="50" height="50"/>
      </bpmndi:BPMNShape>
      <bpmndi:BPMNShape id="s_review" bpmnElement="review">
        <dc:Bounds x="350" y="80" width="100" height="80"/>
      </bpmndi:BPMNShape>
      <bpmndi:BPMNShape id="s_end" bpmnElement="end">
        <dc:Bounds x="512" y="102" width="36" height="36"/>
      </bpmndi:BPMNShape>
      <bpmndi:BPMNEdge id="e_f2" bpmnElement="f2">
        <di:waypoint x="295" y="120"/>
        <di:waypoint x="320" y="120"/>
        <di:waypoint x="350" y="120"/>
      </bpmndi:BPMNEdge>
    </bpmndi:BPMNPlane>
  </bpmndi:BPMNDiagram>
</bpmn:definitions>`

func nodeByID(t *testing.T, def *entities.ProcessDefinition, id string) *entities.Node {
	t.Helper()
	if n := def.FindNode(id); n != nil {
		return n
	}
	t.Fatalf("node %q is not in the definition", id)
	return nil
}

func flowByID(t *testing.T, def *entities.ProcessDefinition, id string) *entities.SequenceFlow {
	t.Helper()
	for _, f := range def.Flows {
		if f.ID == id {
			return f
		}
	}
	t.Fatalf("flow %q is not in the definition", id)
	return nil
}

// TestImportKeepsTheDiagram asserts the geometry survives import. Before this,
// the parser read no diagram interchange at all: a diagram drawn anywhere else
// arrived with every node at the origin, so opening an imported process showed
// a single pile of shapes and the author's layout was gone for good.
func TestImportKeepsTheDiagram(t *testing.T) {
	def, err := (&BPMNXMLParser{}).Parse(strings.NewReader(camundaExport))
	if err != nil {
		t.Fatalf("Parse returned an error: %v", err)
	}

	start := nodeByID(t, def, "start")
	if start.X != 152 || start.Y != 102 || start.Width != 36 || start.Height != 36 {
		t.Errorf("start event lost its bounds: got x=%d y=%d w=%d h=%d, want 152,102,36,36",
			start.X, start.Y, start.Width, start.Height)
	}

	// Half-pixel coordinates are normal in exported diagrams and the node model
	// is integer, so this pins the rounding rather than leaving it accidental.
	if gw := nodeByID(t, def, "gw"); gw.X != 246 {
		t.Errorf("gateway x: got %d, want 246 (245.5 rounded)", gw.X)
	}

	f2 := flowByID(t, def, "f2")
	if len(f2.Waypoints) != 3 {
		t.Fatalf("flow f2 waypoints: got %d, want 3", len(f2.Waypoints))
	}
	if f2.Waypoints[1] != (entities.Waypoint{X: 320, Y: 120}) {
		t.Errorf("middle waypoint: got %+v, want {320 120}", f2.Waypoints[1])
	}
	if f2.Condition != "amount > 1000" {
		t.Errorf("condition expression: got %q, want %q", f2.Condition, "amount > 1000")
	}
}

// TestExportIsLoadableByOtherTools asserts the file declares itself.
//
// Export used to emit a bare <definitions> with no namespace and no diagram.
// That parses here, which is why the round trip looked healthy, but no other
// BPMN tool will open it: elements are resolved by namespace, and bpmn-js needs
// a diagram interchange section to render anything at all.
func TestExportIsLoadableByOtherTools(t *testing.T) {
	parser := &BPMNXMLParser{}
	def, err := parser.Parse(strings.NewReader(camundaExport))
	if err != nil {
		t.Fatalf("Parse returned an error: %v", err)
	}

	out, err := parser.Export(def)
	if err != nil {
		t.Fatalf("Export returned an error: %v", err)
	}
	got := string(out)

	for _, want := range []string{
		`xmlns="http://www.omg.org/spec/BPMN/20100524/MODEL"`,
		`xmlns:bpmndi="http://www.omg.org/spec/BPMN/20100524/DI"`,
		`xmlns:dc="http://www.omg.org/spec/DD/20100524/DC"`,
		`xmlns:di="http://www.omg.org/spec/DD/20100524/DI"`,
		`targetNamespace=`,
		`<bpmndi:BPMNDiagram`,
		`<bpmndi:BPMNPlane`,
		`<bpmndi:BPMNShape`,
		`<dc:Bounds`,
		`<bpmndi:BPMNEdge`,
		`<di:waypoint`,
		`xsi:type="bpmn:tFormalExpression"`,
	} {
		if !strings.Contains(got, want) {
			t.Errorf("exported BPMN is missing %s\n---\n%s", want, got)
		}
	}
}

// TestGeometrySurvivesTheRoundTrip is the property that makes the import path
// worth anything: open a file from another tool, save it here, open it there
// again, and it still looks like the diagram somebody drew.
func TestGeometrySurvivesTheRoundTrip(t *testing.T) {
	parser := &BPMNXMLParser{}
	first, err := parser.Parse(strings.NewReader(camundaExport))
	if err != nil {
		t.Fatalf("Parse returned an error: %v", err)
	}
	out, err := parser.Export(first)
	if err != nil {
		t.Fatalf("Export returned an error: %v", err)
	}
	again, err := parser.Parse(strings.NewReader(string(out)))
	if err != nil {
		t.Fatalf("re-parsing the exported file failed: %v", err)
	}

	for _, id := range []string{"start", "gw", "review", "end"} {
		before, after := nodeByID(t, first, id), nodeByID(t, again, id)
		if before.X != after.X || before.Y != after.Y ||
			before.Width != after.Width || before.Height != after.Height {
			t.Errorf("node %s moved: before x=%d y=%d w=%d h=%d, after x=%d y=%d w=%d h=%d",
				id, before.X, before.Y, before.Width, before.Height,
				after.X, after.Y, after.Width, after.Height)
		}
	}

	f2before, f2after := flowByID(t, first, "f2"), flowByID(t, again, "f2")
	if len(f2after.Waypoints) != len(f2before.Waypoints) {
		t.Fatalf("waypoints: before %d, after %d", len(f2before.Waypoints), len(f2after.Waypoints))
	}
	for i := range f2before.Waypoints {
		if f2before.Waypoints[i] != f2after.Waypoints[i] {
			t.Errorf("waypoint %d moved: before %+v, after %+v", i, f2before.Waypoints[i], f2after.Waypoints[i])
		}
	}

	// f1 and f3 carry no waypoints in the source file. Export has to invent a
	// route for them or the reader drops the edge entirely, which is how an
	// imported diagram loses its arrows.
	if wps := flowByID(t, again, "f1").Waypoints; len(wps) < 2 {
		t.Errorf("f1 exported without a drawable route: got %d waypoints, want at least 2", len(wps))
	}
}

// TestExportDoesNotDropSubProcesses guards silent data loss. classifyNodes had
// no case for a sub-process, so exporting a process containing one produced a
// valid file with the sub-process and every node inside it missing — and
// reported success.
func TestExportDoesNotDropSubProcesses(t *testing.T) {
	parser := &BPMNXMLParser{}
	def, err := parser.Parse(strings.NewReader(`<?xml version="1.0" encoding="UTF-8"?>
<definitions xmlns="http://www.omg.org/spec/BPMN/20100524/MODEL">
  <process id="p" name="P">
    <startEvent id="start"/>
    <subProcess id="sub" name="Review">
      <startEvent id="sub-start"/>
      <userTask id="sub-task" name="Check"/>
      <sequenceFlow id="sf" sourceRef="sub-start" targetRef="sub-task"/>
    </subProcess>
    <endEvent id="end"/>
  </process>
</definitions>`))
	if err != nil {
		t.Fatalf("Parse returned an error: %v", err)
	}

	out, err := parser.Export(def)
	if err != nil {
		t.Fatalf("Export returned an error: %v", err)
	}
	again, err := parser.Parse(strings.NewReader(string(out)))
	if err != nil {
		t.Fatalf("re-parsing the exported file failed: %v", err)
	}

	sub := nodeByID(t, again, "sub")
	if sub.Type != entities.SubProcess {
		t.Fatalf("sub is a %s, want a sub-process", sub.Type)
	}
	if len(sub.Nodes) != 2 {
		t.Errorf("sub-process children: got %d, want 2", len(sub.Nodes))
	}
	if len(sub.Flows) != 1 {
		t.Errorf("sub-process flows: got %d, want 1", len(sub.Flows))
	}
	if nodeByID(t, again, "sub-task").Name != "Check" {
		t.Error("the task inside the sub-process did not survive export")
	}
}

// TestAdHocSubProcessRoundTrips covers the element the engine already executes
// but the file format could not express. Read as an ordinary <subProcess>, an
// ad-hoc sub-process loses IsAdHoc and its completion condition, and the
// imported process looks for a start event it does not have.
func TestAdHocSubProcessRoundTrips(t *testing.T) {
	parser := &BPMNXMLParser{}
	def, err := parser.Parse(strings.NewReader(`<?xml version="1.0" encoding="UTF-8"?>
<definitions xmlns="http://www.omg.org/spec/BPMN/20100524/MODEL">
  <process id="p">
    <adHocSubProcess id="ah" name="Investigate">
      <userTask id="call" name="Call customer"/>
      <userTask id="search" name="Search records"/>
      <completionCondition>done &gt;= 2</completionCondition>
    </adHocSubProcess>
  </process>
</definitions>`))
	if err != nil {
		t.Fatalf("Parse returned an error: %v", err)
	}

	ah := nodeByID(t, def, "ah")
	if !ah.IsAdHoc {
		t.Error("imported ad-hoc sub-process is not marked ad-hoc")
	}
	if ah.CompletionCondition != "done >= 2" {
		t.Errorf("completion condition: got %q, want %q", ah.CompletionCondition, "done >= 2")
	}

	out, err := parser.Export(def)
	if err != nil {
		t.Fatalf("Export returned an error: %v", err)
	}
	if !strings.Contains(string(out), "<adHocSubProcess") {
		t.Errorf("export wrote an ordinary sub-process, losing the ad-hoc semantics\n---\n%s", out)
	}

	again, err := parser.Parse(strings.NewReader(string(out)))
	if err != nil {
		t.Fatalf("re-parsing the exported file failed: %v", err)
	}
	round := nodeByID(t, again, "ah")
	if !round.IsAdHoc || round.CompletionCondition != "done >= 2" {
		t.Errorf("ad-hoc sub-process did not survive the round trip: adHoc=%v condition=%q",
			round.IsAdHoc, round.CompletionCondition)
	}
}

// TestEscalationAndCompensationRoundTrip covers two node types the engine has
// handlers for and the file format could not carry. An exported escalation
// became a plain throw event, so a process that escalated before the round trip
// carried straight on after it.
func TestEscalationAndCompensationRoundTrip(t *testing.T) {
	parser := &BPMNXMLParser{}
	def, err := parser.Parse(strings.NewReader(`<?xml version="1.0" encoding="UTF-8"?>
<definitions xmlns="http://www.omg.org/spec/BPMN/20100524/MODEL">
  <process id="p">
    <intermediateThrowEvent id="esc" name="Escalate">
      <escalationEventDefinition escalationCode="OVERDUE"/>
    </intermediateThrowEvent>
    <intermediateThrowEvent id="comp" name="Undo">
      <compensateEventDefinition activityRef="charge"/>
    </intermediateThrowEvent>
    <endEvent id="stop"><terminateEventDefinition/></endEvent>
  </process>
</definitions>`))
	if err != nil {
		t.Fatalf("Parse returned an error: %v", err)
	}

	if got := nodeByID(t, def, "esc").Type; got != entities.EscalationThrowEvent {
		t.Errorf("escalation imported as %s, want %s", got, entities.EscalationThrowEvent)
	}
	if got := nodeByID(t, def, "comp").Type; got != entities.CompensationThrowEvent {
		t.Errorf("compensation imported as %s, want %s", got, entities.CompensationThrowEvent)
	}

	out, err := parser.Export(def)
	if err != nil {
		t.Fatalf("Export returned an error: %v", err)
	}
	again, err := parser.Parse(strings.NewReader(string(out)))
	if err != nil {
		t.Fatalf("re-parsing the exported file failed: %v", err)
	}

	esc := nodeByID(t, again, "esc")
	if esc.Type != entities.EscalationThrowEvent {
		t.Errorf("after the round trip the escalation is a %s", esc.Type)
	}
	if got := esc.GetStringProperty("escalation_code"); got != "OVERDUE" {
		t.Errorf("escalation code: got %q, want %q", got, "OVERDUE")
	}
	comp := nodeByID(t, again, "comp")
	if got := comp.GetStringProperty("activity_ref"); got != "charge" {
		t.Errorf("compensation activityRef: got %q, want %q", got, "charge")
	}
	if got := nodeByID(t, again, "stop").Type; got != entities.TerminateEndEvent {
		t.Errorf("terminate end event came back as %s — the instance would no longer be ended", got)
	}
}

// TestExportLaysOutADefinitionThatWasNeverDrawn covers definitions created over
// the API or by a test, which carry no coordinates. Exporting those with every
// shape at the origin produces a file that opens as one illegible pile, so the
// nodes are spread out instead.
func TestExportLaysOutADefinitionThatWasNeverDrawn(t *testing.T) {
	def := &entities.ProcessDefinition{
		Key:  "api-made",
		Name: "Made over the API",
		Nodes: []*entities.Node{
			{ID: "a", Type: entities.StartEvent},
			{ID: "b", Type: entities.UserTask},
			{ID: "c", Type: entities.EndEvent},
		},
		Flows: []*entities.SequenceFlow{
			{ID: "f1", SourceRef: "a", TargetRef: "b"},
			{ID: "f2", SourceRef: "b", TargetRef: "c"},
		},
	}

	out, err := (&BPMNXMLParser{}).Export(def)
	if err != nil {
		t.Fatalf("Export returned an error: %v", err)
	}
	again, err := (&BPMNXMLParser{}).Parse(strings.NewReader(string(out)))
	if err != nil {
		t.Fatalf("re-parsing the exported file failed: %v", err)
	}

	seen := map[string]bool{}
	for _, id := range []string{"a", "b", "c"} {
		n := nodeByID(t, again, id)
		if n.Width == 0 || n.Height == 0 {
			t.Errorf("node %s exported with no size: w=%d h=%d", id, n.Width, n.Height)
		}
		// fmt, not string(rune(...)): that maps anything outside the rune
		// range to the replacement character, so two different positions
		// collide and this test reports a stack that is not there.
		key := fmt.Sprintf("%d:%d", n.X, n.Y)
		if seen[key] {
			t.Errorf("node %s was stacked on top of another shape at %d,%d", id, n.X, n.Y)
		}
		seen[key] = true
	}
}

// TestExecutionAttributesSurviveTheRoundTrip covers the attributes whose loss
// changes what the process does rather than how it looks. Each of these was
// silently dropped: a gateway lost the flow it falls back to, a call activity
// lost the process it calls, a parallel multi-instance task became a
// single-run task, and a non-interrupting boundary event started cancelling
// the activity it was only meant to watch.
func TestExecutionAttributesSurviveTheRoundTrip(t *testing.T) {
	parser := &BPMNXMLParser{}
	const src = `<?xml version="1.0" encoding="UTF-8"?>
<definitions xmlns="http://www.omg.org/spec/BPMN/20100524/MODEL"
             xmlns:camunda="http://camunda.org/schema/1.0/bpmn">
  <process id="p" isExecutable="true">
    <exclusiveGateway id="gw" default="f_else"/>
    <callActivity id="call" calledElement="child-process"/>
    <serviceTask id="ship" camunda:type="external" camunda:topic="shipping"/>
    <userTask id="approve" camunda:assignee="alice" camunda:formKey="approval"
              camunda:candidateGroups="finance,ops"/>
    <serviceTask id="each">
      <multiInstanceLoopCharacteristics camunda:collection="orders" camunda:elementVariable="order">
        <loopCardinality>5</loopCardinality>
      </multiInstanceLoopCharacteristics>
    </serviceTask>
    <boundaryEvent id="watch" attachedToRef="approve" cancelActivity="false">
      <timerEventDefinition><timeDuration>PT5M</timeDuration></timerEventDefinition>
    </boundaryEvent>
    <sequenceFlow id="f_else" sourceRef="gw" targetRef="call"/>
  </process>
</definitions>`

	check := func(t *testing.T, def *entities.ProcessDefinition, stage string) {
		t.Helper()
		if got := nodeByID(t, def, "gw").DefaultFlow; got != "f_else" {
			t.Errorf("%s: gateway default flow is %q, want %q — this gateway now raises an incident instead of taking its default", stage, got, "f_else")
		}
		if got := nodeByID(t, def, "call").GetStringProperty("called_element"); got != "child-process" {
			t.Errorf("%s: calledElement is %q, want %q", stage, got, "child-process")
		}
		if got := nodeByID(t, def, "ship").ExternalTopic; got != "shipping" {
			t.Errorf("%s: external topic is %q, want %q", stage, got, "shipping")
		}
		approve := nodeByID(t, def, "approve")
		if approve.Assignee != "alice" {
			t.Errorf("%s: assignee is %q, want alice", stage, approve.Assignee)
		}
		if approve.FormKey != "approval" {
			t.Errorf("%s: form key is %q, want approval", stage, approve.FormKey)
		}
		if len(approve.CandidateGroups) != 2 {
			t.Errorf("%s: candidate groups: got %d, want 2", stage, len(approve.CandidateGroups))
		}
		each := nodeByID(t, def, "each")
		if each.MultiInstanceType != "parallel" {
			t.Errorf("%s: multi-instance type is %q, want parallel", stage, each.MultiInstanceType)
		}
		if each.LoopCardinality != 5 {
			t.Errorf("%s: loop cardinality is %d, want 5", stage, each.LoopCardinality)
		}
		if each.Collection != "orders" || each.ElementVariable != "order" {
			t.Errorf("%s: collection/elementVariable are %q/%q, want orders/order", stage, each.Collection, each.ElementVariable)
		}
		watch := nodeByID(t, def, "watch")
		if !watch.IsNonInterrupting() {
			t.Errorf("%s: boundary event became interrupting — it now cancels the task it was watching", stage)
		}
		if got := watch.GetStringProperty("timer_duration"); got != "PT5M" {
			t.Errorf("%s: timer duration is %q, want PT5M", stage, got)
		}
	}

	def, err := parser.Parse(strings.NewReader(src))
	if err != nil {
		t.Fatalf("Parse returned an error: %v", err)
	}
	check(t, def, "after import")

	out, err := parser.Export(def)
	if err != nil {
		t.Fatalf("Export returned an error: %v", err)
	}
	again, err := parser.Parse(strings.NewReader(string(out)))
	if err != nil {
		t.Fatalf("re-parsing the exported file failed: %v", err)
	}
	check(t, again, "after the round trip")

	// The external-task marker has to be Camunda's, not a bare attribute in the
	// BPMN namespace — that is what makes the exported file deployable by the
	// engine these diagrams come from, and a bare topic="" is not in the schema.
	if !strings.Contains(string(out), `camunda:topic="shipping"`) {
		t.Errorf("external topic was not written in the Camunda namespace\n---\n%s", out)
	}
}

// TestExportWritesNoIllegalAttributes guards the schema. Export wrote
// attachedToRef="", scriptFormat="", topic="" and an empty <script> onto every
// element in the file, including start events and gateways where none of those
// are legal — so the output failed validation in every tool that checks.
func TestExportWritesNoIllegalAttributes(t *testing.T) {
	parser := &BPMNXMLParser{}
	def, err := parser.Parse(strings.NewReader(camundaExport))
	if err != nil {
		t.Fatalf("Parse returned an error: %v", err)
	}
	out, err := parser.Export(def)
	if err != nil {
		t.Fatalf("Export returned an error: %v", err)
	}
	got := string(out)

	for _, unwanted := range []string{
		`attachedToRef=""`,
		`scriptFormat=""`,
		`topic=""`,
		`<script></script>`,
		`name=""`,
	} {
		if strings.Contains(got, unwanted) {
			t.Errorf("exported BPMN contains %s, which is not valid on these elements\n---\n%s", unwanted, got)
		}
	}
	if !strings.Contains(got, `isExecutable="true"`) {
		t.Error("exported process is not marked executable, so another engine will not deploy it")
	}
}

// TestConditionalEventRoundTrips covers the element that had no representation
// here at all. Read as a catch event with nothing on it, an imported
// conditional event becomes a step the process waits at for ever.
func TestConditionalEventRoundTrips(t *testing.T) {
	parser := &BPMNXMLParser{}
	def, err := parser.Parse(strings.NewReader(`<?xml version="1.0" encoding="UTF-8"?>
<definitions xmlns="http://www.omg.org/spec/BPMN/20100524/MODEL">
  <process id="p">
    <intermediateCatchEvent id="wait" name="Wait until funded">
      <conditionalEventDefinition>
        <condition>funded &gt;= 1000</condition>
      </conditionalEventDefinition>
    </intermediateCatchEvent>
  </process>
</definitions>`))
	if err != nil {
		t.Fatalf("Parse returned an error: %v", err)
	}

	wait := nodeByID(t, def, "wait")
	if got := wait.GetStringProperty("condition_expression"); got != "funded >= 1000" {
		t.Errorf("condition: got %q, want %q", got, "funded >= 1000")
	}
	if got := wait.GetStringProperty("event_type"); got != "conditional" {
		t.Errorf("event_type: got %q, want conditional", got)
	}

	out, err := parser.Export(def)
	if err != nil {
		t.Fatalf("Export returned an error: %v", err)
	}
	if !strings.Contains(string(out), "<conditionalEventDefinition>") {
		t.Errorf("export dropped the conditional event definition\n---\n%s", out)
	}

	again, err := parser.Parse(strings.NewReader(string(out)))
	if err != nil {
		t.Fatalf("re-parsing the exported file failed: %v", err)
	}
	if got := nodeByID(t, again, "wait").GetStringProperty("condition_expression"); got != "funded >= 1000" {
		t.Errorf("after the round trip the condition is %q — the process would wait here for ever", got)
	}
}
