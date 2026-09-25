package impl

import (
	"context"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/rs/zerolog/log"

	"github.com/gsoultan/metis/internal/pkg/apierr"
	"github.com/gsoultan/metis/server/domains/adapters"
	"github.com/gsoultan/metis/server/domains/entities"
	servicecontracts "github.com/gsoultan/metis/server/domains/services/contracts"
	"github.com/gsoultan/metis/server/repositories"
	repocontracts "github.com/gsoultan/metis/server/repositories/contracts"
	"github.com/gsoultan/metis/server/repositories/models"

	"github.com/google/uuid"
)

type taskService struct {
	repo        repositories.Repository
	engine      servicecontracts.ExecutionEngine
	auditWriter servicecontracts.AuditWriter
}

func NewTaskService(
	repo repositories.Repository,
	engine servicecontracts.ExecutionEngine,
	auditWriter servicecontracts.AuditWriter,
) servicecontracts.TaskService {
	return &taskService{
		repo:        repo,
		engine:      engine,
		auditWriter: auditWriter,
	}
}

func (s *taskService) GetTask(ctx context.Context, id uuid.UUID) (entities.Task, error) {
	m, err := s.repo.Task().Get(ctx, id)
	if err != nil {
		return entities.Task{}, err
	}
	return adapters.TaskEntityAdapter{Model: m}.ToEntity(), nil
}

func (s *taskService) ListTasks(ctx context.Context, projectID uuid.UUID) ([]entities.Task, error) {
	var ms []models.TaskModel
	var err error
	if projectID != uuid.Nil {
		ms, err = s.repo.Task().ListByProject(ctx, projectID)
	} else {
		ms, err = s.repo.Task().List(ctx)
	}
	if err != nil {
		return nil, err
	}
	res := make([]entities.Task, len(ms))
	for i, m := range ms {
		res[i] = adapters.TaskEntityAdapter{Model: m}.ToEntity()
	}
	return res, nil
}

func (s *taskService) ListTasksByAssignee(ctx context.Context, assignee string) ([]entities.Task, error) {
	ms, err := s.repo.Task().ListByAssignee(ctx, assignee)
	if err != nil {
		return nil, err
	}
	res := make([]entities.Task, len(ms))
	for i, m := range ms {
		res[i] = adapters.TaskEntityAdapter{Model: m}.ToEntity()
	}
	return res, nil
}

func (s *taskService) ListTasksByCandidates(ctx context.Context, userID string, groups []string) ([]entities.Task, error) {
	ms, err := s.repo.Task().ListByCandidates(ctx, userID, groups)
	if err != nil {
		return nil, err
	}
	res := make([]entities.Task, len(ms))
	for i, m := range ms {
		res[i] = adapters.TaskEntityAdapter{Model: m}.ToEntity()
	}
	return res, nil
}

func (s *taskService) ClaimTask(ctx context.Context, id uuid.UUID, userID string) error {
	return s.repo.UnitOfWork().Do(ctx, func(txCtx context.Context) error {
		// Holding the task's row. A claim read the task, saw it unclaimed and
		// wrote it claimed with nothing held in between, so claims made at the
		// same moment all read it unclaimed and were all told it was theirs.
		m, err := s.lockedTask(txCtx, id)
		if err != nil {
			return err
		}
		task := adapters.TaskEntityAdapter{Model: m}.ToEntity()
		if task.Status == entities.TaskClaimed || task.Status == entities.TaskDelegated {
			return apierr.Invalidf("somebody else has already claimed this task")
		}
		if task.Status != entities.TaskUnclaimed {
			return apierr.Invalidf("this task is %s; there is nothing to claim", task.Status)
		}

		if err := s.authorizeCandidate(txCtx, task, userID); err != nil {
			return err
		}
		// Refused at the point somebody picks the work up, not only when they
		// try to finish it: letting them claim a task they can never complete
		// is a queue item that looks taken and is not.
		if err := s.enforceSeparationOfDuties(txCtx, m, userID); err != nil {
			return err
		}

		task.Status = entities.TaskClaimed
		task.Assignee = &entities.User{Username: userID}
		if err := s.repo.Task().Update(txCtx, adapters.TaskModelAdapter{Task: task}.ToModel()); err != nil {
			return fmt.Errorf("failed to update task: %w", err)
		}

		s.announce(txCtx, entities.ProcessEvent{
			Type:      entities.EventTaskClaimed,
			Instance:  task.Instance,
			Project:   task.Project,
			Node:      namedNode(task),
			Timestamp: time.Now().Unix(),
			Variables: map[string]any{"assignee": userID},
		}, task, EventTaskClaimed, userID)
		return nil
	})
}

