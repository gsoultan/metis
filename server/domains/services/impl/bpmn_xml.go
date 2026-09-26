package impl

import (
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"math"
	"strconv"
	"strings"

	"github.com/google/uuid"
	"github.com/gsoultan/metis/server/domains/entities"
)

// The namespaces a BPMN file has to declare to be loadable by anything else.
//
// Export used to emit a bare <definitions> with no namespace at all. That is
// well-formed XML and this parser read it back, which is why the round trip
// looked fine — but no other tool accepts it. bpmn-js, Camunda Modeler and any
// XSD validator resolve elements by namespace, so an undeclared document is not
// a BPMN document to them, and every file this engine exported was a file only
// this engine could open.
const (
	nsBPMNModel = "http://www.omg.org/spec/BPMN/20100524/MODEL"
	nsBPMNDI    = "http://www.omg.org/spec/BPMN/20100524/DI"
	nsDC        = "http://www.omg.org/spec/DD/20100524/DC"
	nsDI        = "http://www.omg.org/spec/DD/20100524/DI"
	nsXSI       = "http://www.w3.org/2001/XMLSchema-instance"
	// nsCamunda is what Camunda 7 reads its own execution properties from. An
	// external-task topic, an assignee or a form key written as a plain
	// attribute in the BPMN namespace is not in the schema and means nothing to
	// anyone else; written here, an exported process is deployable as-is by the
	// engine most of these diagrams came from.
	nsCamunda = "http://camunda.org/schema/1.0/bpmn"

	// exportTargetNamespace is what bpmn.io stamps on its own exports. A
	// targetNamespace is required by the schema and its value is not
	// interpreted, so matching the most common producer keeps diffs small for
	// anyone who edits a file in both tools.
	exportTargetNamespace = "http://bpmn.io/schema/bpmn"

	// formalExpression is the xsi:type bpmn-moddle expects on an expression
	// element. Without it a condition is read as the abstract tExpression and
	// Camunda Modeler shows the flow as unconditional.
	formalExpression = "bpmn:tFormalExpression"
)

// ErrNoProcessInDefinition is returned for BPMN that parses cleanly but declares
// no process — most often a file exported with only a collaboration or a pool.
// The message is the one shown to whoever uploaded the file, so it names what to
// do rather than what failed.
var ErrNoProcessInDefinition = errors.New(
	"this BPMN file contains no process; add a process to the diagram, or check that the export included one")

// BPMNXMLParser handles importing/exporting BPMN 2.0 XML.
type BPMNXMLParser struct{}

type bpmnDefinitions struct {
	XMLName xml.Name `xml:"definitions"`
	ID      string   `xml:"id,attr,omitempty"`

	// The xmlns attributes are declared as literal attribute names rather than
	// through encoding/xml's namespace support, which cannot emit a prefix: a
	// struct tag carrying a namespace is re-declared as xmlns= on every single
	// element it appears on. They are write-only — a decoded document does not
	// populate them, and export sets them unconditionally.
	TargetNamespace string `xml:"targetNamespace,attr,omitempty"`
	Xmlns           string `xml:"xmlns,attr,omitempty"`
	XmlnsBPMNDI     string `xml:"xmlns:bpmndi,attr,omitempty"`
	XmlnsDC         string `xml:"xmlns:dc,attr,omitempty"`
	XmlnsDI         string `xml:"xmlns:di,attr,omitempty"`
	XmlnsXSI        string `xml:"xmlns:xsi,attr,omitempty"`
	XmlnsCamunda    string `xml:"xmlns:camunda,attr,omitempty"`

	Processes      []bpmnProcess       `xml:"process"`
	Collaborations []bpmnCollaboration `xml:"collaboration"`
	Diagrams       []bpmnDiagram       `xml:"BPMNDiagram"`
}

// --- Diagram interchange -----------------------------------------------
//
// One struct family serves both directions. Parsing matches on local name,
// because a struct tag without a namespace matches an element in any namespace
// — so <bpmndi:BPMNShape> and a prefix-less <BPMNShape> both land here.
//
// Exporting sets XMLName to the literal prefixed name. That is the only way
// encoding/xml emits a prefix, and it matters that export always overwrites it:
// a decoded element leaves the *resolved* name in XMLName, so a value that came
// from Parse would marshal back as <BPMNShape xmlns="...DI"> instead. Export
// builds these from entities, never from parsed input, which is what keeps that
// true.

type bpmnDiagram struct {
	XMLName xml.Name
	ID      string     `xml:"id,attr,omitempty"`
	Plane   *bpmnPlane `xml:"BPMNPlane"`
}

type bpmnPlane struct {
	XMLName     xml.Name
	ID          string      `xml:"id,attr,omitempty"`
	BPMNElement string      `xml:"bpmnElement,attr,omitempty"`
	Shapes      []bpmnShape `xml:"BPMNShape"`
	Edges       []bpmnEdge  `xml:"BPMNEdge"`
}

type bpmnShape struct {
	XMLName     xml.Name
	ID          string      `xml:"id,attr,omitempty"`
	BPMNElement string      `xml:"bpmnElement,attr,omitempty"`
	IsExpanded  bool        `xml:"isExpanded,attr,omitempty"`
	Bounds      *bpmnBounds `xml:"Bounds"`
}

type bpmnBounds struct {
	XMLName xml.Name
	X       float64 `xml:"x,attr"`
	Y       float64 `xml:"y,attr"`
	Width   float64 `xml:"width,attr"`
	Height  float64 `xml:"height,attr"`
}

type bpmnEdge struct {
	XMLName     xml.Name
	ID          string         `xml:"id,attr,omitempty"`
	BPMNElement string         `xml:"bpmnElement,attr,omitempty"`
	Waypoints   []bpmnWaypoint `xml:"waypoint"`
}

type bpmnWaypoint struct {
	XMLName xml.Name
	X       float64 `xml:"x,attr"`
	Y       float64 `xml:"y,attr"`
}

type bpmnCollaboration struct {
	ID           string            `xml:"id,attr"`
	Participants []bpmnParticipant `xml:"participant"`
}

