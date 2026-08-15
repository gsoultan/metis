package gorms

import (
	"context"
	"fmt"
	"github.com/gsoultan/gobpm/server/repositories/contracts"
	"github.com/gsoultan/gobpm/server/repositories/models"

	"github.com/google/uuid"

	"gorm.io/gorm"
)

type gormTaskRepository struct {
	db *gorm.DB
}

// NewTaskRepository creates a new GORM-based TaskRepository.
func NewTaskRepository(db *gorm.DB) contracts.TaskRepository {
	return &gormTaskRepository{db: db}
}

func (r *gormTaskRepository) Get(ctx context.Context, id uuid.UUID) (models.TaskModel, error) {
	var m models.TaskModel
	if err := GetTx(ctx, r.db).First(&m, QueryByID, id).Error; err != nil {
		return models.TaskModel{}, fmt.Errorf("could not get task: %w", err)
	}
	return m, nil
}

func (r *gormTaskRepository) List(ctx context.Context) ([]models.TaskModel, error) {
	var modelsList []models.TaskModel
	db := tenantScopeDB(ctx, GetTx(ctx, r.db), "tasks")
	if err := db.Find(&modelsList).Error; err != nil {
		return nil, fmt.Errorf("could not list tasks: %w", err)
	}
	return modelsList, nil
}

func (r *gormTaskRepository) ListByProject(ctx context.Context, projectID uuid.UUID) ([]models.TaskModel, error) {
	var modelsList []models.TaskModel
	if err := GetTx(ctx, r.db).Where(QueryByProjectID, projectID).Find(&modelsList).Error; err != nil {
		return nil, fmt.Errorf("could not list tasks by project: %w", err)
	}
	return modelsList, nil
}

func (r *gormTaskRepository) ListByAssignee(ctx context.Context, assignee string) ([]models.TaskModel, error) {
	var modelsList []models.TaskModel
	db := tenantScopeDB(ctx, GetTx(ctx, r.db), "tasks")
	if err := db.Where(QueryByAssignee, assignee).Find(&modelsList).Error; err != nil {
		return nil, fmt.Errorf("could not list tasks by assignee: %w", err)
	}
	return modelsList, nil
}

// candidateQuery builds the "unclaimed and this user could take it" filter.
//
// Extracted so the paged and unpaged variants cannot drift. The predicate is
// subtle — candidate users and groups are stored as JSON arrays and matched
// with LIKE — and two copies of it would eventually disagree about who can see
// which task, which is an authorization difference, not a cosmetic one.
func (r *gormTaskRepository) candidateQuery(ctx context.Context, userID string, groups []string) *gorm.DB {
	query := tenantScopeDB(ctx, GetTx(ctx, r.db), "tasks").
		Model(&models.TaskModel{}).
		Where(QueryByStatus, string(models.TaskUnclaimed))

	// LIKE against the serialised JSON array; the quotes anchor the match to a
	// whole element so "alice" does not match "alice.smith".
	subQuery := QueryByCandidateUser
	args := []any{fmt.Sprintf("%%%q%%", userID)}

	for _, g := range groups {
		subQuery += " OR " + QueryByCandidateGroup
		args = append(args, fmt.Sprintf("%%%q%%", g))
	}

	return query.Where(subQuery, args...)
}

func (r *gormTaskRepository) ListByCandidates(ctx context.Context, userID string, groups []string) ([]models.TaskModel, error) {
	var modelsList []models.TaskModel
	if err := r.candidateQuery(ctx, userID, groups).Find(&modelsList).Error; err != nil {
		return nil, fmt.Errorf("could not list tasks by candidates: %w", err)
	}
	return modelsList, nil
}

// ListByCandidatesPaged returns one page of the unclaimed tasks a user could take.
func (r *gormTaskRepository) ListByCandidatesPaged(ctx context.Context, userID string, groups []string, p contracts.Pagination) (contracts.Page[models.TaskModel], error) {
	return countAndPage[models.TaskModel](r.candidateQuery(ctx, userID, groups), p, "tasks.created_at DESC")
}

func (r *gormTaskRepository) ListByInstance(ctx context.Context, instanceID uuid.UUID) ([]models.TaskModel, error) {
	var modelsList []models.TaskModel
	if err := GetTx(ctx, r.db).Where(QueryByInstanceID, instanceID).Find(&modelsList).Error; err != nil {
		return nil, fmt.Errorf("could not list tasks by instance: %w", err)
	}
	return modelsList, nil
}

