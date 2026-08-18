package impl

import (
	"context"
	"fmt"
	"strings"

	"github.com/google/uuid"
	"github.com/gsoultan/gobpm/server/domains/adapters"
	"github.com/gsoultan/gobpm/server/domains/entities"
	servicecontracts "github.com/gsoultan/gobpm/server/domains/services/contracts"
	"github.com/gsoultan/gobpm/server/repositories"
	repocontracts "github.com/gsoultan/gobpm/server/repositories/contracts"
	"github.com/gsoultan/gobpm/server/repositories/models"
)

type decisionService struct {
	repo           repositories.Repository
	tableEvaluator servicecontracts.DecisionTableEvaluator
}

// NewDecisionService creates a new DecisionService implementation.
// tableEvaluator is the Strategy used to evaluate decision table rules and hit policies.
func NewDecisionService(repo repositories.Repository, tableEvaluator servicecontracts.DecisionTableEvaluator) servicecontracts.DecisionService {
	return &decisionService{repo: repo, tableEvaluator: tableEvaluator}
}

func (s *decisionService) Evaluate(ctx context.Context, decisionKey string, version int, variables map[string]any) (entities.DecisionResult, error) {
	// Use a copy of variables to avoid polluting caller's map during intermediate steps
	varsCopy := make(map[string]any)
	for k, v := range variables {
		varsCopy[k] = v
	}
	return s.evaluateRecursive(ctx, decisionKey, version, varsCopy, make(map[string]bool))
}

func (s *decisionService) evaluateRecursive(ctx context.Context, decisionKey string, version int, variables map[string]any, seen map[string]bool) (entities.DecisionResult, error) {
	if seen[decisionKey] {
		return entities.DecisionResult{}, fmt.Errorf("circular dependency detected for decision %s", decisionKey)
	}
	seen[decisionKey] = true
	defer delete(seen, decisionKey)

	var m models.DecisionDefinitionModel
	var err error
	if version > 0 {
		m, err = s.repo.Decision().GetByKeyAndVersion(ctx, decisionKey, version)
	} else {
		m, err = s.repo.Decision().GetByKey(ctx, decisionKey)
	}
	if err != nil {
		return entities.DecisionResult{}, err
	}

	decision := adapters.DecisionEntityAdapter{Model: m}.ToEntity()

	// 1. Evaluate required decisions
	for _, reqKey := range decision.RequiredDecisions {
		res, err := s.evaluateRecursive(ctx, reqKey, 0, variables, seen)
		if err != nil {
			return entities.DecisionResult{}, fmt.Errorf("failed to evaluate required decision %s: %w", reqKey, err)
		}
		// DMN says it should be available as decision name/key.
		// If decision has multiple outputs, use a map. If single output, use it directly.
		if len(res.Values) == 1 {
			for _, v := range res.Values {
				variables[reqKey] = v
				break
			}
		} else {
			variables[reqKey] = res.Values
		}
	}

	// 2. Evaluate rules and apply hit policy via the injected Strategy
	return s.tableEvaluator.EvaluateTable(ctx, decision, variables)
}

// ListDecisionsPaged returns one page of a project's decisions, for the same
// reason definitions have one.
func (s *decisionService) ListDecisionsPaged(ctx context.Context, projectID uuid.UUID, page repocontracts.Pagination) (repocontracts.Page[entities.DecisionDefinition], error) {
	result, err := s.repo.Decision().ListByProjectPaged(ctx, projectID, page)
	if err != nil {
		return repocontracts.Page[entities.DecisionDefinition]{}, err
	}
	decisions := make([]entities.DecisionDefinition, len(result.Items))
	for i, m := range result.Items {
		decisions[i] = adapters.DecisionEntityAdapter{Model: m}.ToEntity()
	}
	return repocontracts.NewPage(decisions, result.Total, page), nil
}

func (s *decisionService) ListDecisions(ctx context.Context, projectID uuid.UUID) ([]entities.DecisionDefinition, error) {
	var ms []models.DecisionDefinitionModel
	var err error
	if projectID != uuid.Nil {
		ms, err = s.repo.Decision().ListByProject(ctx, projectID)
	} else {
		ms, err = s.repo.Decision().List(ctx)
	}
	if err != nil {
		return nil, err
	}
	res := make([]entities.DecisionDefinition, len(ms))
	for i, m := range ms {
		res[i] = adapters.DecisionEntityAdapter{Model: m}.ToEntity()
	}
	return res, nil
}

func (s *decisionService) GetDecision(ctx context.Context, id uuid.UUID) (entities.DecisionDefinition, error) {
	m, err := s.repo.Decision().Get(ctx, id)
	if err != nil {
		return entities.DecisionDefinition{}, err
	}
	return adapters.DecisionEntityAdapter{Model: m}.ToEntity(), nil
}

func (s *decisionService) CreateDecision(ctx context.Context, d entities.DecisionDefinition) (uuid.UUID, error) {
	// The key is how a process names this decision, so a keyless one is
	// unreachable by definition. It is also worth rejecting rather than
	// ignoring: the request type carries the decision by value, so a body that
	// omits the "decision" envelope arrives here as the zero value, and the
	// version bump below would look up the empty key, find the last row stored
	// this way, and file another version of it.
	if strings.TrimSpace(d.Key) == "" {
		return uuid.Nil, fmt.Errorf("decision key is required")
	}

	if d.ID == uuid.Nil {
		id, err := uuid.NewV7()
		if err != nil {
			return uuid.Nil, fmt.Errorf("could not generate a decision id: %w", err)
		}
		d.ID = id
	}

	// The version series is per project, matching the unique index, so the
	// allocator needs the same project the adapter is about to write.
	var projectID uuid.UUID
	if d.Project != nil {
		projectID = d.Project.ID
	}

	err := allocateVersion(ctx, s.repo.UnitOfWork(), "decision "+d.Key,
		func(ctx context.Context) (int, error) {
			return s.repo.Decision().NextVersion(ctx, projectID, d.Key)
		},
		func(txCtx context.Context, version int) error {
			d.Version = version
			return s.repo.Decision().Create(txCtx, adapters.DecisionModelAdapter{Decision: d}.ToModel())
		})
	if err != nil {
		return uuid.Nil, err
	}
	return d.ID, nil
}

func (s *decisionService) UpdateDecision(ctx context.Context, id uuid.UUID, d entities.DecisionDefinition) error {
	d.ID = id
	return s.repo.Decision().Update(ctx, id, adapters.DecisionModelAdapter{Decision: d}.ToModel())
}

func (s *decisionService) DeleteDecision(ctx context.Context, id uuid.UUID) error {
	return s.repo.Decision().Delete(ctx, id)
}
