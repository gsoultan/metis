package impl

import (
	"context"
	"strings"

	"github.com/google/uuid"

	"github.com/gsoultan/metis/internal/pkg/apierr"
	"github.com/gsoultan/metis/server/domains/entities"
	servicecontracts "github.com/gsoultan/metis/server/domains/services/contracts"
)

// TryConnectorStep runs a connector step once, as the job would, against its
// project's saved connection — without a job, a service call record, a breaker,
// a rate limit, or any process.
//
// What makes it safe to offer a designer is where the connection comes from:
// the one an administrator saved for the project, resolved exactly as the job
// resolves it, so the caller chooses what is sent and never where it goes. That
// is the difference from POST /connectors/execute, which takes the caller's own
// configuration and is an administrator's for that reason.
//
// It is real: a Slack step posts, an email step sends. The designer says so.
//
// A step that carries its own statement — a database lookup — runs SQL its
// author wrote, so trying one needs what deploying one needs.
func (s *jobService) TryConnectorStep(ctx context.Context, projectID uuid.UUID, node entities.Node, variables map[string]any) (map[string]any, error) {
	if node.GetStringProperty("connector_id") == "" && node.GetStringProperty("connector_instance_id") == "" {
		return nil, apierr.Invalidf("this step does not name a connector to try")
	}
	if strings.TrimSpace(node.GetStringProperty(servicecontracts.StatementProperty)) != "" && !mayAuthorQueries(ctx) {
		return nil, apierr.Forbiddenf("this step looks something up in a database, and trying it needs the " +
			"Query author role; ask an administrator to grant it")
	}
	if variables == nil {
		variables = map[string]any{}
	}
	def := &entities.ProcessDefinition{Project: &entities.Project{ID: projectID}}
	return s.resolveAndExecuteConnector(ctx, def, node, variables)
}