// ErrTaskForbidden is returned when a caller is not permitted to act on a task.
// ErrTaskForbidden wraps apierr.ErrForbidden so the transport answers 403: the
// task exists and the caller may know it does; what they lack is the right.
var ErrTaskForbidden = fmt.Errorf("%w: task: caller is not permitted to act on this task", apierr.ErrForbidden)

// authorizeCandidate reports whether userID may claim or complete an
// unassigned task.
//
// The rule is "absent constraint means deny", with one deliberate exception:
//
//   - No candidate users AND no candidate groups → the task is genuinely open
//     (valid BPMN: an unassigned task with no restriction). Anyone may take it.
//   - Otherwise the caller must appear in CandidateUsers, or belong to one of
//     CandidateGroups.
//
// Two bugs are closed here. Previously the check seeded `isCandidate` with
// `len(task.CandidateUsers) == 0`, so a task restricted purely by group — the
// normal enterprise routing pattern — had an empty user list and was claimable
// by anyone. And CandidateGroups was never consulted at all, so a group
// restriction was presentational only.
//
// Group membership is resolved from the database rather than taken from the
// request, so a caller cannot grant themselves a group by asserting it.
func (s *taskService) authorizeCandidate(ctx context.Context, task entities.Task, userID string) error {
	if len(task.CandidateUsers) == 0 && len(task.CandidateGroups) == 0 {
		return nil
	}

	for _, u := range task.CandidateUsers {
		if u != nil && u.Username == userID {
			return nil
		}
	}

	if len(task.CandidateGroups) == 0 {
		return fmt.Errorf("%w: user %s is not a candidate for task %s", ErrTaskForbidden, userID, task.ID)
	}

	userModel, err := s.repo.User().GetByUsername(ctx, userID)
	if err != nil {
		return fmt.Errorf("%w: cannot resolve caller %s: %w", ErrTaskForbidden, userID, err)
	}
	groups, err := s.repo.Group().ListUserGroups(ctx, uuid.UUID(userModel.ID))
	if err != nil {
		return fmt.Errorf("resolve group membership for %s: %w", userID, err)
	}

	memberOf := make(map[string]struct{}, len(groups))
	for _, g := range groups {
		memberOf[g.Name] = struct{}{}
		memberOf[g.ID.String()] = struct{}{}
	}
	for _, cg := range task.CandidateGroups {
		if cg == nil {
			continue
		}
		if _, ok := memberOf[cg.Name]; ok {
			return nil
		}
		if _, ok := memberOf[cg.ID.String()]; ok {
			return nil
		}
	}

	return fmt.Errorf("%w: user %s is not a candidate for task %s", ErrTaskForbidden, userID, task.ID)
}

