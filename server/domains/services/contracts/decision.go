package contracts

import (
	"context"

	"github.com/google/uuid"
	"github.com/gsoultan/metis/server/domains/entities"
	repocontracts "github.com/gsoultan/metis/server/repositories/contracts"
)

// DecisionEvaluator handles decision evaluation by key and version.
// Consumers that only need to evaluate decisions depend on this narrow interface.
//
// A key names a table only within a project — keys are unique per project, not
// per installation — so the project is part of the question. Tables a decision
// requires are resolved in the same project.
type DecisionEvaluator interface {
	Evaluate(ctx context.Context, projectID uuid.UUID, decisionKey string, version int, variables map[string]any) (entities.DecisionResult, error)
}

// DecisionManager handles CRUD lifecycle of decision definitions.
type DecisionManager interface {
	ListDecisions(ctx context.Context, projectID uuid.UUID) ([]entities.DecisionDefinition, error)

	// ListDecisionsPaged returns one page of a project's decisions, narrowed to
	// those whose name or key contains search when it is not empty.
	ListDecisionsPaged(ctx context.Context, projectID uuid.UUID, search string, page repocontracts.Pagination) (repocontracts.Page[entities.DecisionDefinition], error)
	GetDecision(ctx context.Context, id uuid.UUID) (entities.DecisionDefinition, error)
	CreateDecision(ctx context.Context, def entities.DecisionDefinition) (uuid.UUID, error)

	// UpdateDecision saves an edit of the version id names as the next
	// version of its key, and makes it live unless promote is false and
	// another version is live to keep. The stored version is never changed,
	// and a table that is the same as it stores and changes nothing.
	UpdateDecision(ctx context.Context, id uuid.UUID, def entities.DecisionDefinition, promote bool) (entities.SavedDecision, error)
	// DecisionImpact reports which processes consult a decision and how many of
	// their instances are still running, so the size of a policy change is
	// visible before it is made.
	DecisionImpact(ctx context.Context, id uuid.UUID) (entities.DecisionImpact, error)

	// RunDecisionTests evaluates a table against the examples stored with it.
	// A decision table nobody can test is a spreadsheet with extra steps.
	RunDecisionTests(ctx context.Context, id uuid.UUID) ([]entities.DecisionTestResult, error)

	DeleteDecision(ctx context.Context, id uuid.UUID) error
}

// DecisionCatalog lists what decisions a project has, without their tables.
//
// Its own interface rather than an eighth method on DecisionManager: the
// callers that want it — the dependency graph, a step's decision picker — want
// nothing else from the service.
type DecisionCatalog interface {
	// ListDecisionSummaries returns one page of a project's decision keys, each
	// as its newest version, ordered by key.
	ListDecisionSummaries(ctx context.Context, projectID uuid.UUID, page repocontracts.Pagination) (repocontracts.Page[entities.DecisionSummary], error)
}

// DecisionVersioning is a decision key's history, and the choice of which
// version in it is live.
//
// Its own interface rather than two more methods on DecisionManager, which is
// already past the size the guidelines allow: a version history reads and
// writes something the rest of the manager does not — the release timeline.
type DecisionVersioning interface {
	// ListDecisionVersions returns every stored version of one key, newest
	// first, marking the live one.
	ListDecisionVersions(ctx context.Context, projectID uuid.UUID, key string) ([]entities.DecisionVersionStatus, error)

	// PromoteDecisionVersion makes one stored version the live one: from now
	// on, evaluations that name no version read it. An older version made live
	// again is a rollback.
	PromoteDecisionVersion(ctx context.Context, projectID uuid.UUID, key string, version int) error
}

// DecisionService composes DecisionEvaluator, DecisionManager,
// DecisionCatalog and DecisionVersioning into the full decision service
// contract used by the service facade.
type DecisionService interface {
	DecisionEvaluator
	DecisionManager
	DecisionCatalog
	DecisionVersioning
}
