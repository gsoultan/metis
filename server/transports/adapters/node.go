package adapters

import (
	pbentities "github.com/gsoultan/gobpm/api/proto/entities"
	"github.com/gsoultan/gobpm/server/domains/entities"
	"google.golang.org/protobuf/types/known/structpb"
)

// Nodes and flows cross the wire in both directions, and every transport used
// to spell the conversion out for itself. They disagreed: the outbound mapping
// covered eight fields and the inbound one six, so a definition saved through
// the API came back missing the settings that make its nodes do anything.
//
// Both directions live here so that a field added to the domain node is one
// edit away from working everywhere, and so the round trip is testable on its
// own.

// NodeToProto converts a domain Node to its protobuf message.
func NodeToProto(n *entities.Node) *pbentities.Node {
	if n == nil {
		return nil
	}

	var users []*pbentities.User
	if len(n.CandidateUsers) > 0 {
		users = make([]*pbentities.User, 0, len(n.CandidateUsers))
		for _, u := range n.CandidateUsers {
			if u != nil {
				users = append(users, UserPBAdapter{User: *u}.ToProto())
			}
		}
	}
	var groups []*pbentities.Group
	if len(n.CandidateGroups) > 0 {
		groups = make([]*pbentities.Group, 0, len(n.CandidateGroups))
		for _, g := range n.CandidateGroups {
			if g != nil {
				groups = append(groups, GroupPBAdapter{Group: *g}.ToProto())
			}
		}
	}

	return &pbentities.Node{
		Id:              n.ID,
		Name:            n.Name,
		Type:            string(n.Type),
		Assignee:        userRef(n.Assignee),
		Incoming:        append([]string(nil), n.Incoming...),
		Outgoing:        append([]string(nil), n.Outgoing...),
		CandidateUsers:  users,
		CandidateGroups: groups,

		Properties: propertiesToProto(n.Properties),

		Documentation: n.Documentation,
		FormKey:       n.FormKey,
		DefaultFlow:   n.DefaultFlow,
		Script:        n.Script,
		ScriptFormat:  n.ScriptFormat,
		ExternalTopic: n.ExternalTopic,
		Priority:      int32(n.Priority),
		DueDate:       n.DueDate,
		Condition:     n.Condition,

		AttachedToRef:     n.AttachedToRef,
		ParentId:          n.ParentID,
		CancelActivity:    n.CancelActivity,
		ErrorCode:         n.ErrorCode,
		IsAdHoc:           n.IsAdHoc,
		IsEventSubProcess: n.IsEventSubProcess,

		MultiInstanceType:   n.MultiInstanceType,
		LoopCardinality:     int32(n.LoopCardinality),
		Collection:          n.Collection,
		ElementVariable:     n.ElementVariable,
		CompletionCondition: n.CompletionCondition,

		X: int32(n.X),
		Y: int32(n.Y),

		Nodes: NodesToProto(n.Nodes),
		Flows: FlowsToProto(n.Flows),
	}
}

// NodeFromProto converts a protobuf Node back to the domain node.
func NodeFromProto(n *pbentities.Node) *entities.Node {
	if n == nil {
		return nil
	}

	var users []*entities.User
	for _, u := range n.GetCandidateUsers() {
		if u != nil {
			users = append(users, &entities.User{Username: u.GetUsername(), FullName: u.GetFullName()})
		}
	}
	var groups []*entities.Group
	for _, g := range n.GetCandidateGroups() {
		if g != nil {
			groups = append(groups, &entities.Group{Name: g.GetName()})
		}
	}

	return &entities.Node{
		ID:              n.GetId(),
		Name:            n.GetName(),
		Type:            entities.NodeType(n.GetType()),
		Assignee:        n.GetAssignee().GetUsername(),
		Incoming:        append([]string(nil), n.GetIncoming()...),
		Outgoing:        append([]string(nil), n.GetOutgoing()...),
		CandidateUsers:  users,
		CandidateGroups: groups,

		Properties: propertiesFromProto(n.GetProperties()),

		Documentation: n.GetDocumentation(),
		FormKey:       n.GetFormKey(),
		DefaultFlow:   n.GetDefaultFlow(),
		Script:        n.GetScript(),
		ScriptFormat:  n.GetScriptFormat(),
		ExternalTopic: n.GetExternalTopic(),
		Priority:      int(n.GetPriority()),
		DueDate:       n.GetDueDate(),
		Condition:     n.GetCondition(),

		AttachedToRef:     n.GetAttachedToRef(),
		ParentID:          n.GetParentId(),
		CancelActivity:    n.GetCancelActivity(),
		ErrorCode:         n.GetErrorCode(),
		IsAdHoc:           n.GetIsAdHoc(),
		IsEventSubProcess: n.GetIsEventSubProcess(),

		MultiInstanceType:   n.GetMultiInstanceType(),
		LoopCardinality:     int(n.GetLoopCardinality()),
		Collection:          n.GetCollection(),
		ElementVariable:     n.GetElementVariable(),
		CompletionCondition: n.GetCompletionCondition(),

		X: int(n.GetX()),
		Y: int(n.GetY()),

		Nodes: NodesFromProto(n.GetNodes()),
		Flows: FlowsFromProto(n.GetFlows()),
	}
}