type bpmnParticipant struct {
	ID         string `xml:"id,attr"`
	Name       string `xml:"name,attr"`
	ProcessRef string `xml:"processRef,attr"`
}

type bpmnProcess struct {
	ID   string `xml:"id,attr"`
	Name string `xml:"name,attr,omitempty"`
	// IsExecutable is write-only and always true. Nothing here models a
	// non-executable process, and a reader treats its absence as false —
	// which is how an exported file lands in another engine as a diagram
	// that cannot be deployed.
	IsExecutable bool `xml:"isExecutable,attr,omitempty"`
	// Documentation is declared first because BPMN puts it first in the base
	// element sequence, and encoding/xml emits children in field order.
	Documentation           string            `xml:"documentation,omitempty"`
	StartEvents             []bpmnNode        `xml:"startEvent"`
	EndEvents               []bpmnNode        `xml:"endEvent"`
	UserTasks               []bpmnNode        `xml:"userTask"`
	ServiceTasks            []bpmnNode        `xml:"serviceTask"`
	ScriptTasks             []bpmnNode        `xml:"scriptTask"`
	ManualTasks             []bpmnNode        `xml:"manualTask"`
	BusinessRuleTasks       []bpmnNode        `xml:"businessRuleTask"`
	ExclusiveGateways       []bpmnNode        `xml:"exclusiveGateway"`
	ParallelGateways        []bpmnNode        `xml:"parallelGateway"`
	InclusiveGateways       []bpmnNode        `xml:"inclusiveGateway"`
	EventBasedGateways      []bpmnNode        `xml:"eventBasedGateway"`
	IntermediateCatchEvents []bpmnNode        `xml:"intermediateCatchEvent"`
	IntermediateThrowEvents []bpmnNode        `xml:"intermediateThrowEvent"`
	BoundaryEvents          []bpmnNode        `xml:"boundaryEvent"`
	CallActivities          []bpmnNode        `xml:"callActivity"`
	SubProcesses            []bpmnProcessNode `xml:"subProcess"`
	// AdHocSubProcesses is a distinct BPMN element, not an attribute on
	// <subProcess>. Reading it as an ordinary sub-process would drop IsAdHoc and
	// the imported process would run its children in sequence from a start event
	// it does not have, instead of waiting to be driven a step at a time.
	AdHocSubProcesses []bpmnProcessNode  `xml:"adHocSubProcess"`
	SequenceFlows     []bpmnSequenceFlow `xml:"sequenceFlow"`
	LaneSets          []bpmnLaneSet      `xml:"laneSet"`
}

type bpmnLaneSet struct {
	ID    string     `xml:"id,attr"`
	Lanes []bpmnLane `xml:"lane"`
}

type bpmnLane struct {
	ID       string   `xml:"id,attr"`
	Name     string   `xml:"name,attr"`
	NodeRefs []string `xml:"flowNodeRef"`
}

type bpmnNode struct {
	ID   string `xml:"id,attr"`
	Name string `xml:"name,attr,omitempty"`
	// Every attribute below is omitempty deliberately. Export used to write
	// attachedToRef="", scriptFormat="" and topic="" onto every element
	// including start events, where attachedToRef is not a legal attribute and
	// an empty IDREF is not a legal value — so the file failed schema
	// validation everywhere it was opened.
	AttachedToRef string `xml:"attachedToRef,attr,omitempty"`
	ScriptFormat  string `xml:"scriptFormat,attr,omitempty"`
	// Default names the flow a gateway takes when nothing else matches. Losing
	// it on import is not cosmetic: this engine refuses to guess at a decision
	// point, so a gateway that had a default flow before the import raises an
	// incident after it.
	Default string `xml:"default,attr,omitempty"`
	// CalledElement is the process a call activity runs. Without it an imported
	// call activity has nothing to call and fails on first execution.
	CalledElement string `xml:"calledElement,attr,omitempty"`
	// CancelActivity is a pointer because its BPMN default is true: a boundary
	// event is interrupting unless it says otherwise, so the only value worth
	// writing is false, and a plain bool cannot tell "false" from "unset".
	CancelActivity *bool `xml:"cancelActivity,attr,omitempty"`

	// Read/write pairs for Camunda's execution attributes.
	//
	// A struct tag without a namespace matches an attribute in any namespace,
	// so the plain field reads both camunda:topic and a bare topic. A tag
	// *with* a prefix is the only way encoding/xml emits one, and it never
	// matches on the way in. So each of these is read through the plain field
	// and written through the prefixed one, and the two are never set at the
	// same time — mapNode only reads the first, toNode only sets the second.
	Topic            string `xml:"topic,attr,omitempty"`
	Assignee         string `xml:"assignee,attr,omitempty"`
	FormKey          string `xml:"formKey,attr,omitempty"`
	CandidateGroups  string `xml:"candidateGroups,attr,omitempty"`
	CamundaType      string `xml:"camunda:type,attr,omitempty"`
	CamundaTopic     string `xml:"camunda:topic,attr,omitempty"`
	CamundaAssignee  string `xml:"camunda:assignee,attr,omitempty"`
	CamundaFormKey   string `xml:"camunda:formKey,attr,omitempty"`
	CamundaCandGroup string `xml:"camunda:candidateGroups,attr,omitempty"`

	Documentation string   `xml:"documentation,omitempty"`
	Incoming      []string `xml:"incoming"`
	Outgoing      []string `xml:"outgoing"`

	TerminateEventDefinition  *struct{}               `xml:"terminateEventDefinition"`
	ErrorEventDefinition      *bpmnErrorEventDef      `xml:"errorEventDefinition"`
	SignalEventDefinition     *bpmnSignalEventDef     `xml:"signalEventDefinition"`
	MessageEventDefinition    *bpmnMessageEventDef    `xml:"messageEventDefinition"`
	TimerEventDefinition      *bpmnTimerEventDef      `xml:"timerEventDefinition"`
	EscalationEventDefinition *bpmnEscalationEventDef `xml:"escalationEventDefinition"`
	CompensateEventDefinition *bpmnCompensateEventDef `xml:"compensateEventDefinition"`
	// A conditional event waits until something about the process becomes true,
	// rather than until a clock runs out or a message arrives. It had no
	// representation here at all, so an imported one arrived as a catch event
	// with nothing configured — and a catch event with nothing configured used
	// to hold its token silently and for ever.
	ConditionalEventDefinition *bpmnConditionalEventDef `xml:"conditionalEventDefinition"`

	MultiInstance *bpmnMultiInstance `xml:"multiInstanceLoopCharacteristics"`

	// Script is omitempty because <script> is only legal inside a script task,
	// and an empty one was previously written into every element in the file.
	Script string `xml:"script,omitempty"`
}

