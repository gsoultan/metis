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

// DeleteDecision removes a decision table, unless something still needs it.
//
// A decision is a business policy, and a running instance is a commitment made
// under it. Deleting a table an instance is still going to consult turns that
// instance into one that fails at a step which worked yesterday, with an error
// naming a key that no longer exists — and by then nobody remembers what the
// table said. So the deletion is refused and the message names what is in the
// way.
//
// Completed instances do not count: they have already made their decisions, and
// what those were is recorded on their timelines rather than read back from the
// table.
func (s *decisionService) DeleteDecision(ctx context.Context, id uuid.UUID) error {
	decision, err := s.GetDecision(ctx, id)
	if err != nil {
		return err
	}

	blocking, err := s.instancesNeeding(ctx, decision)
	if err != nil {
		return err
	}
	if blocking > 0 {
		return fmt.Errorf(
			"cannot delete the decision %q: %d running process %s still reach it; complete or cancel them first",
			decision.Key, blocking, pluralInstances(blocking))
	}

	return s.repo.Decision().Delete(ctx, id)
}

// instancesNeeding counts the running instances whose process can still consult
// this decision.
//
// It works from the definitions rather than from the instances: an instance
// reaches a decision through a business rule task in the process it is running,
// so the question is which running processes contain such a task. Definitions
// are few and cached by the database; instances are many.
func (s *decisionService) instancesNeeding(ctx context.Context, decision entities.DecisionDefinition) (int, error) {
	var projectID uuid.UUID
	if decision.Project != nil {
		projectID = decision.Project.ID
	}

	definitions, err := s.repo.Definition().ListByProject(ctx, projectID)
	if err != nil {
		return 0, fmt.Errorf("could not check which processes use the decision: %w", err)
	}

	blocking := 0
	for _, listed := range definitions {
		// The list query selects a few columns and deliberately leaves the BPMN
		// out — it feeds a picker, not an analysis — so the nodes have to be
		// fetched. That is a full read per definition, which is why this is only
		// ever done on a delete: a rare, deliberate act by an administrator, not
		// something on any request path.
		m, err := s.repo.Definition().Get(ctx, uuid.UUID(listed.ID))
		if err != nil {
			return 0, fmt.Errorf("could not read the process %q while checking the decision: %w", listed.Key, err)
		}
		if !definitionUsesDecision(m, decision.Key) {
			continue
		}
		instances, err := s.repo.Process().ListByDefinition(ctx, uuid.UUID(m.ID))
		if err != nil {
			return 0, fmt.Errorf("could not check which instances use the decision: %w", err)
		}
		for _, instance := range instances {
			// Suspended counts as running: it is stopped, not finished, and
			// resuming it must not fail on a missing table.
			if instance.Status == models.ProcessActive || instance.Status == models.ProcessSuspended {
				blocking++
			}
		}
	}
	return blocking, nil
}

// definitionUsesDecision reports whether any step of a process consults this
// decision, including steps nested inside sub-processes.
func definitionUsesDecision(m models.ProcessDefinitionModel, key string) bool {
	var walk func(nodes []models.FlowNode) bool
	walk = func(nodes []models.FlowNode) bool {
		for _, node := range nodes {
			if node.Type == models.BusinessRuleTask {
				configured, isText := node.Properties["decision_key"].(string)
				if isText && configured == key {
					return true
				}
			}
			if walk(node.Nodes) {
				return true
			}
		}
		return false
	}
	return walk(m.Nodes)
}

func pluralInstances(n int) string {
	if n == 1 {
		return "instance"
	}
	return "instances"
}
