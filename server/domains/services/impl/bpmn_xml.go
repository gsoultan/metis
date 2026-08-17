package impl

import (
	"encoding/xml"
	"errors"
	"io"

	"github.com/google/uuid"
	"github.com/gsoultan/gobpm/server/domains/entities"
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
	XMLName        xml.Name            `xml:"definitions"`
	Processes      []bpmnProcess       `xml:"process"`
	Collaborations []bpmnCollaboration `xml:"collaboration"`
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
	ID                      string             `xml:"id,attr"`
	Name                    string             `xml:"name,attr"`
	StartEvents             []bpmnNode         `xml:"startEvent"`
	EndEvents               []bpmnNode         `xml:"endEvent"`
	UserTasks               []bpmnNode         `xml:"userTask"`
	ServiceTasks            []bpmnNode         `xml:"serviceTask"`
	ScriptTasks             []bpmnNode         `xml:"scriptTask"`
	ManualTasks             []bpmnNode         `xml:"manualTask"`
	BusinessRuleTasks       []bpmnNode         `xml:"businessRuleTask"`
	ExclusiveGateways       []bpmnNode         `xml:"exclusiveGateway"`
	ParallelGateways        []bpmnNode         `xml:"parallelGateway"`
	InclusiveGateways       []bpmnNode         `xml:"inclusiveGateway"`
	EventBasedGateways      []bpmnNode         `xml:"eventBasedGateway"`
	IntermediateCatchEvents []bpmnNode         `xml:"intermediateCatchEvent"`
	IntermediateThrowEvents []bpmnNode         `xml:"intermediateThrowEvent"`
	BoundaryEvents          []bpmnNode         `xml:"boundaryEvent"`
	CallActivities          []bpmnNode         `xml:"callActivity"`
	SubProcesses            []bpmnProcessNode  `xml:"subProcess"`
	SequenceFlows           []bpmnSequenceFlow `xml:"sequenceFlow"`
	LaneSets                []bpmnLaneSet      `xml:"laneSet"`
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
	ID                       string               `xml:"id,attr"`
	Name                     string               `xml:"name,attr"`
	AttachedToRef            string               `xml:"attachedToRef,attr"`
	Incoming                 []string             `xml:"incoming"`
	Outgoing                 []string             `xml:"outgoing"`
	TerminateEventDefinition *struct{}            `xml:"terminateEventDefinition"`
	ErrorEventDefinition     *bpmnErrorEventDef   `xml:"errorEventDefinition"`
	SignalEventDefinition    *bpmnSignalEventDef  `xml:"signalEventDefinition"`
	MessageEventDefinition   *bpmnMessageEventDef `xml:"messageEventDefinition"`
	TimerEventDefinition     *bpmnTimerEventDef   `xml:"timerEventDefinition"`
	Script                   string               `xml:"script"`
	ScriptFormat             string               `xml:"scriptFormat,attr"`
	// Topic marks a service task as external work: the engine publishes it on
	// this topic and a worker pulls it, instead of the engine calling out. The
	// attribute is matched by local name, so plain topic="", camunda:topic=""
	// and zeebe-style exports all land here — an imported diagram that worked
	// as external work elsewhere keeps working here.
	Topic string `xml:"topic,attr"`
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
		ID:       n.ID,
		Name:     n.Name,
		Incoming: n.Incoming,
		Outgoing: n.Outgoing,
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

type bpmnSequenceFlow struct {
	ID        string `xml:"id,attr"`
	SourceRef string `xml:"sourceRef,attr"`
	TargetRef string `xml:"targetRef,attr"`
	// ConditionExpression holds the routing expression on gateway outgoing flows.
	ConditionExpression string `xml:"conditionExpression"`
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

	return &def, nil
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
	add(bp.IntermediateThrowEvents, entities.IntermediateThrowEvent)
	add(bp.BoundaryEvents, entities.BoundaryEvent)
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

	// Subprocesses
	for _, bsp := range bp.SubProcesses {
		node := p.mapNode(bsp.flowNode(), entities.SubProcess)
		node.Nodes = p.mapNodes(bsp.bpmnProcess)
		node.Flows = p.mapFlows(bsp.SequenceFlows)
		nodes = append(nodes, node)
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
		Properties:    make(map[string]any),
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
			Condition: bf.ConditionExpression,
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
		SequenceFlows: p.toBPMNFlows(def.Flows),
	}
	p.classifyNodes(def.Nodes, &bp)
	return bpmnDefinitions{Processes: []bpmnProcess{bp}}
}

func (p *BPMNXMLParser) toBPMNFlows(flows []*entities.SequenceFlow) []bpmnSequenceFlow {
	out := make([]bpmnSequenceFlow, len(flows))
	for i, f := range flows {
		out[i] = bpmnSequenceFlow{
			ID:                  f.ID,
			SourceRef:           f.SourceRef,
			TargetRef:           f.TargetRef,
			ConditionExpression: f.Condition,
		}
	}
	return out
}

// classifyNodes routes each entity node into the correct bpmnProcess slice.
func (p *BPMNXMLParser) classifyNodes(nodes []*entities.Node, bp *bpmnProcess) {
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
		case entities.IntermediateCatchEvent:
			bp.IntermediateCatchEvents = append(bp.IntermediateCatchEvents, bn)
		case entities.IntermediateThrowEvent:
			bp.IntermediateThrowEvents = append(bp.IntermediateThrowEvents, bn)
		case entities.BoundaryEvent:
			bp.BoundaryEvents = append(bp.BoundaryEvents, bn)
		case entities.CallActivity:
			bp.CallActivities = append(bp.CallActivities, bn)
		}
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
		Topic:         n.ExternalTopic,
	}
	if code, ok := n.Properties["error_code"].(string); ok && code != "" {
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
	return bn
}
