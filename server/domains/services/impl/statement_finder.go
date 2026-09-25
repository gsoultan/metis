package impl

import (
	"strings"

	"github.com/gsoultan/metis/server/domains/entities"
	servicecontracts "github.com/gsoultan/metis/server/domains/services/contracts"
)

// statementFinder collects the steps, at any depth, that carry a statement of
// their own. A visitor, so a step inside a sub-process is found the same way
// validation finds it: a check that looked only at the top level would be
// passed by moving the step one level down.
type statementFinder struct {
	steps []*entities.Node
}

func (f *statementFinder) VisitDefinition(*entities.ProcessDefinition) {}

func (f *statementFinder) VisitFlowNode(n *entities.Node) {
	if strings.TrimSpace(n.GetStringProperty(servicecontracts.StatementProperty)) != "" {
		f.steps = append(f.steps, n)
	}
}

func (f *statementFinder) VisitSequenceFlow(*entities.SequenceFlow) {}
