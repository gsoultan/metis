package impl

import (
	"context"

	"github.com/gsoultan/metis/internal/pkg/apierr"
	"github.com/gsoultan/metis/server/domains/entities"
	servicecontracts "github.com/gsoultan/metis/server/domains/services/contracts"
	"github.com/gsoultan/metis/server/domains/services/impl/sqlconnector"
)

// authorizeLookups holds a definition that carries a database lookup — a step
// with SQL its author wrote — to somebody who may author one, and checks each
// such step as far as it can be checked before anyone knows which database it
// will run against.
//
// Here, in DeployDefinition, because every way a definition is deployed — the
// definition endpoint, a deployment's resources, a BPMN import — arrives here,
// and a check on one endpoint is a check the next endpoint forgets.
//
// A lookup is recognised by carrying a statement, not by which connector it
// names. The statement is what the executor reads; a step could name its
// connection by instance rather than by catalogue entry, and a check on the
// entry would miss it.
func authorizeLookups(ctx context.Context, def *entities.ProcessDefinition) error {
	finder := &statementFinder{}
	def.Accept(finder)
	if len(finder.steps) == 0 {
		return nil
	}
	if !mayAuthorQueries(ctx) {
		return apierr.Forbiddenf("this process looks things up in a database, and deploying it needs the " +
			"Query author role; ask an administrator to grant it, or to deploy the process")
	}
	for _, step := range finder.steps {
		err := sqlconnector.ValidateStep(
			step.GetStringProperty(servicecontracts.StatementProperty),
			step.GetStringProperty(servicecontracts.ResultVariableProperty))
		if err != nil {
			return apierr.Invalidf("the step %q: %v", stepLabel(step), err)
		}
	}
	return nil
}

// mayAuthorQueries is true for a caller holding the Query author role, or an
// administrator.
//
// Nobody else — including work marked as system work. No background job
// deploys definitions today; the first one that does, deploying definitions
// somebody else wrote, should have to decide who is answerable for their
// queries rather than inherit a way round this.
func mayAuthorQueries(ctx context.Context) bool {
	roles, known := callerRoles(ctx)
	if !known {
		return false
	}
	return entities.HasRole(roles, entities.RoleAdmin) || entities.HasRole(roles, entities.RoleQueryAuthor)
}

func stepLabel(node *entities.Node) string {
	if node.Name != "" {
		return node.Name
	}
	return node.ID
}