func (s *taskService) UnclaimTask(ctx context.Context, id uuid.UUID) error {
	return s.repo.UnitOfWork().Do(ctx, func(txCtx context.Context) error {
		// Held, like every other write to a task. A release that read the
		// task while it was being completed waited for the completion's
		// commit and then wrote its own copy back — "unclaimed" over
		// "completed" — and the finished task was open again.
		m, err := s.lockedTask(txCtx, id)
		if err != nil {
			return err
		}
		task := adapters.TaskEntityAdapter{Model: m}.ToEntity()
		if task.Status != entities.TaskClaimed {
			return fmt.Errorf("task %s is not claimed", id)
		}
		task.Status = entities.TaskUnclaimed
		task.Assignee = nil
		if err := s.repo.Task().Update(txCtx, adapters.TaskModelAdapter{Task: task}.ToModel()); err != nil {
			return fmt.Errorf("failed to update task: %w", err)
		}

		s.announce(txCtx, entities.ProcessEvent{
			Type:      entities.EventTaskUpdated,
			Instance:  task.Instance,
			Project:   task.Project,
			Node:      namedNode(task),
			Timestamp: time.Now().Unix(),
			Variables: task.Variables,
		}, task, EventTaskUnclaimed, "")
		return nil
	})
}

func (s *taskService) DelegateTask(ctx context.Context, id uuid.UUID, userID string) error {
	return s.repo.UnitOfWork().Do(ctx, func(txCtx context.Context) error {
		task, err := s.openTaskForHandOver(txCtx, id, "delegated")
		if err != nil {
			return err
		}
		task.Status = entities.TaskDelegated
		task.Assignee = &entities.User{Username: userID}
		if err := s.repo.Task().Update(txCtx, adapters.TaskModelAdapter{Task: task}.ToModel()); err != nil {
			return fmt.Errorf("failed to update task: %w", err)
		}

		s.announce(txCtx, entities.ProcessEvent{
			Type:      entities.EventTaskUpdated,
			Instance:  task.Instance,
			Project:   task.Project,
			Node:      namedNode(task),
			Timestamp: time.Now().Unix(),
			Variables: task.Variables,
		}, task, EventTaskDelegated, userID)
		return nil
	})
}

func (s *taskService) CompleteTask(ctx context.Context, id uuid.UUID, userID string, vars map[string]any) error {
	return s.repo.UnitOfWork().Do(ctx, func(txCtx context.Context) error {
		m, err := s.repo.Task().Get(txCtx, id)
		if err != nil {
			return fmt.Errorf("failed to get task %s: %w", id, err)
		}

		// May this caller act on this task, as it stands right now?
		//
		// An assigned task may only be completed by its assignee. An unassigned
		// task falls back to the same candidate check as claiming.
		//
		// The previous guard was `task.Assignee != nil && ...`, so a nil
		// assignee skipped authorization entirely — and CreateTaskForNode
		// leaves Assignee nil for every task routed by candidate user or group.
		// Any authenticated user could therefore complete any unclaimed task in
		// any project and inject arbitrary variables into the instance.
		authorize := func(task entities.Task) error {
			if task.Status == entities.TaskCompleted {
				return fmt.Errorf("task %s is already completed", id)
			}
			if task.Status == entities.TaskCanceled {
				return fmt.Errorf("%w: task %s was cancelled and cannot be completed", ErrTaskForbidden, id)
			}
			if task.Assignee != nil {
				if task.Assignee.Username != userID {
					return fmt.Errorf("%w: task %s is assigned to %s, not %s", ErrTaskForbidden, id, task.Assignee.Username, userID)
				}
				return nil
			}
			return s.authorizeCandidate(txCtx, task, userID)
		}

		// Decided before the instance is touched, so a caller with no business
		// here is told that and nothing else — "forbidden" and "no such
		// instance" are different answers and only one of them is theirs.
		if err := authorize(adapters.TaskEntityAdapter{Model: m}.ToEntity()); err != nil {
			return err
		}
		if err := s.enforceSeparationOfDuties(txCtx, m, userID); err != nil {
			return err
		}

		// Then hold the instance and ask again.
		//
		// The lock used to be taken further down, after the check and after the
		// status write. That left a window: a migration running at the same
		// moment re-points this task onto a different node and re-derives who
		// may do it, so a completion that had already read the old row would
		// authorise against an assignee the task no longer has and complete a
		// step the instance is no longer on. Taking the lock makes the two
		// serialise; re-reading and re-checking is what makes the second one
		// see what the first did.
		locked, err := s.engine.GetInstanceForUpdate(txCtx, uuid.UUID(m.InstanceID))
		if err != nil {
			return err
		}
		if m, err = s.repo.Task().Get(txCtx, id); err != nil {
			return fmt.Errorf("failed to re-read task %s: %w", id, err)
		}
		task := adapters.TaskEntityAdapter{Model: m}.ToEntity()
		if err := authorize(task); err != nil {
			return err
		}

		// Who did it, recorded on the task itself.
		//
		// A task routed by candidate group is completed with a nil assignee, so
		// the row said a step had been performed and not by whom. That is the
		// answer a segregation-of-duties rule needs — and the one an auditor
		// asks for first.
		m.Status = models.TaskStatus(entities.TaskCompleted)
		if m.Assignee == "" {
			m.Assignee = userID
		}
		if err := s.repo.Task().Update(txCtx, m); err != nil {
			return fmt.Errorf("failed to update task status: %w", err)
		}

		instance := locked

		if _, err := s.repo.Definition().Get(txCtx, instance.Definition.ID); err != nil {
			return err
		}

		for k, v := range vars {
			instance.SetVariable(k, v)
		}

		s.announce(txCtx, entities.ProcessEvent{
			Type:      entities.EventTaskCompleted,
			Instance:  &instance,
			Project:   instance.Project,
			Node:      namedNode(task),
			Timestamp: time.Now().Unix(),
			Variables: vars,
		}, task, EventTaskCompleted, userID)

		fullDef, err := s.engine.GetProcessDefinition(txCtx, instance.Definition.ID)
		if err != nil {
			return fmt.Errorf("failed to load definition %s: %w", instance.Definition.ID, err)
		}

		return s.engine.Proceed(txCtx, &instance, fullDef, task.NodeID())
	})
}

