package impl

import (
	"context"
	"encoding/json"

	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/gsoultan/metis/internal/pkg/apierr"
	"github.com/gsoultan/metis/server/domains/entities"
	servicecontracts "github.com/gsoultan/metis/server/domains/services/contracts"
	repocontracts "github.com/gsoultan/metis/server/repositories/contracts"
)

// approvalDefinitions is what the approval service needs of definitions.
type approvalDefinitions interface {
	ImportDefinition(ctx context.Context, projectID uuid.UUID, xml []byte) (uuid.UUID, error)
	GetDefinition(ctx context.Context, id uuid.UUID) (*entities.ProcessDefinition, error)
	ListDefinitions(ctx context.Context, projectID uuid.UUID) ([]*entities.ProcessDefinition, error)
	ListLiveVersions(ctx context.Context, projectID uuid.UUID) (map[string]int, error)
}

// approvalEngine is what it needs of the engine.
type approvalEngine interface {
	StartProcess(ctx context.Context, projectID uuid.UUID, definitionKey string, vars map[string]any) (uuid.UUID, error)
	GetInstance(ctx context.Context, id uuid.UUID) (entities.ProcessInstance, error)
	ListInstancesPaged(ctx context.Context, projectID uuid.UUID, filter repocontracts.InstanceFilter, page repocontracts.Pagination) (repocontracts.Page[entities.ProcessInstance], error)
}

// approvalTasks is what it needs of tasks. CompleteTask is the authority on
// who may decide: Decide adds what is decided, never who may.
type approvalTasks interface {
	GetTask(ctx context.Context, id uuid.UUID) (entities.Task, error)
	ListTasksByInstancePaged(ctx context.Context, instanceID uuid.UUID, page repocontracts.Pagination) (repocontracts.Page[entities.Task], error)
	CompleteTask(ctx context.Context, id uuid.UUID, userID string, vars map[string]any) error
}

type approvalLineService struct {
	defs   approvalDefinitions
	engine approvalEngine
	tasks  approvalTasks
	// moves is nil on a server wired without migrations; moving requests
	// is then refused.
	moves approvalMigrations
	now   func() time.Time
}

// approvalMigrations is what moving requests between versions needs: the
// migration an administrator applies in the designer, nothing of its own.
type approvalMigrations interface {
	PlanInstanceMigration(ctx context.Context, sourceDefID, targetDefID uuid.UUID, nodeMapping map[string]string, opts ...servicecontracts.MigrationOption) (entities.MigrationPlan, error)
	ApplyInstanceMigration(ctx context.Context, sourceDefID uuid.UUID, targetDefID uuid.UUID, nodeMapping map[string]string, opts ...servicecontracts.MigrationOption) (entities.MigrationResult, error)
}

// NewApprovalLineService runs approval lines on definitions, the engine and
// tasks: a line is nothing but a definition, and a request an instance.
func NewApprovalLineService(defs approvalDefinitions, engine approvalEngine, tasks approvalTasks, moves approvalMigrations) *approvalLineService {
	return &approvalLineService{defs: defs, engine: engine, tasks: tasks, moves: moves, now: time.Now}
}

// maxCommentRunes bounds a decision's comment, as a hand-over's reason is.
const maxCommentRunes = 1000

func (s *approvalLineService) DefineApprovalLine(ctx context.Context, projectID uuid.UUID, line entities.ApprovalLine) (entities.ApprovalLine, error) {
	if projectID == uuid.Nil {
		return entities.ApprovalLine{}, apierr.Invalidf("an approval line needs a project")
	}
	xml, err := approvalLineBPMN(line)
	if err != nil {
		return entities.ApprovalLine{}, apierr.Invalidf("%s", err.Error())
	}
	// A key already used by a process that is not a line would gain a
	// version that is one, and every running request of the other shape would
	// be read as a line it never was.
	if existing, found, err := s.latest(ctx, projectID, line.Key); err != nil {
		return entities.ApprovalLine{}, err
	} else if found {
		if _, isLine := approvalLineOf(existing); !isLine {
			return entities.ApprovalLine{}, apierr.Invalidf("%s is already a process that is not an approval line; pick another key", line.Key)
		}
	}
	id, err := s.defs.ImportDefinition(ctx, projectID, xml)
	if err != nil {
		return entities.ApprovalLine{}, err
	}
	def, err := s.defs.GetDefinition(ctx, id)
	if err != nil {
		return entities.ApprovalLine{}, err
	}
	deployed, ok := approvalLineOf(def)
	if !ok {
		return entities.ApprovalLine{}, fmt.Errorf("approval line %s was deployed but does not read back as one", line.Key)
	}
	return deployed, nil
}