// bpmnMultiInstance is the loop characteristics element. The engine reads
// MultiInstanceType, LoopCardinality, Collection and ElementVariable and had no
// way to express any of them in a file: a parallel multi-instance task imported
// as a single-run task, which is a process that does a fraction of its work and
// reports success.
type bpmnMultiInstance struct {
	IsSequential bool `xml:"isSequential,attr,omitempty"`
	// Read through the plain names, written through the prefixed ones — see the
	// note on bpmnNode.
	Collection          string          `xml:"collection,attr,omitempty"`
	ElementVariable     string          `xml:"elementVariable,attr,omitempty"`
	CamundaCollection   string          `xml:"camunda:collection,attr,omitempty"`
	CamundaElementVar   string          `xml:"camunda:elementVariable,attr,omitempty"`
	LoopCardinality     string          `xml:"loopCardinality,omitempty"`
	CompletionCondition *bpmnExpression `xml:"completionCondition"`
}

// bpmnProcessNode is a <subProcess>: simultaneously a flow node and a container
// of other flow nodes.
//
// It previously embedded both bpmnNode and bpmnProcess, which each declare
// `id,attr` and `name,attr`. encoding/xml drops fields that are ambiguous at
// equal depth, so every sub-process imported from BPMN XML parsed with an empty
// ID and Name — leaving its child nodes unreachable.
type bpmnProcessNode struct {
	ID       string   `xml:"id,attr"`
	Name     string   `xml:"name,attr"`
	Incoming []string `xml:"incoming"`
	Outgoing []string `xml:"outgoing"`
	// TriggeredByEvent is what makes a <subProcess> an event sub-process; it is
	// an attribute, not a separate element.
	TriggeredByEvent bool `xml:"triggeredByEvent,attr,omitempty"`
	// CompletionCondition is what releases an ad-hoc sub-process. Losing it on
	// import is the difference between a sub-process that finishes and one that
	// holds its token forever.
	CompletionCondition *bpmnExpression `xml:"completionCondition"`

	// Only bpmnProcess is embedded, for the child-element collections. bpmnNode
	// is deliberately NOT embedded: it declares `id,attr` and `name,attr` too,
	// and two sources for the same attribute make the field ambiguous —
	// encoding/xml silently drops ambiguous fields, and go vet reports the
	// duplicate tag. The flow-node attributes a <subProcess> actually carries
	// are declared explicitly above instead.
	bpmnProcess
}

// flowNode adapts the sub-process to the plain flow-node shape mapNode expects.
func (n bpmnProcessNode) flowNode() bpmnNode {
	return bpmnNode{
		ID:            n.ID,
		Name:          n.Name,
		Incoming:      n.Incoming,
		Outgoing:      n.Outgoing,
		Documentation: n.Documentation,
	}
}

type bpmnErrorEventDef struct {
	ErrorCode string `xml:"errorCode,attr"`
}

type bpmnSignalEventDef struct {
	SignalRef string `xml:"signalRef,attr"`
}

type bpmnMessageEventDef struct {
	MessageRef string `xml:"messageRef,attr"`
}

type bpmnTimerEventDef struct {
	TimeDuration string `xml:"timeDuration"`
}

type bpmnEscalationEventDef struct {
	EscalationCode string `xml:"escalationCode,attr"`
	EscalationRef  string `xml:"escalationRef,attr"`
}

type bpmnConditionalEventDef struct {
	Condition *bpmnExpression `xml:"condition"`
}

type bpmnCompensateEventDef struct {
	ActivityRef string `xml:"activityRef,attr"`
}

// bpmnExpression is an expression element: text content plus the xsi:type that
// tells a reader it is a formal expression rather than an abstract one.
//
// The Type attribute is write-only, for the same reason the xmlns attributes
// are: "xsi:type" does not match the decoded attribute name, whose namespace is
// resolved away. Nothing reads it back, and nothing needs to.
type bpmnExpression struct {
	Type  string `xml:"xsi:type,attr,omitempty"`
	Value string `xml:",chardata"`
}

// formal wraps a non-empty expression for export, or returns nil so that
// encoding/xml omits the element entirely. A struct cannot carry omitempty, so
// without this an unconditional flow exports an empty <conditionExpression/>,
// which bpmn-moddle reads as a condition that is present and never true.
func formal(value string) *bpmnExpression {
	if value == "" {
		return nil
	}
	return &bpmnExpression{Type: formalExpression, Value: value}
}

func (e *bpmnExpression) text() string {
	if e == nil {
		return ""
	}
	return e.Value
}

type bpmnSequenceFlow struct {
	ID        string `xml:"id,attr"`
	Name      string `xml:"name,attr,omitempty"`
	SourceRef string `xml:"sourceRef,attr"`
	TargetRef string `xml:"targetRef,attr"`
	// ConditionExpression holds the routing expression on gateway outgoing flows.
	ConditionExpression *bpmnExpression `xml:"conditionExpression"`
	Documentation       string          `xml:"documentation,omitempty"`
}

