package impl

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/rs/zerolog/log"

	"github.com/gsoultan/gobpm/server/domains/adapters"
	"github.com/gsoultan/gobpm/server/domains/entities"
	servicecontracts "github.com/gsoultan/gobpm/server/domains/services/contracts"
	"github.com/gsoultan/gobpm/server/repositories"
	repocontracts "github.com/gsoultan/gobpm/server/repositories/contracts"
	"github.com/gsoultan/gobpm/server/repositories/models"

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
		m, err := s.repo.Task().Get(txCtx, id)
		if err != nil {
			return fmt.Errorf("failed to get task: %w", err)
		}
		task := adapters.TaskEntityAdapter{Model: m}.ToEntity()
		if task.Status != entities.TaskUnclaimed {
			return fmt.Errorf("task %s is not unclaimed (current status: %s)", id, task.Status)
		}

		if err := s.authorizeCandidate(txCtx, task, userID); err != nil {
			return err
		}

		task.Status = entities.TaskClaimed
		task.Assignee = &entities.User{Username: userID}
		if err := s.repo.Task().Update(txCtx, adapters.TaskModelAdapter{Task: task}.ToModel()); err != nil {
			return fmt.Errorf("failed to update task: %w", err)
		}

		s.engine.DispatchEvent(txCtx, entities.ProcessEvent{
			Type:      entities.EventTaskClaimed,
			Instance:  task.Instance,
			Project:   task.Project,
			Node:      namedNode(task),
			Timestamp: time.Now().Unix(),
			Variables: map[string]any{"assignee": userID},
		})

		s.recordAuditEvent(txCtx, task, EventTaskClaimed, userID)
		return nil
	})
}

// ErrTaskForbidden is returned when a caller is not permitted to act on a task.
var ErrTaskForbidden = errors.New("task: caller is not permitted to act on this task")

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
		m, err := s.repo.Task().Get(txCtx, id)
		if err != nil {
			return fmt.Errorf("failed to get task: %w", err)
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

		s.engine.DispatchEvent(txCtx, entities.ProcessEvent{
			Type:      entities.EventTaskUpdated,
			Instance:  task.Instance,
			Project:   task.Project,
			Node:      namedNode(task),
			Timestamp: time.Now().Unix(),
			Variables: task.Variables,
		})

		s.recordAuditEvent(txCtx, task, EventTaskUnclaimed, "")
		return nil
	})
}

func (s *taskService) DelegateTask(ctx context.Context, id uuid.UUID, userID string) error {
	return s.repo.UnitOfWork().Do(ctx, func(txCtx context.Context) error {
		m, err := s.repo.Task().Get(txCtx, id)
		if err != nil {
			return fmt.Errorf("failed to get task: %w", err)
		}
		task := adapters.TaskEntityAdapter{Model: m}.ToEntity()
		task.Status = entities.TaskDelegated
		task.Assignee = &entities.User{Username: userID}
		if err := s.repo.Task().Update(txCtx, adapters.TaskModelAdapter{Task: task}.ToModel()); err != nil {
			return fmt.Errorf("failed to update task: %w", err)
		}

		s.engine.DispatchEvent(txCtx, entities.ProcessEvent{
			Type:      entities.EventTaskUpdated,
			Instance:  task.Instance,
			Project:   task.Project,
			Node:      namedNode(task),
			Timestamp: time.Now().Unix(),
			Variables: task.Variables,
		})

		s.recordAuditEvent(txCtx, task, EventTaskDelegated, userID)
		return nil
	})
}

func (s *taskService) CompleteTask(ctx context.Context, id uuid.UUID, userID string, vars map[string]any) error {
	return s.repo.UnitOfWork().Do(ctx, func(txCtx context.Context) error {
		m, err := s.repo.Task().Get(txCtx, id)
		if err != nil {
			return fmt.Errorf("failed to get task %s: %w", id, err)
		}
		task := adapters.TaskEntityAdapter{Model: m}.ToEntity()

		if task.Status == entities.TaskCompleted {
			return fmt.Errorf("task %s is already completed", id)
		}

		// An assigned task may only be completed by its assignee. An unassigned
		// task falls back to the same candidate check as claiming.
		//
		// The previous guard was `task.Assignee != nil && ...`, so a nil
		// assignee skipped authorization entirely — and CreateTaskForNode
		// leaves Assignee nil for every task routed by candidate user or group.
		// Any authenticated user could therefore complete any unclaimed task in
		// any project and inject arbitrary variables into the instance.
		if task.Assignee != nil {
			if task.Assignee.Username != userID {
				return fmt.Errorf("%w: task %s is assigned to %s, not %s", ErrTaskForbidden, id, task.Assignee.Username, userID)
			}
		} else if err := s.authorizeCandidate(txCtx, task, userID); err != nil {
			return err
		}

		if err := s.repo.Task().UpdateStatus(txCtx, id, models.TaskStatus(entities.TaskCompleted)); err != nil {
			return fmt.Errorf("failed to update task status: %w", err)
		}

		instance, err := s.engine.GetInstanceForUpdate(txCtx, task.Instance.ID)
		if err != nil {
			return err
		}

		if _, err := s.repo.Definition().Get(txCtx, instance.Definition.ID); err != nil {
			return err
		}

		for k, v := range vars {
			instance.SetVariable(k, v)
		}

		s.engine.DispatchEvent(txCtx, entities.ProcessEvent{
			Type:      entities.EventTaskCompleted,
			Instance:  &instance,
			Project:   instance.Project,
			Node:      namedNode(task),
			Timestamp: time.Now().Unix(),
			Variables: vars,
		})

		s.recordAuditEvent(txCtx, task, EventTaskCompleted, userID)

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

		var dueDate *time.Time
		if node.DueDate != "" {
			if t, err := time.Parse(time.RFC3339, node.DueDate); err == nil {
				dueDate = &t
			}
		}

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
			FormDefinition:  node.GetStringProperty("form_definition"),
			Variables:       instance.Variables,
			CreatedAt:       time.Now(),
		}

		if err := s.repo.Task().Create(txCtx, adapters.TaskModelAdapter{Task: task}.ToModel()); err != nil {
			return err
		}

		s.engine.DispatchEvent(txCtx, entities.ProcessEvent{
			Type:      entities.EventTaskCreated,
			Instance:  &instance,
			Project:   instance.Project,
			Node:      &node,
			Timestamp: time.Now().Unix(),
			Variables: instance.Variables,
		})

		s.recordAuditEvent(txCtx, task, EventTaskCreated, "")
		return nil
	})
}

func (s *taskService) UpdateTask(ctx context.Context, task entities.Task) error {
	return s.repo.UnitOfWork().Do(ctx, func(txCtx context.Context) error {
		// Ensure task exists before updating
		m, err := s.repo.Task().Get(txCtx, task.ID)
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
		m, err := s.repo.Task().Get(txCtx, id)
		if err != nil {
			return err
		}
		task := adapters.TaskEntityAdapter{Model: m}.ToEntity()
		task.Assignee = &entities.User{Username: userID}
		task.Status = entities.TaskClaimed
		if err := s.repo.Task().Update(txCtx, adapters.TaskModelAdapter{Task: task}.ToModel()); err != nil {
			return err
		}

		s.engine.DispatchEvent(txCtx, entities.ProcessEvent{
			Type:      entities.EventTaskClaimed,
			Instance:  task.Instance,
			Project:   task.Project,
			Node:      namedNode(task),
			Timestamp: time.Now().Unix(),
			Variables: map[string]any{"assignee": userID},
		})

		s.recordAuditEvent(txCtx, task, EventTaskAssigned, userID)
		return nil
	})
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