func (s *taskService) CreateTaskForNode(ctx context.Context, instance entities.ProcessInstance, node entities.Node) error {
	return s.repo.UnitOfWork().Do(ctx, func(txCtx context.Context) error {
		idObj, err := uuid.NewV7()
		if err != nil {
			return fmt.Errorf("could not generate an id for the %q task: %w", node.Name, err)
		}
		status := entities.TaskUnclaimed
		var assignee *entities.User
		if node.Assignee != "" {
			status = entities.TaskClaimed
			assignee = &entities.User{Username: node.Assignee}
		}

		candidateUsers := node.CandidateUsers
		candidateGroups := node.CandidateGroups

		dueDate := entities.ResolveDueDate(node.DueDate, time.Now())

		task := entities.Task{
			ID:              idObj,
			Project:         instance.Project,
			Instance:        &instance,
			Node:            &node,
			Name:            node.Name,
			Description:     node.Documentation,
			Type:            node.Type,
			Status:          status,
			Assignee:        assignee,
			CandidateUsers:  candidateUsers,
			CandidateGroups: candidateGroups,
			Priority:        node.Priority,
			DueDate:         dueDate,
			FormKey:         node.FormKey,
			FormDefinition:  node.GetTextProperty("form_definition"),
			Variables:       instance.Variables,
			CreatedAt:       time.Now(),
		}

		if err := s.repo.Task().Create(txCtx, adapters.TaskModelAdapter{Task: task}.ToModel()); err != nil {
			return err
		}

		s.announce(txCtx, entities.ProcessEvent{
			Type:      entities.EventTaskCreated,
			Instance:  &instance,
			Project:   instance.Project,
			Node:      &node,
			Timestamp: time.Now().Unix(),
			Variables: instance.Variables,
		}, task, EventTaskCreated, "")
		return nil
	})
}