func (s *approvalLineService) GetApprovalLine(ctx context.Context, projectID uuid.UUID, key string) (entities.ApprovalLine, error) {
	def, found, err := s.live(ctx, projectID, key)
	if err != nil {
		return entities.ApprovalLine{}, err
	}
	line, isLine := approvalLineOf(def)
	if !found || !isLine {
		return entities.ApprovalLine{}, fmt.Errorf("%w: no approval line %s in this project", apierr.ErrNotFound, key)
	}
	return line, nil
}

func (s *approvalLineService) SubmitApproval(ctx context.Context, projectID uuid.UUID, key string, vars map[string]any, actor string) (entities.ApprovalRequest, error) {
	if strings.TrimSpace(actor) == "" {
		return entities.ApprovalRequest{}, apierr.Forbiddenf("a request is submitted by somebody")
	}
	if _, err := s.GetApprovalLine(ctx, projectID, key); err != nil {
		return entities.ApprovalRequest{}, err
	}
	if err := refuseApprovalVariables(vars); err != nil {
		return entities.ApprovalRequest{}, err
	}
	start := make(map[string]any, len(vars)+3)
	for k, v := range vars {
		start[k] = v
	}
	start[entities.ApprovalVarSubmittedBy] = actor
	start[entities.ApprovalVarRound] = 1
	start[entities.ApprovalVarTrail] = []any{}
	id, err := s.engine.StartProcess(ctx, projectID, key, start)
	if err != nil {
		return entities.ApprovalRequest{}, err
	}
	return s.GetApprovalRequest(ctx, id)
}

func (s *approvalLineService) DecideApproval(ctx context.Context, taskID uuid.UUID, decision, comment string, vars map[string]any, actor string) (entities.ApprovalRequest, error) {
	if strings.TrimSpace(actor) == "" {
		return entities.ApprovalRequest{}, apierr.Forbiddenf("a decision is made by somebody")
	}
	task, err := s.tasks.GetTask(ctx, taskID)
	if err != nil {
		return entities.ApprovalRequest{}, err
	}
	if task.Instance == nil {
		return entities.ApprovalRequest{}, fmt.Errorf("%w: task %s belongs to no request", apierr.ErrNotFound, taskID)
	}
	instance, err := s.engine.GetInstance(ctx, task.Instance.ID)
	if err != nil {
		return entities.ApprovalRequest{}, err
	}
	def, err := s.definitionOf(ctx, instance)
	if err != nil {
		return entities.ApprovalRequest{}, err
	}
	line, isLine := approvalLineOf(def)
	if !isLine {
		return entities.ApprovalRequest{}, apierr.Invalidf("task %s is not on an approval line: complete it as a task", taskID)
	}
	node := task.NodeID()
	step, onStep := strings.CutPrefix(node, approvalStepPrefix)
	allowed := approvalActions(line, node)
	if len(allowed) == 0 {
		return entities.ApprovalRequest{}, apierr.Invalidf("task %s is not an approval line's step", taskID)
	}
	decision = strings.TrimSpace(decision)
	if !slices.Contains(allowed, decision) {
		return entities.ApprovalRequest{}, apierr.Invalidf("%q cannot be decided here: %s", decision, strings.Join(allowed, ", "))
	}
	comment = strings.TrimSpace(comment)
	if len([]rune(comment)) > maxCommentRunes {
		return entities.ApprovalRequest{}, apierr.Invalidf("a comment is at most %d characters", maxCommentRunes)
	}
	if comment == "" && decision != entities.ApprovalApprove && decision != entities.ApprovalResubmit {
		return entities.ApprovalRequest{}, apierr.Invalidf("say why: a %s needs a comment", decision)
	}
	if len(vars) > 0 && decision != entities.ApprovalResubmit {
		return entities.ApprovalRequest{}, apierr.Invalidf("variables are changed only by resubmitting a request")
	}
	if err := refuseApprovalVariables(vars); err != nil {
		return entities.ApprovalRequest{}, err
	}

	round := intVariable(instance.Variables[entities.ApprovalVarRound], 1)
	if !onStep {
		step = approvalReviseNode
	}
	trail := append(approvalTrailOf(instance.Variables), entities.ApprovalDecision{
		Step: step, Decision: decision, Comment: comment, By: actor, At: s.now().UTC(), Round: round,
	})
	done := make(map[string]any, len(vars)+3)
	for k, v := range vars {
		done[k] = v
	}
	done[entities.ApprovalVarDecision] = decision
	done[entities.ApprovalVarTrail] = trailVariable(trail)
	if decision == entities.ApprovalResubmit {
		done[entities.ApprovalVarRound] = round + 1
	}
	if err := s.tasks.CompleteTask(ctx, taskID, actor, done); err != nil {
		return entities.ApprovalRequest{}, err
	}
	return s.GetApprovalRequest(ctx, instance.ID)
}

