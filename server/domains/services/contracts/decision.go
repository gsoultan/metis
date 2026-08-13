package contracts

import (
	"context"
	"github.com/google/uuid"
	"github.com/gsoultan/gobpm/server/domains/entities"
	repocontracts "github.com/gsoultan/gobpm/server/repositories/contracts"
)

// DecisionEvaluator handles decision evaluation by key and version.
// Consumers that only need to evaluate decisions depend on this narrow interface.
type DecisionEvaluator interface {
	Evaluate(ctx context.Context, decisionKey string, version int, variables map[string]any) (entities.DecisionResult, error)
}

// DecisionManager handles CRUD lifecycle of decision definitions.
type DecisionManager interface {
	ListDecisions(ctx context.Context, projectID uuid.UUID) ([]entities.DecisionDefinition, error)

	// ListDecisionsPaged returns one page of a project's decisions.
	ListDecisionsPaged(ctx context.Context, projectID uuid.UUID, page repocontracts.Pagination) (repocontracts.Page[entities.DecisionDefinition], error)
	GetDecision(ctx context.Context, id uuid.UUID) (entities.DecisionDefinition, error)
	CreateDecision(ctx context.Context, def entities.DecisionDefinition) (uuid.UUID, error)
	UpdateDecision(ctx context.Context, id uuid.UUID, def entities.DecisionDefinition) error
	DeleteDecision(ctx context.Context, id uuid.UUID) error
}

// DecisionService composes DecisionEvaluator and DecisionManager into the full
// decision service contract used by the service facade.
type DecisionService interface {
	DecisionEvaluator
	DecisionManager
}
