package entities

import "slices"

// NodeType represents the type of a BPMN element.
type NodeType string

const (
	StartEvent             NodeType = "startEvent"
	EndEvent               NodeType = "endEvent"
	UserTask               NodeType = "userTask"
	ServiceTask            NodeType = "serviceTask"
	ExclusiveGateway       NodeType = "exclusiveGateway"
	ParallelGateway        NodeType = "parallelGateway"
	InclusiveGateway       NodeType = "inclusiveGateway"
	ScriptTask             NodeType = "scriptTask"
	IntermediateCatchEvent NodeType = "intermediateCatchEvent"
	IntermediateThrowEvent NodeType = "intermediateThrowEvent"
	CallActivity           NodeType = "callActivity"
	ManualTask             NodeType = "manualTask"
	BusinessRuleTask       NodeType = "businessRuleTask"
	SubProcess             NodeType = "subProcess"
	BoundaryEvent          NodeType = "boundaryEvent"
	EventBasedGateway      NodeType = "eventBasedGateway"
	MessageEvent           NodeType = "messageEvent"
	SignalEvent            NodeType = "signalEvent"
	TimerEvent             NodeType = "timerEvent"
	ErrorEndEvent          NodeType = "errorEndEvent"
	TerminateEndEvent      NodeType = "terminateEndEvent"
	EscalationThrowEvent   NodeType = "escalationThrowEvent"
	CompensationThrowEvent NodeType = "compensationThrowEvent"
	Pool                   NodeType = "pool"
	Lane                   NodeType = "lane"
)

// nodeTypes is every type above, in one place, so Known and NodeTypes cannot
// disagree. TestNodeTypesAreTheDeclaredOnes holds it to the declarations.
var nodeTypes = []NodeType{
	StartEvent, EndEvent, UserTask, ServiceTask, ExclusiveGateway, ParallelGateway,
	InclusiveGateway, ScriptTask, IntermediateCatchEvent, IntermediateThrowEvent,
	CallActivity, ManualTask, BusinessRuleTask, SubProcess, BoundaryEvent,
	EventBasedGateway, MessageEvent, SignalEvent, TimerEvent, ErrorEndEvent,
	TerminateEndEvent, EscalationThrowEvent, CompensationThrowEvent, Pool, Lane,
}

// NodeTypes returns every type a definition may hold.
func NodeTypes() []NodeType {
	return slices.Clone(nodeTypes)
}

// Known reports whether t is one of the types above.
//
// A definition may hold only these. Deploy used to accept any type that was
// not empty, and a step of a type the engine has no handler for parked every
// token that reached it, with nothing to say why.
func (t NodeType) Known() bool {
	return slices.Contains(nodeTypes, t)
}