func (s *approvalLineService) ListApprovalRequests(ctx context.Context, projectID uuid.UUID, key, submittedBy string) ([]entities.ApprovalRequest, error) {
	defs, err := s.defs.ListDefinitions(ctx, projectID)
	if err != nil {
		return nil, err
	}
	var out []entities.ApprovalRequest
	found := false
	for _, d := range defs {
		if d == nil || d.Key != key {
			continue
		}
		def := d
		if len(def.Nodes) == 0 {
			if def, err = s.defs.GetDefinition(ctx, d.ID); err != nil {
				return nil, err
			}
		}
		line, isLine := approvalLineOf(def)
		if !isLine {
			continue
		}
		found = true
		// Read newest first, a page at a time, until enough are kept: a
		// filter on the submitter is applied here, not in the database.
		for page := 1; len(out) < entities.MaxApprovalRequestsListed && page <= 20; page++ {
			got, err := s.engine.ListInstancesPaged(ctx, projectID, repocontracts.InstanceFilter{DefinitionID: def.ID},
				repocontracts.Pagination{Page: page, PageSize: 100})
			if err != nil {
				return nil, err
			}
			for _, instance := range got.Items {
				req := approvalRequestOf(line, instance, nil)
				if submittedBy == "" || req.SubmittedBy == submittedBy {
					out = append(out, req)
				}
			}
			if int64(page*100) >= got.Total || len(got.Items) == 0 {
				break
			}
		}
	}
	if !found {
		return nil, fmt.Errorf("%w: no approval line %s in this project", apierr.ErrNotFound, key)
	}
	slices.SortStableFunc(out, func(a, b entities.ApprovalRequest) int { return b.SubmittedAt.Compare(a.SubmittedAt) })
	if len(out) > entities.MaxApprovalRequestsListed {
		out = out[:entities.MaxApprovalRequestsListed]
	}
	return out, nil
}

func (s *approvalLineService) GetApprovalRequest(ctx context.Context, instanceID uuid.UUID) (entities.ApprovalRequest, error) {
	instance, err := s.engine.GetInstance(ctx, instanceID)
	if err != nil {
		return entities.ApprovalRequest{}, err
	}
	def, err := s.definitionOf(ctx, instance)
	if err != nil {
		return entities.ApprovalRequest{}, err
	}
	line, isLine := approvalLineOf(def)
	if !isLine {
		return entities.ApprovalRequest{}, fmt.Errorf("%w: instance %s is not an approval request", apierr.ErrNotFound, instanceID)
	}
	page, err := s.tasks.ListTasksByInstancePaged(ctx, instanceID, repocontracts.Pagination{Page: 1, PageSize: 100})
	if err != nil {
		return entities.ApprovalRequest{}, err
	}
	return approvalRequestOf(line, instance, page.Items), nil
}