// NodesToProto maps a slice of domain nodes to protobuf nodes.
func NodesToProto(nodes []*entities.Node) []*pbentities.Node {
	if len(nodes) == 0 {
		return nil
	}
	out := make([]*pbentities.Node, len(nodes))
	for i, n := range nodes {
		out[i] = NodeToProto(n)
	}
	return out
}

// NodesFromProto maps a slice of protobuf nodes back to domain nodes.
func NodesFromProto(nodes []*pbentities.Node) []*entities.Node {
	if len(nodes) == 0 {
		return nil
	}
	out := make([]*entities.Node, len(nodes))
	for i, n := range nodes {
		out[i] = NodeFromProto(n)
	}
	return out
}

// FlowToProto converts a domain sequence flow to its protobuf message.
func FlowToProto(f *entities.SequenceFlow) *pbentities.Flow {
	if f == nil {
		return nil
	}
	return &pbentities.Flow{
		Id:            f.ID,
		SourceRef:     f.SourceRef,
		TargetRef:     f.TargetRef,
		Condition:     f.Condition,
		Documentation: f.Documentation,
	}
}

// FlowFromProto converts a protobuf flow back to the domain sequence flow.
func FlowFromProto(f *pbentities.Flow) *entities.SequenceFlow {
	if f == nil {
		return nil
	}
	return &entities.SequenceFlow{
		ID:            f.GetId(),
		SourceRef:     f.GetSourceRef(),
		TargetRef:     f.GetTargetRef(),
		Condition:     f.GetCondition(),
		Documentation: f.GetDocumentation(),
	}
}

// FlowsToProto maps a slice of domain flows to protobuf flows.
func FlowsToProto(flows []*entities.SequenceFlow) []*pbentities.Flow {
	if len(flows) == 0 {
		return nil
	}
	out := make([]*pbentities.Flow, len(flows))
	for i, f := range flows {
		out[i] = FlowToProto(f)
	}
	return out
}

// FlowsFromProto maps a slice of protobuf flows back to domain flows.
func FlowsFromProto(flows []*pbentities.Flow) []*entities.SequenceFlow {
	if len(flows) == 0 {
		return nil
	}
	out := make([]*entities.SequenceFlow, len(flows))
	for i, f := range flows {
		out[i] = FlowFromProto(f)
	}
	return out
}

// propertiesToProto converts the free-form node settings to a Struct.
//
// A value that Struct cannot hold is dropped rather than failing the whole
// conversion: properties arrive as decoded JSON, so this should not happen, and
// losing one setting is a better answer to an unexpected type than refusing to
// return the definition at all.
func propertiesToProto(props map[string]any) *structpb.Struct {
	if len(props) == 0 {
		return nil
	}
	out, err := structpb.NewStruct(props)
	if err == nil {
		return out
	}
	partial := make(map[string]any, len(props))
	for k, v := range props {
		if _, convErr := structpb.NewValue(v); convErr == nil {
			partial[k] = v
		}
	}
	out, err = structpb.NewStruct(partial)
	if err != nil {
		return nil
	}
	return out
}

func propertiesFromProto(s *structpb.Struct) map[string]any {
	if s == nil || len(s.GetFields()) == 0 {
		return nil
	}
	return s.AsMap()
}