func (p *BPMNXMLParser) Parse(reader io.Reader) (*entities.ProcessDefinition, error) {
	var defs bpmnDefinitions
	if err := xml.NewDecoder(reader).Decode(&defs); err != nil {
		return nil, err
	}

	if len(defs.Processes) == 0 {
		// Returning (nil, nil) here broke the convention that a nil result comes
		// with an error, and every caller then had to remember a nil check the
		// signature did not ask for. It also surfaced to the user as a
		// validation failure about a definition they never wrote, rather than
		// the actual problem with the file they uploaded.
		//
		// This is reachable: a BPMN file exported with only a collaboration or
		// pool, and no process, parses perfectly well and lands here.
		return nil, ErrNoProcessInDefinition
	}

	// For now, we take the first process.
	bp := defs.Processes[0]
	id, err := uuid.NewV7()
	if err != nil {
		return nil, err
	}
	def := entities.ProcessDefinition{
		ID:   id,
		Key:  bp.ID,
		Name: bp.Name,
	}

	def.Nodes = p.mapNodes(bp)
	def.Flows = p.mapFlows(bp.SequenceFlows)

	// Add Pools/Participants as nodes if they exist
	for _, coll := range defs.Collaborations {
		for _, part := range coll.Participants {
			def.Nodes = append(def.Nodes, &entities.Node{
				ID:   part.ID,
				Name: part.Name,
				Type: entities.Pool,
				Properties: map[string]any{
					"processRef": part.ProcessRef,
				},
			})
		}
	}

	p.applyDiagram(&def, defs.Diagrams)

	return &def, nil
}

// applyDiagram copies the diagram interchange geometry onto the nodes and flows
// it refers to.
//
// The plane is flat: an expanded sub-process and the children drawn inside it
// are siblings in the same shape list, so this indexes by element id and then
// walks the whole node tree rather than trying to follow the nesting.
// A shape referring to an element that is not in the process is ignored — a
// collaboration's pool shapes arrive here for a process this import dropped.
func (p *BPMNXMLParser) applyDiagram(def *entities.ProcessDefinition, diagrams []bpmnDiagram) {
	shapes := make(map[string]bpmnShape)
	edges := make(map[string]bpmnEdge)
	for _, d := range diagrams {
		if d.Plane == nil {
			continue
		}
		for _, sh := range d.Plane.Shapes {
			if sh.BPMNElement != "" {
				shapes[sh.BPMNElement] = sh
			}
		}
		for _, e := range d.Plane.Edges {
			if e.BPMNElement != "" {
				edges[e.BPMNElement] = e
			}
		}
	}
	if len(shapes) == 0 && len(edges) == 0 {
		return
	}

	var walk func(nodes []*entities.Node)
	walk = func(nodes []*entities.Node) {
		for _, n := range nodes {
			if sh, ok := shapes[n.ID]; ok {
				if sh.Bounds != nil {
					n.X = round(sh.Bounds.X)
					n.Y = round(sh.Bounds.Y)
					n.Width = round(sh.Bounds.Width)
					n.Height = round(sh.Bounds.Height)
				}
				n.IsExpanded = sh.IsExpanded
			}
			applyEdges(n.Flows, edges)
			walk(n.Nodes)
		}
	}
	walk(def.Nodes)
	applyEdges(def.Flows, edges)
}

func applyEdges(flows []*entities.SequenceFlow, edges map[string]bpmnEdge) {
	for _, f := range flows {
		e, ok := edges[f.ID]
		if !ok {
			continue
		}
		f.Waypoints = make([]entities.Waypoint, 0, len(e.Waypoints))
		for _, wp := range e.Waypoints {
			f.Waypoints = append(f.Waypoints, entities.Waypoint{X: round(wp.X), Y: round(wp.Y)})
		}
	}
}

// round converts a BPMN coordinate to the integer the node entity stores.
// Diagrams commonly carry halves (x="152.5"); the node model is pixel-integer
// because that is what the designer works in, so a sub-pixel offset is lost on
// import and does not come back on export. Nothing renders differently.
func round(v float64) int { return int(math.Round(v)) }

// splitList reads a comma-separated attribute into the names it holds, dropping
// the empty entries a trailing comma leaves behind.
func splitList(v string) []string {
	var out []string
	for _, part := range strings.Split(v, ",") {
		if part = strings.TrimSpace(part); part != "" {
			out = append(out, part)
		}
	}
	return out
}

// groupNames renders candidate groups back into the comma-separated attribute
// they came from. A nil group in the slice is skipped rather than dereferenced:
// the list is decoded from a JSON column and nothing guarantees its shape.
func groupNames(groups []*entities.Group) string {
	names := make([]string, 0, len(groups))
	for _, g := range groups {
		if g != nil && g.Name != "" {
			names = append(names, g.Name)
		}
	}
	return strings.Join(names, ",")
}

func (p *BPMNXMLParser) mapNodes(bp bpmnProcess) []*entities.Node {
	var nodes []*entities.Node

	// Helper to add nodes
	add := func(bpNodes []bpmnNode, nodeType entities.NodeType) {
		for _, bn := range bpNodes {
			nodes = append(nodes, p.mapNode(bn, nodeType))
		}
	}

	add(bp.StartEvents, entities.StartEvent)
	add(bp.UserTasks, entities.UserTask)
	add(bp.ServiceTasks, entities.ServiceTask)
	add(bp.ScriptTasks, entities.ScriptTask)
	add(bp.ManualTasks, entities.ManualTask)
	add(bp.BusinessRuleTasks, entities.BusinessRuleTask)
	add(bp.ExclusiveGateways, entities.ExclusiveGateway)
	add(bp.ParallelGateways, entities.ParallelGateway)
	add(bp.InclusiveGateways, entities.InclusiveGateway)
	add(bp.EventBasedGateways, entities.EventBasedGateway)
	add(bp.IntermediateCatchEvents, entities.IntermediateCatchEvent)

	add(bp.BoundaryEvents, entities.BoundaryEvent)

	for _, bn := range bp.IntermediateThrowEvents {
		nodes = append(nodes, p.mapNode(bn, throwEventType(bn)))
	}
	add(bp.CallActivities, entities.CallActivity)

	// Special handling for EndEvents to detect Terminate/Error
	for _, bn := range bp.EndEvents {
		nt := entities.EndEvent
		if bn.TerminateEventDefinition != nil {
			nt = entities.TerminateEndEvent
		} else if bn.ErrorEventDefinition != nil {
			nt = entities.ErrorEndEvent
		}
		nodes = append(nodes, p.mapNode(bn, nt))
	}

	// Sub-processes, ordinary and ad-hoc. Both are containers; the ad-hoc one
	// additionally carries the condition that releases it.
	for _, bsp := range bp.SubProcesses {
		nodes = append(nodes, p.mapSubProcess(bsp, false))
	}
	for _, bsp := range bp.AdHocSubProcesses {
		nodes = append(nodes, p.mapSubProcess(bsp, true))
	}

	// Lanes
	for _, ls := range bp.LaneSets {
		for _, lane := range ls.Lanes {
			nodes = append(nodes, &entities.Node{
				ID:   lane.ID,
				Name: lane.Name,
				Type: entities.Lane,
				Properties: map[string]any{
					"nodeRefs": lane.NodeRefs,
				},
			})
		}
	}

	return nodes
}