// approvalRequestOf puts a request together from its line, its instance and
// its tasks.
func approvalRequestOf(line entities.ApprovalLine, instance entities.ProcessInstance, tasks []entities.Task) entities.ApprovalRequest {
	vars := instance.Variables
	req := entities.ApprovalRequest{
		InstanceID: instance.ID, Key: line.Key, Name: line.Name, Version: line.Version,
		SubmittedAt: instance.CreatedAt,
		Round:       intVariable(vars[entities.ApprovalVarRound], 1),
		Trail:       approvalTrailOf(vars),
		Variables:   map[string]any{},
	}
	if who, ok := vars[entities.ApprovalVarSubmittedBy].(string); ok {
		req.SubmittedBy = who
	}
	for k, v := range vars {
		if !isApprovalVariable(k) {
			req.Variables[k] = v
		}
	}
	this := map[string]entities.ApprovalDecision{}
	for _, d := range req.Trail {
		if d.Round == req.Round {
			this[d.Step] = d
		}
	}
	open := map[string]entities.Task{}
	for _, t := range tasks {
		if t.Status != entities.TaskCompleted && t.Status != entities.TaskCanceled {
			open[t.NodeID()] = t
		}
	}
	for _, s := range line.Steps {
		st := entities.ApprovalStepState{ID: s.ID, Name: s.Name, Status: entities.ApprovalStepPending, Users: s.Users, Groups: s.Groups}
		if d, ok := this[s.ID]; ok {
			d := d
			st.Decision = &d
			st.Status = map[string]string{
				entities.ApprovalApprove: entities.ApprovalStepApproved,
				entities.ApprovalReject:  entities.ApprovalStepRejected,
				entities.ApprovalRevise:  entities.ApprovalStepSentBack,
			}[d.Decision]
		}
		if t, ok := open[approvalStepNode(s.ID)]; ok {
			st.Status = entities.ApprovalStepWaiting
			st.Decision = nil
			st.Waiting = waitingOn(t)
		}
		req.Steps = append(req.Steps, st)
	}
	if t, ok := open[approvalReviseNode]; ok {
		req.Revision = waitingOn(t)
	}

	last := ""
	if n := len(req.Trail); n > 0 {
		last = req.Trail[n-1].Decision
	}
	switch {
	case instance.Status == entities.ProcessActive && (req.Revision != nil || last == entities.ApprovalRevise):
		req.Status = entities.ApprovalSentBack
	case instance.Status == entities.ProcessActive || instance.Status == entities.ProcessSuspended:
		req.Status = entities.ApprovalInProgress
	case instance.Status == entities.ProcessCompleted && last == entities.ApprovalApprove:
		req.Status = entities.ApprovalApproved
	case instance.Status == entities.ProcessCompleted && last == entities.ApprovalReject:
		req.Status = entities.ApprovalRejected
	case instance.Status == entities.ProcessCompleted && last == entities.ApprovalWithdraw:
		req.Status = entities.ApprovalWithdrawn
	default:
		req.Status = entities.ApprovalStopped
	}
	return req
}

func waitingOn(t entities.Task) *entities.ApprovalWaiting {
	w := &entities.ApprovalWaiting{TaskID: t.ID, Assignee: t.AssigneeUsername(), Since: t.CreatedAt}
	for _, u := range t.CandidateUsers {
		if u != nil {
			w.Users = append(w.Users, u.Username)
		}
	}
	for _, g := range t.CandidateGroups {
		if g != nil {
			w.Groups = append(w.Groups, approvalGroupName(g))
		}
	}
	return w
}

// approvalActions are what may be decided on the node of a line.
func approvalActions(line entities.ApprovalLine, node string) []string {
	if node == approvalReviseNode && line.AllowRevise {
		return []string{entities.ApprovalResubmit, entities.ApprovalWithdraw}
	}
	if !strings.HasPrefix(node, approvalStepPrefix) {
		return nil
	}
	if line.AllowRevise {
		return []string{entities.ApprovalApprove, entities.ApprovalReject, entities.ApprovalRevise}
	}
	return []string{entities.ApprovalApprove, entities.ApprovalReject}
}

