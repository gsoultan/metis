package contracts

import (
	"context"

	"github.com/google/uuid"
	"github.com/gsoultan/metis/server/domains/entities"
	repocontracts "github.com/gsoultan/metis/server/repositories/contracts"
)

// TaskService defines the task management operations.
type TaskService interface {
	GetTask(ctx context.Context, id uuid.UUID) (entities.Task, error)
	ListTasks(ctx context.Context, projectID uuid.UUID) ([]entities.Task, error)
	ListTasksPaged(ctx context.Context, projectID uuid.UUID, page repocontracts.Pagination) (repocontracts.Page[entities.Task], error)

	// ListTasksByInstancePaged narrows a listing to one process instance —
	// "what is this run waiting on", which the project-wide listing could only
	// answer by returning everything and letting the caller match.
	ListTasksByInstancePaged(ctx context.Context, instanceID uuid.UUID, page repocontracts.Pagination) (repocontracts.Page[entities.Task], error)

	// ListTasksByAssigneePaged returns one window of a user's tasks plus the
	// total, so a caller can page rather than pulling an unbounded list.
	ListTasksByAssigneePaged(ctx context.Context, assignee string, page repocontracts.Pagination) (repocontracts.Page[entities.Task], error)
	ListTasksByCandidatesPaged(ctx context.Context, userID string, groups []string, page repocontracts.Pagination) (repocontracts.Page[entities.Task], error)
	ClaimTask(ctx context.Context, id uuid.UUID, userID string) error
	UnclaimTask(ctx context.Context, id uuid.UUID) error
	DelegateTask(ctx context.Context, id uuid.UUID, userID string) error
	CompleteTask(ctx context.Context, id uuid.UUID, userID string, vars map[string]any) error
	CreateTaskForNode(ctx context.Context, instance entities.ProcessInstance, node entities.Node) error
	UpdateTask(ctx context.Context, task entities.Task) error
	AssignTask(ctx context.Context, id uuid.UUID, userID string) error
}