func (r *gormTaskRepository) ListWithFilters(ctx context.Context, filter contracts.TaskFilter) ([]models.TaskModel, error) {
	var modelsList []models.TaskModel
	query := GetTx(ctx, r.db)

	if filter.ProjectID != nil {
		query = query.Where(QueryByProjectID, *filter.ProjectID)
	}
	if len(filter.Status) > 0 {
		statusStrings := make([]string, len(filter.Status))
		for i, s := range filter.Status {
			statusStrings[i] = string(s)
		}
		query = query.Where(QueryByStatus+" IN ?", statusStrings)
	}
	if filter.Assignee != nil {
		query = query.Where(QueryByAssignee, *filter.Assignee)
	}
	if filter.Priority != nil {
		query = query.Where(QueryByPriority, *filter.Priority)
	}

	if err := query.Find(&modelsList).Error; err != nil {
		return nil, fmt.Errorf("could not list tasks with filters: %w", err)
	}

	return modelsList, nil
}

func (r *gormTaskRepository) Update(ctx context.Context, t models.TaskModel) error {
	if err := GetTx(ctx, r.db).Save(&t).Error; err != nil {
		return fmt.Errorf("could not update task: %w", err)
	}
	return nil
}

func (r *gormTaskRepository) UpdateStatus(ctx context.Context, id uuid.UUID, status models.TaskStatus) error {
	result := GetTx(ctx, r.db).Model(&models.TaskModel{}).Where(QueryByID, id).Update(UpdateFieldStatus, status)
	if result.Error != nil {
		return fmt.Errorf("could not update task status: %w", result.Error)
	}
	if result.RowsAffected == 0 {
		return fmt.Errorf("task not found: %s", id)
	}
	return nil
}

func (r *gormTaskRepository) Create(ctx context.Context, t models.TaskModel) error {
	if err := GetTx(ctx, r.db).Create(&t).Error; err != nil {
		return fmt.Errorf("could not create task: %w", err)
	}
	return nil
}

func (r *gormTaskRepository) CountByStatus(ctx context.Context, projectID uuid.UUID, status models.TaskStatus) (int64, error) {
	var count int64
	query := GetTx(ctx, r.db).Model(&models.TaskModel{})
	if status != "" {
		query = query.Where("status = ?", status)
	}
	if projectID != uuid.Nil {
		query = query.Where(QueryByProjectID, projectID)
	}
	if err := query.Count(&count).Error; err != nil {
		return 0, fmt.Errorf("could not count tasks: %w", err)
	}
	return count, nil
}

// countAndPage runs the same query twice: once for the total, once for the
// requested window. Two round trips is the cost of being able to say
// "1–50 of 1,234"; without the count a caller can only guess whether another
// page exists.
//
// Each runs on its own cloned session. GORM's Count ignores LIMIT and OFFSET,
// so the total is correct either way, but sharing one *gorm.DB across two
// terminal calls lets conditions from the first leak into the second — the
// clone keeps the two independent rather than relying on that.
// order must qualify its column with the table name. tenantScopeDB joins
// projects, which carries created_at too, so a bare "created_at DESC" is
// ambiguous the moment tenant scoping is active — and it is active for every
// request-driven call.
func countAndPage[T any](base *gorm.DB, p contracts.Pagination, order string) (contracts.Page[T], error) {
	var total int64
	if err := base.Session(&gorm.Session{}).Count(&total).Error; err != nil {
		return contracts.Page[T]{}, fmt.Errorf("count: %w", err)
	}

	var rows []T
	q := base.Session(&gorm.Session{})
	if order != "" {
		q = q.Order(order)
	}
	if err := p.Apply(q).Find(&rows).Error; err != nil {
		return contracts.Page[T]{}, fmt.Errorf("page: %w", err)
	}
	return contracts.NewPage(rows, total, p), nil
}

// ListByAssigneePaged returns one page of the tasks assigned to a user.
func (r *gormTaskRepository) ListByAssigneePaged(ctx context.Context, assignee string, p contracts.Pagination) (contracts.Page[models.TaskModel], error) {
	base := tenantScopeDB(ctx, GetTx(ctx, r.db), "tasks").
		Model(&models.TaskModel{}).
		Where(QueryByAssignee, assignee)
	return countAndPage[models.TaskModel](base, p, "tasks.created_at DESC")
}

// ListByProjectPaged returns one page of a project's tasks.
func (r *gormTaskRepository) ListByProjectPaged(ctx context.Context, projectID uuid.UUID, p contracts.Pagination) (contracts.Page[models.TaskModel], error) {
	base := tenantScopeDB(ctx, GetTx(ctx, r.db), "tasks").
		Model(&models.TaskModel{}).
		Where(QueryByProjectID, projectID)
	return countAndPage[models.TaskModel](base, p, "tasks.created_at DESC")
}

// ListPaged returns one page of tasks across the active tenant.
func (r *gormTaskRepository) ListPaged(ctx context.Context, p contracts.Pagination) (contracts.Page[models.TaskModel], error) {
	base := tenantScopeDB(ctx, GetTx(ctx, r.db), "tasks").Model(&models.TaskModel{})
	return countAndPage[models.TaskModel](base, p, "tasks.created_at DESC")
}