func (s *taskService) UpdateTask(ctx context.Context, task entities.Task) error {
	return s.repo.UnitOfWork().Do(ctx, func(txCtx context.Context) error {
		// Held for the reason UnclaimTask holds it: the whole row is written
		// back, status included, so an unheld read could reopen a task
		// completed in between.
		m, err := s.lockedTask(txCtx, task.ID)
		if err != nil {
			return err
		}
		existing := adapters.TaskEntityAdapter{Model: m}.ToEntity()
		// UpdateConnectorInstance specific allowed fields
		existing.Name = task.Name
		existing.Priority = task.Priority
		existing.DueDate = task.DueDate

		if err := s.repo.Task().Update(txCtx, adapters.TaskModelAdapter{Task: existing}.ToModel()); err != nil {
			return err
		}

		s.engine.DispatchEvent(txCtx, entities.ProcessEvent{
			Type:      entities.EventTaskUpdated,
			Instance:  existing.Instance,
			Project:   existing.Project,
			Node:      existing.Node,
			Timestamp: time.Now().Unix(),
			Variables: existing.Variables,
		})

		return nil
	})
}

func (s *taskService) AssignTask(ctx context.Context, id uuid.UUID, userID string) error {
	return s.repo.UnitOfWork().Do(ctx, func(txCtx context.Context) error {
		task, err := s.openTaskForHandOver(txCtx, id, "assigned")
		if err != nil {
			return err
		}
		task.Assignee = &entities.User{Username: userID}
		task.Status = entities.TaskClaimed
		if err := s.repo.Task().Update(txCtx, adapters.TaskModelAdapter{Task: task}.ToModel()); err != nil {
			return err
		}

		s.announce(txCtx, entities.ProcessEvent{
			Type:      entities.EventTaskClaimed,
			Instance:  task.Instance,
			Project:   task.Project,
			Node:      namedNode(task),
			Timestamp: time.Now().Unix(),
			Variables: map[string]any{"assignee": userID},
		}, task, EventTaskAssigned, userID)
		return nil
	})
}

// openTaskForHandOver reads a task about to be delegated or assigned, holding
// its row, and refuses one nobody can work on any more.
//
// Both hand-overs set the status without asking what it was, so a completed
// task could be handed on — reopened — and completed again, running
// everything after it a second time. Holding the row matters as much as the
// check: completion writes it too, so a hand-over that read the task open
// while it was being completed would otherwise write it back open.
func (s *taskService) openTaskForHandOver(ctx context.Context, id uuid.UUID, action string) (entities.Task, error) {
	m, err := s.lockedTask(ctx, id)
	if err != nil {
		return entities.Task{}, err
	}
	task := adapters.TaskEntityAdapter{Model: m}.ToEntity()
	switch task.Status {
	case entities.TaskCompleted:
		return entities.Task{}, apierr.Invalidf("this task is completed; it cannot be %s", action)
	case entities.TaskCanceled:
		return entities.Task{}, apierr.Invalidf("this task was withdrawn; it cannot be %s", action)
	}
	return task, nil
}

// lockedTask reads a task and holds its row, so what is decided from the read
// cannot be overtaken by another decision made from the same read. Everything
// that competes for a task — a claim, a hand-over, completion, a migration
// moving it — writes that row, so holding it is what makes them take turns.
func (s *taskService) lockedTask(ctx context.Context, id uuid.UUID) (models.TaskModel, error) {
	m, err := s.repo.Task().GetForUpdate(ctx, id)
	if err != nil {
		return models.TaskModel{}, fmt.Errorf("failed to get task: %w", err)
	}
	return m, nil
}

// announce raises a task event and writes the task's audit entry for it.
//
// Together, because the entry is written here — with who acted, which the
// event does not carry — and the audit observer would otherwise write its own
// from the event, so every task action was in the trail twice. The event says
// it has been audited only when there is a writer to have done it.
func (s *taskService) announce(ctx context.Context, event entities.ProcessEvent, task entities.Task, auditType, actor string) {
	event.Audited = s.auditWriter != nil
	s.engine.DispatchEvent(ctx, event)
	s.recordAuditEvent(ctx, task, auditType, actor)
}

