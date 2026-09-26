package project_test

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/gsoultan/metis/server/domains/entities"
	observersimpl "github.com/gsoultan/metis/server/domains/observers/impl"
	"github.com/gsoultan/metis/server/domains/services"
	serviceimpl "github.com/gsoultan/metis/server/domains/services/impl"
	"github.com/gsoultan/metis/server/repositories"
	"github.com/gsoultan/metis/server/repositories/contracts"
	"github.com/gsoultan/metis/server/repositories/models"
	"github.com/gsoultan/metis/tests/testutils"
	"gorm.io/gorm"
)

// auditSpy counts reads of a project's whole audit trail.
type auditSpy struct {
	contracts.AuditRepository
	projectReads int
}

func (s *auditSpy) ListByProject(ctx context.Context, projectID uuid.UUID) ([]models.AuditModel, error) {
	s.projectReads++
	return s.AuditRepository.ListByProject(ctx, projectID)
}

type spiedRepository struct {
	repositories.Repository
	audit *auditSpy
}

func (r spiedRepository) Audit() contracts.AuditRepository { return r.audit }

// The dashboard's figures. The completion rate was total minus unclaimed, so
// a task somebody had merely claimed counted as done, because the count it
// needed did not exist. And every dashboard load read the project's whole
// audit trail to build a step heat map that no transport sends: work that
// grows with the history of the project, thrown away on every refresh.
func TestTheStatisticsCountCompletedTasksAndReadNoAuditTrail(t *testing.T) {
	db := testutils.SetupTestDB(t)
	repo := repositories.NewRepository(testutils.StormConn(db))
	svc := services.NewServiceFacade(repo, observersimpl.NewEventDispatcher(), observersimpl.NewSSEObserver(),
		"statistics-test", nil, nil, nil, func(*gorm.DB) {})
	ctx, _, projectID := testutils.ScopedProject(t, repo)

	if _, err := svc.CreateDefinition(ctx, &entities.ProcessDefinition{
		Project: &entities.Project{ID: projectID},
		Key:     "review",
		Nodes: []*entities.Node{
			{ID: "start", Type: entities.StartEvent},
			{ID: "review", Type: entities.UserTask, Name: "Review"},
			{ID: "end", Type: entities.EndEvent},
		},
		Flows: []*entities.SequenceFlow{
			{ID: "f1", SourceRef: "start", TargetRef: "review"},
			{ID: "f2", SourceRef: "review", TargetRef: "end"},
		},
	}); err != nil {
		t.Fatalf("deploy: %v", err)
	}
	for range 3 {
		if _, err := svc.StartProcess(ctx, projectID, "review", nil); err != nil {
			t.Fatalf("start: %v", err)
		}
	}
	tasks, err := svc.ListTasks(ctx, projectID)
	if err != nil || len(tasks) != 3 {
		t.Fatalf("expected three tasks: %d, %v", len(tasks), err)
	}
	// One done, one claimed and not done, one nobody has picked up.
	if err := svc.CompleteTask(ctx, tasks[0].ID, "ada", nil); err != nil {
		t.Fatalf("complete: %v", err)
	}
	if err := svc.ClaimTask(ctx, tasks[1].ID, "ada"); err != nil {
		t.Fatalf("claim: %v", err)
	}

	spy := &auditSpy{AuditRepository: repo.Audit()}
	stats, err := serviceimpl.NewProjectService(spiedRepository{Repository: repo, audit: spy}).GetProcessStatistics(ctx, projectID)
	if err != nil {
		t.Fatalf("statistics: %v", err)
	}
	if stats.TotalTasks != 3 || stats.CompletedTasks != 1 || stats.PendingTasks != 1 {
		t.Errorf("total=%d completed=%d pending=%d, want 3, 1 and 1", stats.TotalTasks, stats.CompletedTasks, stats.PendingTasks)
	}
	if spy.projectReads != 0 {
		t.Errorf("the statistics read the project's whole audit trail %d time(s)", spy.projectReads)
	}
}