func (p *BPMNXMLParser) mapSubProcess(bsp bpmnProcessNode, adHoc bool) *entities.Node {
	node := p.mapNode(bsp.flowNode(), entities.SubProcess)
	node.IsAdHoc = adHoc
	node.IsEventSubProcess = bsp.TriggeredByEvent
	node.CompletionCondition = bsp.CompletionCondition.text()
	node.Nodes = p.mapNodes(bsp.bpmnProcess)
	node.Flows = p.mapFlows(bsp.SequenceFlows)
	return node
}

// throwEventType picks the node type for an <intermediateThrowEvent> from the
// event definition inside it.
//
// Signal and message deliberately stay IntermediateThrowEvent: that handler
// reads signal_name and message_name and does both, so it is a superset of the
// dedicated throw handlers and routing them separately would change nothing.
// Escalation and compensation are not — they take different properties and a
// different engine call — so leaving them as a plain throw event meant an
// imported diagram silently skipped the escalation it was drawn to raise.
func throwEventType(bn bpmnNode) entities.NodeType {
	switch {
	case bn.EscalationEventDefinition != nil:
		return entities.EscalationThrowEvent
	case bn.CompensateEventDefinition != nil:
		return entities.CompensationThrowEvent
	default:
		return entities.IntermediateThrowEvent
	}
}

func (p *BPMNXMLParser) mapNode(bn bpmnNode, nodeType entities.NodeType) *entities.Node {
	node := &entities.Node{
		ID:            bn.ID,
		Name:          bn.Name,
		Type:          nodeType,
		AttachedToRef: bn.AttachedToRef,
		Incoming:      bn.Incoming,
		Outgoing:      bn.Outgoing,
		Script:        bn.Script,
		ScriptFormat:  bn.ScriptFormat,
		ExternalTopic: bn.Topic,
		Documentation: bn.Documentation,
		DefaultFlow:   bn.Default,
		Assignee:      bn.Assignee,
		FormKey:       bn.FormKey,
		Properties:    make(map[string]any),
	}

	// A bare group name is what the assignment path itself constructs when it
	// resolves a candidate group from a definition, so an imported diagram
	// arrives in the same shape a designed one does.
	for _, name := range splitList(bn.CandidateGroups) {
		node.CandidateGroups = append(node.CandidateGroups, &entities.Group{Name: name})
	}

	if bn.CalledElement != "" {
		node.Properties["called_element"] = bn.CalledElement
	}

	// cancelActivity="false" is the only value that carries information: BPMN
	// defaults a boundary event to interrupting. It is recorded as the
	// non_interrupting property because that is the one the engine reads —
	// Node.CancelActivity is written false unconditionally by the designer, so
	// honouring it would make every boundary event non-interrupting.
	if bn.CancelActivity != nil {
		node.CancelActivity = *bn.CancelActivity
		if !*bn.CancelActivity {
			node.Properties["non_interrupting"] = true
		}
	}

	if mi := bn.MultiInstance; mi != nil {
		node.MultiInstanceType = "parallel"
		if mi.IsSequential {
			node.MultiInstanceType = "sequential"
		}
		if n, err := strconv.Atoi(strings.TrimSpace(mi.LoopCardinality)); err == nil {
			node.LoopCardinality = n
		}
		node.Collection = mi.Collection
		node.ElementVariable = mi.ElementVariable
		// The engine does not evaluate a multi-instance completion condition —
		// Node.CompletionCondition is read only by the ad-hoc sub-process — so
		// this is kept as a property rather than assigned to that field. It
		// survives the round trip instead of being silently dropped, without
		// claiming a behaviour this engine does not have.
		if c := mi.CompletionCondition.text(); c != "" {
			node.Properties["multi_instance_completion_condition"] = c
		}
	}

	if bn.ErrorEventDefinition != nil {
		node.Properties["error_code"] = bn.ErrorEventDefinition.ErrorCode
	}
	if bn.SignalEventDefinition != nil {
		node.Properties["signal_name"] = bn.SignalEventDefinition.SignalRef
	}
	if bn.MessageEventDefinition != nil {
		node.Properties["message_name"] = bn.MessageEventDefinition.MessageRef
	}
	if bn.TimerEventDefinition != nil {
		node.Properties["timer_duration"] = bn.TimerEventDefinition.TimeDuration
	}
	if bn.EscalationEventDefinition != nil {
		code := bn.EscalationEventDefinition.EscalationCode
		if code == "" {
			code = bn.EscalationEventDefinition.EscalationRef
		}
		node.Properties["escalation_code"] = code
		node.Properties["event_type"] = "escalation"
	}
	if bn.ConditionalEventDefinition != nil {
		node.Properties["condition_expression"] = bn.ConditionalEventDefinition.Condition.text()
		node.Properties["event_type"] = "conditional"
	}
	if bn.CompensateEventDefinition != nil {
		node.Properties["activity_ref"] = bn.CompensateEventDefinition.ActivityRef
		node.Properties["compensation"] = "true"
		node.Properties["event_type"] = "compensation"
	}

	// ErrorCode is a field as well as a property, and the two matching paths
	// read different ones — see Node.ErrorCodeValue. An import that filled only
	// the property worked in-process and silently did nothing in the job worker.
	if bn.ErrorEventDefinition != nil {
		node.ErrorCode = bn.ErrorEventDefinition.ErrorCode
	}

	return node
}