// recordAuditEvent writes a Business Timeline narrative audit entry for a task
// lifecycle event. Errors are intentionally swallowed so audit failures never
// affect the primary operation outcome.
// namedNode returns the task's node with the task's own name filled in when
// the node carries none.
//
// A task loaded from storage references its node by ID alone, so every
// lifecycle event shipped a nameless node — and both narrative writers
// degraded to their fallbacks. The timeline read `admin claimed task
// "unknown"` and `admin has started working on 'the task'` about a task whose
// name was sitting in the same struct the whole time.
func namedNode(task entities.Task) *entities.Node {
	if task.Node == nil {
		if task.Name == "" {
			return nil
		}
		return &entities.Node{Name: task.Name}
	}
	if task.Node.Name != "" || task.Name == "" {
		return task.Node
	}
	named := *task.Node
	named.Name = task.Name
	return &named
}

func (s *taskService) recordAuditEvent(ctx context.Context, task entities.Task, eventType, actor string) {
	if s.auditWriter == nil {
		return
	}
	if err := s.auditWriter.RecordEvent(ctx, entities.AuditEntry{
		Type:     eventType,
		Project:  task.Project,
		Instance: task.Instance,
		Node:     namedNode(task),
		Data:     map[string]any{"actor": actor},
	}); err != nil {
		// Who did what to a task is exactly what an audit is asked for later.
		log.Error().Err(err).Str("event", eventType).Str("actor", actor).
			Msg("A task audit event was lost; the trail is incomplete from here")
	}
}

// ListTasksByAssigneePaged returns one page of a user's tasks with the total.
//
// The mapping from models to entities happens per page rather than per result
// set, which is the point: the previous unpaged call adapted every row a user
// had ever been assigned in order to render fifty of them.
func (s *taskService) ListTasksByAssigneePaged(ctx context.Context, assignee string, page repocontracts.Pagination) (repocontracts.Page[entities.Task], error) {
	result, err := s.repo.Task().ListByAssigneePaged(ctx, assignee, page)
	if err != nil {
		return repocontracts.Page[entities.Task]{}, err
	}

	tasks := make([]entities.Task, len(result.Items))
	for i, m := range result.Items {
		tasks[i] = adapters.TaskEntityAdapter{Model: m}.ToEntity()
	}
	return repocontracts.NewPage(tasks, result.Total, page), nil
}

// ListTasksByCandidatesPaged returns one page of the unclaimed tasks a user
// could take, with the total.
func (s *taskService) ListTasksByCandidatesPaged(ctx context.Context, userID string, groups []string, page repocontracts.Pagination) (repocontracts.Page[entities.Task], error) {
	result, err := s.repo.Task().ListByCandidatesPaged(ctx, userID, groups, page)
	if err != nil {
		return repocontracts.Page[entities.Task]{}, err
	}

	tasks := make([]entities.Task, len(result.Items))
	for i, m := range result.Items {
		tasks[i] = adapters.TaskEntityAdapter{Model: m}.ToEntity()
	}
	return repocontracts.NewPage(tasks, result.Total, page), nil
}

// ListTasksPaged returns one page of a project's tasks, or of the tenant's
// tasks when no project is selected.
// ListTasksByInstancePaged returns one window of a single instance's tasks.
//
// Separate from ListTasksPaged rather than another argument to it: an instance
// belongs to exactly one project, so naming an instance already answers the
// project question, and a call taking both would have to define what a
// mismatched pair means.
func (s *taskService) ListTasksByInstancePaged(ctx context.Context, instanceID uuid.UUID, page repocontracts.Pagination) (repocontracts.Page[entities.Task], error) {
	result, err := s.repo.Task().ListByInstancePaged(ctx, instanceID, page)
	if err != nil {
		return repocontracts.Page[entities.Task]{}, err
	}

	tasks := make([]entities.Task, len(result.Items))
	for i, m := range result.Items {
		tasks[i] = adapters.TaskEntityAdapter{Model: m}.ToEntity()
	}
	return repocontracts.NewPage(tasks, result.Total, page), nil
}

