package validation

import (
	"fmt"

	"github.com/gsoultan/metis/server/domains/entities"
)

// Visitor implements the entities.DefinitionVisitor interface to validate BPMN definitions.
type Visitor struct {
	errors []string
}

func NewVisitor() *Visitor {
	return &Visitor{
		errors: make([]string, 0),
	}
}

func (v *Visitor) VisitDefinition(pd *entities.ProcessDefinition) {
	// A request whose body omits or misspells "definition" decodes to a nil
	// pointer, so this is reachable from outside rather than only from a
	// programming error. Report it as invalid input; dereferencing it here
	// takes the request down without ever reaching the logging interceptor.
	if pd == nil {
		v.errors = append(v.errors, "No process definition was supplied")
		return
	}
	if pd.Key == "" {
		v.errors = append(v.errors, "Process definition key is missing")
	}
	if len(pd.Nodes) == 0 {
		v.errors = append(v.errors, "Process definition has no nodes")
	}
}

func (v *Visitor) VisitFlowNode(n *entities.Node) {
	if n.ID == "" {
		v.errors = append(v.errors, "Flow node ID is missing")
	}
	if n.Type == "" {
		v.errors = append(v.errors, fmt.Sprintf("Flow node %s has no type", n.ID))
	}
}

func (v *Visitor) VisitSequenceFlow(sf *entities.SequenceFlow) {
	if sf.ID == "" {
		v.errors = append(v.errors, "Sequence flow ID is missing")
	}
	if sf.SourceRef == "" {
		v.errors = append(v.errors, fmt.Sprintf("Sequence flow %s has no source reference", sf.ID))
	}
	if sf.TargetRef == "" {
		v.errors = append(v.errors, fmt.Sprintf("Sequence flow %s has no target reference", sf.ID))
	}
}

func (v *Visitor) Errors() []string {
	return v.errors
}

func (v *Visitor) IsValid() bool {
	return len(v.errors) == 0
}