func (p *BPMNXMLParser) mapFlows(bpFlows []bpmnSequenceFlow) []*entities.SequenceFlow {
	flows := make([]*entities.SequenceFlow, len(bpFlows))
	for i, bf := range bpFlows {
		flows[i] = &entities.SequenceFlow{
			ID:        bf.ID,
			SourceRef: bf.SourceRef,
			TargetRef: bf.TargetRef,
			// Preserve the condition expression from the BPMN XML so gateway
			// routing works correctly after import.
			Condition:     bf.ConditionExpression.text(),
			Documentation: bf.Documentation,
		}
	}
	return flows
}

// Export serialises a ProcessDefinition back to a BPMN 2.0 XML document.
// It converts the internal entity model to the bpmnDefinitions wire format so
// that the output is a valid BPMN file rather than a raw JSON-tagged struct dump.
func (p *BPMNXMLParser) Export(def *entities.ProcessDefinition) ([]byte, error) {
	bpmnDefs := p.toBPMNDefinitions(def)
	data, err := xml.MarshalIndent(bpmnDefs, "", "  ")
	if err != nil {
		return nil, err
	}
	return append([]byte(xml.Header), data...), nil
}

func (p *BPMNXMLParser) toBPMNDefinitions(def *entities.ProcessDefinition) bpmnDefinitions {
	bp := bpmnProcess{
		ID:            def.Key,
		Name:          def.Name,
		Documentation: def.Documentation,
		IsExecutable:  true,
		SequenceFlows: p.toBPMNFlows(def.Flows),
	}
	var participants []bpmnParticipant
	p.classifyNodes(def.Nodes, &bp, &participants)

	out := bpmnDefinitions{
		ID:              "Definitions_" + def.Key,
		TargetNamespace: exportTargetNamespace,
		Xmlns:           nsBPMNModel,
		XmlnsBPMNDI:     nsBPMNDI,
		XmlnsDC:         nsDC,
		XmlnsDI:         nsDI,
		XmlnsXSI:        nsXSI,
		XmlnsCamunda:    nsCamunda,
		Processes:       []bpmnProcess{bp},
	}

	planeElement := def.Key
	if len(participants) > 0 {
		collabID := "Collaboration_" + def.Key
		out.Collaborations = []bpmnCollaboration{{ID: collabID, Participants: participants}}
		// The plane refers to whatever is at the top of the diagram. With pools
		// present that is the collaboration, not the process inside it; pointing
		// it at the process leaves the pool shapes orphaned and bpmn-js drops
		// them.
		planeElement = collabID
	}

	out.Diagrams = []bpmnDiagram{p.toDiagram(def, planeElement)}
	return out
}

// rect is a resolved shape: what the element is actually drawn as, after
// defaults and fallbacks have been applied.
type rect struct{ X, Y, W, H int }

// defaultSize is the size BPMN tools draw an element at when the file does not
// say. A definition authored in the designer has a position but no size — the
// designer's nodes are sized by CSS — so without this every shape would export
// as a zero-by-zero box and the diagram would open empty.
func defaultSize(t entities.NodeType) (int, int) {
	switch t {
	case entities.StartEvent, entities.EndEvent, entities.TerminateEndEvent,
		entities.ErrorEndEvent, entities.IntermediateCatchEvent,
		entities.IntermediateThrowEvent, entities.BoundaryEvent,
		entities.TimerEvent, entities.SignalEvent, entities.MessageEvent,
		entities.EscalationThrowEvent, entities.CompensationThrowEvent:
		return 36, 36
	case entities.ExclusiveGateway, entities.ParallelGateway,
		entities.InclusiveGateway, entities.EventBasedGateway:
		return 50, 50
	case entities.SubProcess:
		return 350, 200
	case entities.Pool:
		return 600, 250
	case entities.Lane:
		return 570, 125
	default:
		return 100, 80
	}
}

// flatten returns every node in the definition, parents before children, in
// document order. The diagram plane is flat even where the model is not.
func flatten(nodes []*entities.Node, out *[]*entities.Node) {
	for _, n := range nodes {
		*out = append(*out, n)
		flatten(n.Nodes, out)
	}
}

// flattenFlows returns every sequence flow, including those inside sub-processes.
func flattenFlows(def *entities.ProcessDefinition) []*entities.SequenceFlow {
	out := append([]*entities.SequenceFlow{}, def.Flows...)
	var walk func(nodes []*entities.Node)
	walk = func(nodes []*entities.Node) {
		for _, n := range nodes {
			out = append(out, n.Flows...)
			walk(n.Nodes)
		}
	}
	walk(def.Nodes)
	return out
}

// layout resolves every node to the box it will be drawn in.
//
// A definition that has never been through a diagram — one created over the
// API, or by a test — has no coordinates at all, and exporting it with every
// shape at the origin produces a file that opens as a single illegible pile.
// When nothing is positioned, and only then, the nodes are laid out in one row
// so the file is at least readable; a definition where anything carries a
// position is taken at its word, including a node genuinely at the origin.
func layout(def *entities.ProcessDefinition) map[string]rect {
	var all []*entities.Node
	flatten(def.Nodes, &all)

	positioned := false
	for _, n := range all {
		if n.X != 0 || n.Y != 0 {
			positioned = true
			break
		}
	}

	boxes := make(map[string]rect, len(all))
	for i, n := range all {
		w, h := n.Width, n.Height
		dw, dh := defaultSize(n.Type)
		if w <= 0 {
			w = dw
		}
		if h <= 0 {
			h = dh
		}
		x, y := n.X, n.Y
		if !positioned {
			x, y = 160+i*180, 100
		}
		boxes[n.ID] = rect{X: x, Y: y, W: w, H: h}
	}
	return boxes
}