func (s *taskService) ListTasksPaged(ctx context.Context, projectID uuid.UUID, page repocontracts.Pagination) (repocontracts.Page[entities.Task], error) {
	var result repocontracts.Page[models.TaskModel]
	var err error
	if projectID != uuid.Nil {
		result, err = s.repo.Task().ListByProjectPaged(ctx, projectID, page)
	} else {
		result, err = s.repo.Task().ListPaged(ctx, page)
	}
	if err != nil {
		return repocontracts.Page[entities.Task]{}, err
	}

	tasks := make([]entities.Task, len(result.Items))
	for i, m := range result.Items {
		tasks[i] = adapters.TaskEntityAdapter{Model: m}.ToEntity()
	}
	return repocontracts.NewPage(tasks, result.Total, page), nil
}

// SeparationOfDutiesKey is the node property naming the steps whose performer
// must not also perform this one.
//
// A list of node ids, comma-separated. "The person who requested it cannot be
// the person who approves it" is the oldest control there is, and a process
// graph cannot express it: the graph says a supervisor approves, not that the
// supervisor is somebody else.
const SeparationOfDutiesKey = "separation_of_duties"

// enforceSeparationOfDuties refuses work whose conflicting step this person has
// already performed on this instance.
//
// Scoped to the instance on purpose. The control is about one transaction — the
// same person requesting and approving *this* quotation — not about a person
// being permanently barred from a kind of step, which is what roles are for.
//
// A node it names that the instance never performed is not a violation: it may
// have been skipped, or on a branch this instance did not take. The rule is
// "not the same person twice", not "that step must have happened".
func (s *taskService) enforceSeparationOfDuties(ctx context.Context, task models.TaskModel, userID string) error {
	node, err := s.nodeBehind(ctx, task)
	if err != nil || node == nil {
		// A task whose node cannot be read is refused by the caller's own
		// checks; there is nothing to enforce here.
		return nil //nolint:nilerr // absence of a node is not a conflict
	}
	conflicts := splitNodeList(node.GetStringProperty(SeparationOfDutiesKey))
	if len(conflicts) == 0 {
		return nil
	}

	performed, err := s.repo.Task().ListByInstance(ctx, uuid.UUID(task.InstanceID))
	if err != nil {
		return err
	}
	for _, other := range performed {
		if other.Status != models.TaskCompleted || !slices.Contains(conflicts, other.NodeID) {
			continue
		}
		if other.Assignee != userID {
			continue
		}
		return fmt.Errorf("%w: %s already did %q on this instance, and %q may not be done by the same person",
			ErrTaskForbidden, userID, other.NodeID, task.NodeID)
	}
	return nil
}

// nodeBehind reads the definition node a task was created from.
func (s *taskService) nodeBehind(ctx context.Context, task models.TaskModel) (*entities.Node, error) {
	instance, err := s.repo.Process().Get(ctx, uuid.UUID(task.InstanceID))
	if err != nil {
		return nil, err
	}
	def, err := s.engine.GetProcessDefinition(ctx, uuid.UUID(instance.DefinitionID))
	if err != nil || def == nil {
		return nil, err
	}
	node := def.FindNode(task.NodeID)
	return node, nil
}

// splitNodeList reads a comma-separated node id list from a node property.
func splitNodeList(raw string) []string {
	if strings.TrimSpace(raw) == "" {
		return nil
	}
	parts := strings.Split(raw, ",")
	out := make([]string, 0, len(parts))
	for _, part := range parts {
		if trimmed := strings.TrimSpace(part); trimmed != "" {
			out = append(out, trimmed)
		}
	}
	return out
}
