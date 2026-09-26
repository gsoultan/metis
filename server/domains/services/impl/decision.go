package impl

import (
	"context"
	"fmt"
	"strings"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/gsoultan/metis/internal/pkg/apierr"
	"github.com/gsoultan/metis/server/domains/adapters"
	"github.com/gsoultan/metis/server/domains/entities"
	servicecontracts "github.com/gsoultan/metis/server/domains/services/contracts"
	"github.com/gsoultan/metis/server/repositories"
	repocontracts "github.com/gsoultan/metis/server/repositories/contracts"
	"github.com/gsoultan/metis/server/repositories/models"
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

// ErrNoDecisionProject refuses an evaluation that does not say which project
// the table belongs to. Without one a key could name any tenant's table.
var ErrNoDecisionProject = apierr.Invalidf("a decision is looked up in a project, and none was given")

func (s *decisionService) Evaluate(ctx context.Context, projectID uuid.UUID, decisionKey string, version int, variables map[string]any) (entities.DecisionResult, error) {
	if projectID == uuid.Nil {
		return entities.DecisionResult{}, ErrNoDecisionProject
	}
	// Use a copy of variables to avoid polluting caller's map during intermediate steps
	varsCopy := make(map[string]any)
	for k, v := range variables {
		varsCopy[k] = v
	}
	return s.evaluateRecursive(ctx, projectID, decisionKey, version, varsCopy, make(map[string]bool))
}

func (s *decisionService) evaluateRecursive(ctx context.Context, projectID uuid.UUID, decisionKey string, version int, variables map[string]any, seen map[string]bool) (entities.DecisionResult, error) {
	if seen[decisionKey] {
		return entities.DecisionResult{}, fmt.Errorf("circular dependency detected for decision %s", decisionKey)
	}
	seen[decisionKey] = true
	defer delete(seen, decisionKey)

	// A pinned version is exactly that version. Otherwise the live one — which
	// may be older than the newest: a saved version can be staged, and an
	// older one made live again.
	var m models.DecisionDefinitionModel
	var err error
	if version > 0 {
		m, err = s.repo.Decision().GetByKeyAndVersion(ctx, projectID, decisionKey, version)
	} else {
		m, err = s.repo.Decision().GetLiveByKey(ctx, projectID, decisionKey)
	}
	if err != nil {
		return entities.DecisionResult{}, err
	}

	decision := adapters.DecisionEntityAdapter{Model: m}.ToEntity()

	// 1. Evaluate required decisions, in the same project: a requirement names
	// a table beside this one, not one anywhere a key happens to match. At
	// their live versions, whatever version this one is: a requirement names a
	// key, and a staged table must not come into force through a decision
	// that depends on it.
	for _, reqKey := range decision.RequiredDecisions {
		res, err := s.evaluateRecursive(ctx, projectID, reqKey, 0, variables, seen)
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

// maxDecisionSearch is the longest search a decision list runs: names and keys
// are at most 255 characters, so a longer search matches nothing.
const maxDecisionSearch = 255

// ListDecisionsPaged returns one page of a project's decisions, for the same
// reason definitions have one, narrowed by search when it is not empty.
func (s *decisionService) ListDecisionsPaged(ctx context.Context, projectID uuid.UUID, search string, page repocontracts.Pagination) (repocontracts.Page[entities.DecisionDefinition], error) {
	if utf8.RuneCountInString(search) > maxDecisionSearch {
		return repocontracts.Page[entities.DecisionDefinition]{}, apierr.Invalidf(
			"a search is at most %d characters, the longest a decision's name or key can be", maxDecisionSearch)
	}
	result, err := s.repo.Decision().ListByProjectPaged(ctx, projectID, search, page)
	if err != nil {
		return repocontracts.Page[entities.DecisionDefinition]{}, err
	}
	decisions := make([]entities.DecisionDefinition, len(result.Items))
	for i, m := range result.Items {
		decisions[i] = adapters.DecisionEntityAdapter{Model: m}.ToEntity()
	}
	return repocontracts.NewPage(decisions, result.Total, page), nil
}

// ListDecisionSummaries returns one page of a project's decision keys, one row
// each, without the tables: the decision list, a step's picker and the
// dependency graph all want every key once, and which version of it is live.
func (s *decisionService) ListDecisionSummaries(ctx context.Context, projectID uuid.UUID, search string, page repocontracts.Pagination) (repocontracts.Page[entities.DecisionSummary], error) {
	if utf8.RuneCountInString(search) > maxDecisionSearch {
		return repocontracts.Page[entities.DecisionSummary]{}, apierr.Invalidf(
			"a search is at most %d characters, the longest a decision's name or key can be", maxDecisionSearch)
	}
	result, err := s.repo.Decision().ListKeysByProject(ctx, projectID, search, page)
	if err != nil {
		return repocontracts.Page[entities.DecisionSummary]{}, err
	}
	summaries := make([]entities.DecisionSummary, len(result.Items))
	for i, m := range result.Items {
		summaries[i] = entities.DecisionSummary{
			ID:                uuid.UUID(m.ID),
			Key:               m.Key,
			Name:              m.Name,
			Version:           m.Version,
			HitPolicy:         m.HitPolicy,
			RequiredDecisions: m.RequiredDecisions,
			LiveVersion:       m.LiveVersion,
			NewestVersion:     m.NewestVersion,
			LastChangedAt:     m.LastChangedAt,
		}
	}
	return repocontracts.NewPage(summaries, result.Total, page), nil
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

	// Live, as creating one always was: every caller written before a save
	// could stage means exactly this.
	saved, err := s.storeNewVersion(ctx, d, true)
	if err != nil {
		return uuid.Nil, err
	}
	return saved.ID, nil
}

// DeleteDecision removes one stored version of a decision table, unless
// something still needs it.
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
//
// Nor can the live version go while other versions remain: every step with no
// version binding would be left with nothing to evaluate. The only version of a
// key can: deleting it is deleting the decision.
func (s *decisionService) DeleteDecision(ctx context.Context, id uuid.UUID) error {
	decision, err := s.GetDecision(ctx, id)
	if err != nil {
		return err
	}

	users, err := s.processesUsing(ctx, decision)
	if err != nil {
		return err
	}
	blocking := 0
	for _, process := range users {
		blocking += process.RunningInstances
	}
	if blocking > 0 {
		return apierr.Invalidf(
			"cannot delete v%d of the decision %q: %d running process %s still reach it; complete or cancel them first",
			decision.Version, decision.Key, blocking, pluralInstances(blocking))
	}

	projectID := decision.Project.ID
	return s.repo.UnitOfWork().Do(ctx, func(txCtx context.Context) error {
		// Locked before the check, as a promotion locks it before its write:
		// a version being made live cannot be deleted in the same moment, and
		// one being deleted cannot be made live.
		if _, err := s.repo.Decision().LockVersion(txCtx, projectID, decision.Key, decision.Version); err != nil {
			return err
		}
		if err := s.refuseToStrandTheKey(txCtx, projectID, decision); err != nil {
			return err
		}
		return s.repo.Decision().Delete(txCtx, id)
	})
}

// refuseToStrandTheKey refuses to delete the live version of a key that has
// other versions.
func (s *decisionService) refuseToStrandTheKey(ctx context.Context, projectID uuid.UUID, decision entities.DecisionDefinition) error {
	live, err := s.liveVersion(ctx, projectID, decision.Key)
	if err != nil || live != decision.Version {
		return err
	}
	versions, err := s.repo.Decision().ListVersionsByKey(ctx, projectID, decision.Key)
	if err != nil {
		return err
	}
	if others := len(versions) - 1; others > 0 {
		return apierr.Invalidf(
			"v%d is the live version of the decision %q and %d other %s; make another version live first, then delete this one",
			decision.Version, decision.Key, others, otherVersionsRemain(others))
	}
	return nil
}

// DecisionImpact answers "what breaks if I change this?".
//
// A decision table is a policy several processes can share, and the person
// about to edit one is usually the person least able to see who else depends on
// it. Before changing a threshold they should know which processes consult the
// table and how many instances are part-way through one.
func (s *decisionService) DecisionImpact(ctx context.Context, id uuid.UUID) (entities.DecisionImpact, error) {
	decision, err := s.GetDecision(ctx, id)
	if err != nil {
		return entities.DecisionImpact{}, err
	}
	users, err := s.processesUsing(ctx, decision)
	if err != nil {
		return entities.DecisionImpact{}, err
	}

	impact := entities.DecisionImpact{DecisionKey: decision.Key, Processes: users}
	for _, process := range users {
		impact.RunningInstances += process.RunningInstances
	}
	return impact, nil
}

// processesUsing lists the processes that can reach this decision, and how many
// of their instances are still running.
//
// It works from the definitions rather than from the instances: an instance
// reaches a decision through a business rule task in the process it is running,
// so the question is which processes contain such a task. Definitions are few;
// instances are many.
//
// Every version of every process in the project, walked a batch at a time with
// its graph: a running instance can be on any of them. They were read as a
// list, which stopped at the newest thousand — past which a version still
// consulting the decision went unseen and the delete went through — and then
// read again one at a time for their graphs.
func (s *decisionService) processesUsing(ctx context.Context, decision entities.DecisionDefinition) ([]entities.DecisionUsage, error) {
	var projectID uuid.UUID
	if decision.Project != nil {
		projectID = decision.Project.ID
	}

	var usages []entities.DecisionUsage
	err := s.repo.Definition().ScanProjectWithGraphs(ctx, projectID, func(batch []models.ProcessDefinitionModel) error {
		for _, m := range batch {
			if usage, ok := decisionUsageOf(m, decision.Key); ok {
				usages = append(usages, usage)
			}
		}
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("could not check which processes use the decision: %w", err)
	}
	return s.countRunning(ctx, usages)
}

// decisionUsageOf names the steps of one version that consult the decision.
func decisionUsageOf(m models.ProcessDefinitionModel, key string) (entities.DecisionUsage, bool) {
	nodes := nodesUsingDecision(m, key)
	if len(nodes) == 0 {
		return entities.DecisionUsage{}, false
	}
	return entities.DecisionUsage{
		DefinitionID:   uuid.UUID(m.ID),
		DefinitionKey:  m.Key,
		DefinitionName: m.Name,
		Version:        m.Version,
		Steps:          nodes,
	}, true
}

// countRunning fills in how many instances of each process are still running,
// in one grouped count.
//
// Counted, not listed. Reading the instances to count them read a thousand at
// most — the store's default limit — so a running instance started before a
// thousand others finished was not counted, and the decision it still needed
// could be deleted. Reading all of them instead would load every instance a
// version ever had to answer one number.
func (s *decisionService) countRunning(ctx context.Context, usages []entities.DecisionUsage) ([]entities.DecisionUsage, error) {
	if len(usages) == 0 {
		return usages, nil
	}
	ids := make([]uuid.UUID, len(usages))
	for i, usage := range usages {
		ids[i] = usage.DefinitionID
	}
	counts, err := s.repo.Process().CountInstancesByDefinitions(ctx, ids)
	if err != nil {
		return nil, fmt.Errorf("could not check which instances use the decision: %w", err)
	}
	for i := range usages {
		count := counts[usages[i].DefinitionID]
		// Suspended counts as running: it is stopped, not finished, and
		// resuming it must not meet a policy that changed underneath it.
		usages[i].RunningInstances = int(count.Running + count.Suspended)
	}
	return usages, nil
}

// nodesUsingDecision names the steps of a process that consult this decision,
// including steps nested inside sub-processes.
//
// Named rather than counted: "Score the applicant" tells whoever is about to
// change a policy where it is used; "3 steps" does not.
func nodesUsingDecision(m models.ProcessDefinitionModel, key string) []string {
	var found []string
	var walk func(nodes []models.FlowNode)
	walk = func(nodes []models.FlowNode) {
		for _, node := range nodes {
			if usesDecision(node, key) {
				name := node.Name
				if name == "" {
					name = node.ID
				}
				found = append(found, name)
			}
			walk(node.Nodes)
		}
	}
	walk(m.Nodes)
	return found
}

// usesDecision reports whether one step consults this decision — either as the
// decision it evaluates, or as the table that decides who does it.
func usesDecision(node models.FlowNode, key string) bool {
	for _, property := range []string{"decision_key", handlerAssignmentDecisionKey} {
		if configured, isText := node.Properties[property].(string); isText && configured == key {
			return true
		}
	}
	return false
}

// handlerAssignmentDecisionKey mirrors handlers/impl.AssignmentDecisionKey.
//
// Duplicated rather than imported: services must not depend on handlers, and
// one string is a smaller price than that dependency. A test pins the two
// together so they cannot drift.
const handlerAssignmentDecisionKey = "assignment_decision_key"

// AssignmentDecisionKeyForTest exposes that constant so a test can hold it
// against the handler's own and fail if they drift. If they do, the impact view
// goes quietly blind to every approval matrix.
const AssignmentDecisionKeyForTest = handlerAssignmentDecisionKey

func otherVersionsRemain(n int) string {
	if n == 1 {
		return "version remains"
	}
	return "versions remain"
}

func pluralInstances(n int) string {
	if n == 1 {
		return "instance"
	}
	return "instances"
}