// toDiagram builds the diagram interchange section: one shape per node, one
// edge per sequence flow.
func (p *BPMNXMLParser) toDiagram(def *entities.ProcessDefinition, planeElement string) bpmnDiagram {
	boxes := layout(def)

	var all []*entities.Node
	flatten(def.Nodes, &all)

	plane := &bpmnPlane{
		XMLName:     xml.Name{Local: "bpmndi:BPMNPlane"},
		ID:          "BPMNPlane_" + def.Key,
		BPMNElement: planeElement,
	}

	for _, n := range all {
		b := boxes[n.ID]
		plane.Shapes = append(plane.Shapes, bpmnShape{
			XMLName:     xml.Name{Local: "bpmndi:BPMNShape"},
			ID:          fmt.Sprintf("Shape_%s", n.ID),
			BPMNElement: n.ID,
			// A sub-process whose children are drawn in this same plane has to
			// say it is expanded, or a reader collapses it to a task-sized box
			// and its children render on top of the diagram.
			IsExpanded: n.IsExpanded || (n.Type == entities.SubProcess && len(n.Nodes) > 0),
			Bounds: &bpmnBounds{
				XMLName: xml.Name{Local: "dc:Bounds"},
				X:       float64(b.X), Y: float64(b.Y),
				Width: float64(b.W), Height: float64(b.H),
			},
		})
	}

	for _, f := range flattenFlows(def) {
		wps := f.Waypoints
		if len(wps) < 2 {
			// BPMN requires at least two waypoints on an edge. A flow drawn in
			// the designer has none — the designer routes edges itself — so a
			// straight line from the source's right edge to the target's left
			// is synthesised. Without it the edge is dropped by the reader and
			// the diagram opens as disconnected nodes.
			src, okS := boxes[f.SourceRef]
			dst, okT := boxes[f.TargetRef]
			if !okS || !okT {
				continue
			}
			wps = []entities.Waypoint{
				{X: src.X + src.W, Y: src.Y + src.H/2},
				{X: dst.X, Y: dst.Y + dst.H/2},
			}
		}
		edge := bpmnEdge{
			XMLName:     xml.Name{Local: "bpmndi:BPMNEdge"},
			ID:          fmt.Sprintf("Edge_%s", f.ID),
			BPMNElement: f.ID,
		}
		for _, wp := range wps {
			edge.Waypoints = append(edge.Waypoints, bpmnWaypoint{
				XMLName: xml.Name{Local: "di:waypoint"},
				X:       float64(wp.X), Y: float64(wp.Y),
			})
		}
		plane.Edges = append(plane.Edges, edge)
	}

	return bpmnDiagram{
		XMLName: xml.Name{Local: "bpmndi:BPMNDiagram"},
		ID:      "BPMNDiagram_" + def.Key,
		Plane:   plane,
	}
}

func (p *BPMNXMLParser) toBPMNFlows(flows []*entities.SequenceFlow) []bpmnSequenceFlow {
	out := make([]bpmnSequenceFlow, len(flows))
	for i, f := range flows {
		out[i] = bpmnSequenceFlow{
			ID:                  f.ID,
			SourceRef:           f.SourceRef,
			TargetRef:           f.TargetRef,
			ConditionExpression: formal(f.Condition),
			Documentation:       f.Documentation,
		}
	}
	return out
}

// classifyNodes routes each entity node into the correct bpmnProcess slice.
//
// Every node type the engine can execute has to appear here. A type with no
// case is not a warning and not an error — it is simply absent from the output,
// and export is the path behind "download this process". Sub-processes were
// missing, so exporting a process that had one dropped the sub-process and
// every node inside it and still reported success.
func (p *BPMNXMLParser) classifyNodes(nodes []*entities.Node, bp *bpmnProcess, participants *[]bpmnParticipant) {
	var lanes []bpmnLane

	for _, n := range nodes {
		bn := p.toNode(n)
		switch n.Type {
		case entities.StartEvent:
			bp.StartEvents = append(bp.StartEvents, bn)
		case entities.EndEvent, entities.TerminateEndEvent, entities.ErrorEndEvent:
			bp.EndEvents = append(bp.EndEvents, bn)
		case entities.UserTask:
			bp.UserTasks = append(bp.UserTasks, bn)
		case entities.ServiceTask:
			bp.ServiceTasks = append(bp.ServiceTasks, bn)
		case entities.ScriptTask:
			bp.ScriptTasks = append(bp.ScriptTasks, bn)
		case entities.ManualTask:
			bp.ManualTasks = append(bp.ManualTasks, bn)
		case entities.BusinessRuleTask:
			bp.BusinessRuleTasks = append(bp.BusinessRuleTasks, bn)
		case entities.ExclusiveGateway:
			bp.ExclusiveGateways = append(bp.ExclusiveGateways, bn)
		case entities.ParallelGateway:
			bp.ParallelGateways = append(bp.ParallelGateways, bn)
		case entities.InclusiveGateway:
			bp.InclusiveGateways = append(bp.InclusiveGateways, bn)
		case entities.EventBasedGateway:
			bp.EventBasedGateways = append(bp.EventBasedGateways, bn)
		case entities.IntermediateCatchEvent, entities.TimerEvent:
			// TimerEvent is the designer's name for an intermediate catch event
			// carrying a timer, and the handler factory maps the two to the same
			// handler. BPMN has one element for both.
			bp.IntermediateCatchEvents = append(bp.IntermediateCatchEvents, bn)
		case entities.IntermediateThrowEvent, entities.SignalEvent, entities.MessageEvent,
			entities.EscalationThrowEvent, entities.CompensationThrowEvent:
			// Signal and message throws round-trip back as IntermediateThrowEvent
			// rather than their own types. That is a normalisation, not a loss:
			// IntermediateThrowEventHandler reads signal_name and message_name
			// and does exactly what the dedicated handlers do.
			bp.IntermediateThrowEvents = append(bp.IntermediateThrowEvents, bn)
		case entities.BoundaryEvent:
			bp.BoundaryEvents = append(bp.BoundaryEvents, bn)
		case entities.CallActivity:
			bp.CallActivities = append(bp.CallActivities, bn)
		case entities.SubProcess:
			bsp := bpmnProcessNode{
				ID:               n.ID,
				Name:             n.Name,
				Incoming:         n.Incoming,
				Outgoing:         n.Outgoing,
				TriggeredByEvent: n.IsEventSubProcess,
			}
			bsp.Documentation = n.Documentation
			bsp.SequenceFlows = p.toBPMNFlows(n.Flows)
			// Children of a sub-process are classified into the sub-process, and
			// a pool nested inside one is not a thing BPMN can express, so the
			// participant list is not threaded further down.
			p.classifyNodes(n.Nodes, &bsp.bpmnProcess, nil)
			if n.IsAdHoc {
				bsp.CompletionCondition = formal(n.CompletionCondition)
				bp.AdHocSubProcesses = append(bp.AdHocSubProcesses, bsp)
			} else {
				bp.SubProcesses = append(bp.SubProcesses, bsp)
			}
		case entities.Pool:
			if participants == nil {
				continue
			}
			// processRef came out of a JSON column, so it is whatever was stored
			// rather than necessarily a string. Defaulting to this process is
			// right for the single-process export this produces.
			ref, ok := n.Properties["processRef"].(string)
			if !ok || ref == "" {
				ref = bp.ID
			}
			*participants = append(*participants, bpmnParticipant{ID: n.ID, Name: n.Name, ProcessRef: ref})
		case entities.Lane:
			lanes = append(lanes, bpmnLane{ID: n.ID, Name: n.Name, NodeRefs: stringSlice(n.Properties["nodeRefs"])})
		}
	}

	if len(lanes) > 0 {
		bp.LaneSets = append(bp.LaneSets, bpmnLaneSet{ID: "LaneSet_" + bp.ID, Lanes: lanes})
	}
}