func (s *approvalLineService) definitionOf(ctx context.Context, instance entities.ProcessInstance) (*entities.ProcessDefinition, error) {
	if instance.Definition == nil {
		return nil, fmt.Errorf("instance %s names no definition", instance.ID)
	}
	if len(instance.Definition.Nodes) > 0 {
		return instance.Definition, nil
	}
	return s.defs.GetDefinition(ctx, instance.Definition.ID)
}

// latest is the highest version of key; live is the one new requests start
// on, which a scheduled release can make an older one.
func (s *approvalLineService) latest(ctx context.Context, projectID uuid.UUID, key string) (*entities.ProcessDefinition, bool, error) {
	defs, err := s.defs.ListDefinitions(ctx, projectID)
	if err != nil {
		return nil, false, err
	}
	var best *entities.ProcessDefinition
	for _, d := range defs {
		if d != nil && d.Key == key && (best == nil || d.Version > best.Version) {
			best = d
		}
	}
	return best, best != nil, nil
}

func (s *approvalLineService) live(ctx context.Context, projectID uuid.UUID, key string) (*entities.ProcessDefinition, bool, error) {
	defs, err := s.defs.ListDefinitions(ctx, projectID)
	if err != nil {
		return nil, false, err
	}
	versions, err := s.defs.ListLiveVersions(ctx, projectID)
	if err != nil {
		return nil, false, err
	}
	var chosen *entities.ProcessDefinition
	for _, d := range defs {
		if d == nil || d.Key != key {
			continue
		}
		if v, pinned := versions[key]; pinned {
			if d.Version == v {
				chosen = d
			}
			continue
		}
		if chosen == nil || d.Version > chosen.Version {
			chosen = d
		}
	}
	if chosen == nil {
		return nil, false, nil
	}
	// The list may carry definitions without their graph.
	if len(chosen.Nodes) == 0 {
		full, err := s.defs.GetDefinition(ctx, chosen.ID)
		if err != nil {
			return nil, false, err
		}
		chosen = full
	}
	return chosen, true, nil
}

func isApprovalVariable(name string) bool {
	switch name {
	case entities.ApprovalVarDecision, entities.ApprovalVarTrail, entities.ApprovalVarRound, entities.ApprovalVarSubmittedBy:
		return true
	}
	return false
}

// refuseApprovalVariables refuses a caller setting what the line keeps: who
// submitted, what was decided and by whom are metis's to write.
func refuseApprovalVariables(vars map[string]any) error {
	for k := range vars {
		if isApprovalVariable(k) {
			return apierr.Invalidf("%s is kept by the approval line and cannot be set", k)
		}
	}
	return nil
}

// approvalTrailOf reads the trail from the variables, which hold it as JSON
// values: a list of maps once it has been stored.
func approvalTrailOf(vars map[string]any) []entities.ApprovalDecision {
	raw, ok := vars[entities.ApprovalVarTrail]
	if !ok || raw == nil {
		return nil
	}
	data, err := json.Marshal(raw)
	if err != nil {
		return nil
	}
	var trail []entities.ApprovalDecision
	if err := json.Unmarshal(data, &trail); err != nil {
		return nil
	}
	return trail
}

// trailVariable is the trail as a variable holds it: plain JSON values, so it
// reads back the same whether or not it went through the database.
func trailVariable(trail []entities.ApprovalDecision) []any {
	data, err := json.Marshal(trail)
	if err != nil {
		return []any{}
	}
	var out []any
	if err := json.Unmarshal(data, &out); err != nil {
		return []any{}
	}
	return out
}

func intVariable(raw any, fallback int) int {
	switch v := raw.(type) {
	case int:
		return v
	case int64:
		return int(v)
	case float64:
		return int(v)
	case json.Number:
		if n, err := v.Int64(); err == nil {
			return int(n)
		}
	}
	return fallback
}
