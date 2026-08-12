package adapters

import (
	pbentities "github.com/gsoultan/gobpm/api/proto/entities"
	"github.com/gsoultan/gobpm/server/domains/entities"
)

type ProcessDefinitionPBAdapter struct {
	Definition *entities.ProcessDefinition
}

// ToProto converts the definition with the nodes and flows that make it a
// process rather than a name.
//
// Those were previously left out, so a caller reading a definition back got its
// key and version and nothing else — the designer saved a process and reopened
// it to an empty canvas, having been handed nothing to draw.
func (a ProcessDefinitionPBAdapter) ToProto() *pbentities.ProcessDefinition {
	out := a.ToProtoSummary()
	if out == nil {
		return nil
	}
	out.Nodes = NodesToProto(a.Definition.Nodes)
	out.Flows = FlowsToProto(a.Definition.Flows)
	return out
}

// ToProtoSummary converts everything except the nodes and flows, for lists.
//
// A project's definitions are listed to be chosen between, and sending every
// node of every process to draw a list of names is the kind of thing that is
// free with three processes and expensive with three hundred.
func (a ProcessDefinitionPBAdapter) ToProtoSummary() *pbentities.ProcessDefinition {
	if a.Definition == nil {
		return nil
	}
	projectID := ""
	if a.Definition.Project != nil {
		projectID = a.Definition.Project.ID.String()
	}
	return &pbentities.ProcessDefinition{
		Id:            a.Definition.ID.String(),
		Project:       projectRef(projectID),
		Key:           a.Definition.Key,
		Name:          a.Definition.Name,
		Version:       int32(a.Definition.Version),
		Documentation: a.Definition.Documentation,
	}
}