// stringSlice reads a list of ids out of a node property.
//
// The property is written as []string and read back from a JSON column as
// []any, so a bare type assertion to []string succeeds on a freshly parsed
// definition and silently yields nothing on a stored one — which is the only
// case that matters, because export reads stored definitions.
func stringSlice(v any) []string {
	switch vs := v.(type) {
	case []string:
		return vs
	case []any:
		out := make([]string, 0, len(vs))
		for _, e := range vs {
			if s, ok := e.(string); ok {
				out = append(out, s)
			}
		}
		return out
	default:
		return nil
	}
}

func (p *BPMNXMLParser) toNode(n *entities.Node) bpmnNode {
	bn := bpmnNode{
		ID:            n.ID,
		Name:          n.Name,
		AttachedToRef: n.AttachedToRef,
		Incoming:      n.Incoming,
		Outgoing:      n.Outgoing,
		Script:        n.Script,
		ScriptFormat:  n.ScriptFormat,
		Documentation: n.Documentation,
		Default:       n.DefaultFlow,
	}

	// The Camunda-namespaced attributes are the write side of the read/write
	// pairs on bpmnNode; the plain fields stay empty so nothing is emitted
	// twice, once legally and once not.
	if topic := n.WorkerTopic(); topic != "" {
		bn.CamundaType = "external"
		bn.CamundaTopic = topic
	}
	bn.CamundaAssignee = n.Assignee
	bn.CamundaFormKey = n.FormKey
	if names := groupNames(n.CandidateGroups); names != "" {
		bn.CamundaCandGroup = names
	}

	if called := n.GetStringProperty("called_element"); called != "" {
		bn.CalledElement = called
	} else if called := n.GetStringProperty("called_process_key"); called != "" {
		// The property panel writes called_process_key; BPMN has one spelling.
		bn.CalledElement = called
	}

	if n.Type == entities.BoundaryEvent && n.IsNonInterrupting() {
		interrupting := false
		bn.CancelActivity = &interrupting
	}

	if n.MultiInstanceType != "" && n.MultiInstanceType != "none" {
		mi := &bpmnMultiInstance{
			IsSequential:      n.MultiInstanceType == "sequential",
			CamundaCollection: n.Collection,
			CamundaElementVar: n.ElementVariable,
		}
		if n.LoopCardinality > 0 {
			mi.LoopCardinality = strconv.Itoa(n.LoopCardinality)
		}
		mi.CompletionCondition = formal(n.GetStringProperty("multi_instance_completion_condition"))
		bn.MultiInstance = mi
	}
	if n.Type == entities.TerminateEndEvent {
		// Without the event definition a terminate end event exports as a plain
		// <endEvent>, and re-importing it gives a process that finishes one
		// token instead of ending the instance.
		bn.TerminateEventDefinition = &struct{}{}
	}
	if code := n.ErrorCodeValue(); code != "" {
		bn.ErrorEventDefinition = &bpmnErrorEventDef{ErrorCode: code}
	}
	if sig, ok := n.Properties["signal_name"].(string); ok && sig != "" {
		bn.SignalEventDefinition = &bpmnSignalEventDef{SignalRef: sig}
	}
	if msg, ok := n.Properties["message_name"].(string); ok && msg != "" {
		bn.MessageEventDefinition = &bpmnMessageEventDef{MessageRef: msg}
	}
	if dur, ok := n.Properties["timer_duration"].(string); ok && dur != "" {
		bn.TimerEventDefinition = &bpmnTimerEventDef{TimeDuration: dur}
	}
	if n.Type == entities.EscalationThrowEvent {
		bn.EscalationEventDefinition = &bpmnEscalationEventDef{
			EscalationCode: n.GetStringProperty("escalation_code"),
		}
	}
	if expr := n.GetStringProperty("condition_expression"); expr != "" {
		bn.ConditionalEventDefinition = &bpmnConditionalEventDef{Condition: formal(expr)}
	}
	if n.Type == entities.CompensationThrowEvent {
		bn.CompensateEventDefinition = &bpmnCompensateEventDef{
			ActivityRef: n.GetStringProperty("activity_ref"),
		}
	}
	return bn
}
