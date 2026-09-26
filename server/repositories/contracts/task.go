package contracts

import (
	"context"
	"time"

	"github.com/google/uuid"
	"github.com/gsoultan/metis/server/repositories/models"
)

// DeadlineRow is one open task with a due date and the process it is part of.
type DeadlineRow struct {
	TaskID      uuid.UUID
	Name        string
	NodeID      string
	Status      string
	Priority    int64
	Assignee    string
	DueDate     time.Time
	ProcessKey  string
	ProcessName string
}

// DeadlineCounts counts a project's open tasks, with a due date and without.
type DeadlineCounts struct {
	WithDeadline    int64
	WithoutDeadline int64
}

// TaskRepository defines the BPM task operations.
type TaskRepository interface {
	Get(ctx context.Context, id uuid.UUID) (models.TaskModel, error)
	// GetForUpdate reads a task and holds its row until the transaction ends.
	GetForUpdate(ctx context.Context, id uuid.UUID) (models.TaskModel, error)
	List(ctx context.Context) ([]models.TaskModel, error)
	ListByProject(ctx context.Context, projectID uuid.UUID) ([]models.TaskModel, error)
	ListByAssignee(ctx context.Context, assignee string) ([]models.TaskModel, error)
	ListByCandidates(ctx context.Context, c Candidacy) ([]models.TaskModel, error)
	ListByInstance(ctx context.Context, instanceID uuid.UUID) ([]models.TaskModel, error)
	// Deadlines reads a project's open tasks with a due date, soonest first
	// and at most limit of them, and counts all its open tasks. Scoped to the
	// caller's tenant like every project read.
	Deadlines(ctx context.Context, projectID uuid.UUID, limit int) ([]DeadlineRow, DeadlineCounts, error)

	// Paged variants for the lists a user browses. The unpaged ones above stay
	// for internal callers — the engine and job worker genuinely need every
	// row and are not driven by a request.
	ListByAssigneePaged(ctx context.Context, assignee string, p Pagination) (Page[models.TaskModel], error)
	ListByProjectPaged(ctx context.Context, projectID uuid.UUID, p Pagination) (Page[models.TaskModel], error)
	ListByInstancePaged(ctx context.Context, instanceID uuid.UUID, p Pagination) (Page[models.TaskModel], error)
	ListByCandidatesPaged(ctx context.Context, c Candidacy, p Pagination) (Page[models.TaskModel], error)
	ListPaged(ctx context.Context, p Pagination) (Page[models.TaskModel], error)
	Update(ctx context.Context, task models.TaskModel) error
	UpdateStatus(ctx context.Context, id uuid.UUID, status models.TaskStatus) error
	Create(ctx context.Context, task models.TaskModel) error
	CountByStatus(ctx context.Context, projectID uuid.UUID, status models.TaskStatus) (int64, error)
}
