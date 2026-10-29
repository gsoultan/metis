package pagination_test

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/gsoultan/metis/server/repositories/db"
	"github.com/gsoultan/metis/server/repositories/store/decisiondefinition"
	"github.com/gsoultan/metis/server/repositories/store/notification"
	"github.com/gsoultan/metis/server/repositories/store/processdefinition"
	"github.com/gsoultan/metis/server/repositories/store/processinstance"
	"github.com/gsoultan/metis/server/repositories/store/task"
	"github.com/gsoultan/storm/runtime"

	"github.com/google/uuid"
	"github.com/gsoultan/metis/server/repositories"
	"github.com/gsoultan/metis/server/repositories/contracts"
	"github.com/gsoultan/metis/server/repositories/models"
	"github.com/gsoultan/metis/tests/testutils"
)

// Rows created in one statement — the way one transaction creates them, a
// deployment or a multi-instance step — share a created_at to the microsecond.
// sameMoment is several pages of them.
const (
	sameMoment = 120
	smallPage  = 10
)

// A paged list has to show every row exactly once as somebody walks it.
//
// Every one of these was ordered by created_at alone. Among rows that share a
// created_at that is no order at all: PostgreSQL returns ties in whatever order
// the plan produces, and a plan with LIMIT 10 OFFSET 20 is not the plan with
// LIMIT 10 OFFSET 30. So a row could come back on two pages and another on
// none, and nobody could find the one that was never shown.
func TestEveryPagedListShowsRowsSharingACreationTimeExactlyOnce(t *testing.T) {
	db := testutils.SetupTestDB(t)
	repo := repositories.NewRepository(db)
	ctx, _, projectID := testutils.ScopedProject(t, repo)
	instanceID := seedAtOneMoment(t, db, ctx, projectID)

	reads := []struct {
		name string
		page func(p contracts.Pagination) ([]uuid.UUID, error)
	}{
		{"tasks of a project", func(p contracts.Pagination) ([]uuid.UUID, error) {
			page, err := repo.Task().ListByProjectPaged(ctx, projectID, p)
			return idsOf(page.Items, taskIDOf), err
		}},
		{"tasks of the tenant", func(p contracts.Pagination) ([]uuid.UUID, error) {
			page, err := repo.Task().ListPaged(ctx, p)
			return idsOf(page.Items, taskIDOf), err
		}},
		{"tasks of an assignee", func(p contracts.Pagination) ([]uuid.UUID, error) {
			page, err := repo.Task().ListByAssigneePaged(ctx, "alice", p)
			return idsOf(page.Items, taskIDOf), err
		}},
		{"tasks of an instance", func(p contracts.Pagination) ([]uuid.UUID, error) {
			page, err := repo.Task().ListByInstancePaged(ctx, instanceID, p)
			return idsOf(page.Items, taskIDOf), err
		}},
		{"tasks a candidate could take", func(p contracts.Pagination) ([]uuid.UUID, error) {
			page, err := repo.Task().ListByCandidatesPaged(ctx, contracts.Candidacy{User: "alice", Groups: []string{"clerks"}}, p)
			return idsOf(page.Items, taskIDOf), err
		}},
		{"instances of a project", func(p contracts.Pagination) ([]uuid.UUID, error) {
			page, err := repo.Process().ListByProjectPaged(ctx, projectID, contracts.InstanceFilter{}, p)
			return idsOf(page.Items, instanceIDOf), err
		}},
		{"instances of the tenant", func(p contracts.Pagination) ([]uuid.UUID, error) {
			page, err := repo.Process().ListPaged(ctx, contracts.InstanceFilter{}, p)
			return idsOf(page.Items, instanceIDOf), err
		}},
		{"definitions of a project", func(p contracts.Pagination) ([]uuid.UUID, error) {
			page, err := repo.Definition().ListByProjectPaged(ctx, projectID, p)
			return idsOf(page.Items, definitionIDOf), err
		}},
		{"decisions of a project", func(p contracts.Pagination) ([]uuid.UUID, error) {
			page, err := repo.Decision().ListByProjectPaged(ctx, projectID, "", p)
			return idsOf(page.Items, decisionIDOf), err
		}},
		{"notifications of a person", func(p contracts.Pagination) ([]uuid.UUID, error) {
			page, err := repo.Notification().ListByUserPaged(ctx, "alice", p)
			return idsOf(page.Items, notificationIDOf), err
		}},
	}
	for _, read := range reads {
		t.Run(read.name, func(t *testing.T) {
			seen := map[uuid.UUID]int{}
			for n := 1; n <= sameMoment/smallPage; n++ {
				ids, err := read.page(contracts.Pagination{Page: n, PageSize: smallPage})
				if err != nil {
					t.Fatalf("page %d: %v", n, err)
				}
				for _, id := range ids {
					seen[id]++
				}
			}
			repeated := 0
			for _, count := range seen {
				if count > 1 {
					repeated++
				}
			}
			if len(seen) != sameMoment || repeated > 0 {
				t.Fatalf("walking every page showed %d of %d rows, %d of them more than once",
					len(seen), sameMoment, repeated)
			}
		})
	}
}

// seedAtOneMoment writes sameMoment tasks, instances, definitions, decisions
// and notifications, each table in one statement, so each shares one
// created_at. It answers the instance the tasks belong to.
func seedAtOneMoment(t *testing.T, db *db.Conn, ctx context.Context, projectID uuid.UUID) uuid.UUID {
	t.Helper()
	ex, now := testutils.Executor(db), time.Now()
	none, empty := runtime.JSON("[]"), runtime.JSON("{}")
	definitions := make([]processdefinition.Row, sameMoment)
	instances := make([]processinstance.Row, sameMoment)
	tasks := make([]task.Row, sameMoment)
	decisions := make([]decisiondefinition.Row, sameMoment)
	notifications := make([]notification.Row, sameMoment)
	for i := range sameMoment {
		n := i + 1
		definitions[i] = processdefinition.Row{
			ID: uuid.New(), CreatedAt: now, UpdatedAt: now, ProjectID: projectID,
			Key: fmt.Sprintf("process-%d", n), Name: fmt.Sprintf("Process %d", n), Version: 1, Nodes: none, Flows: none,
		}
		instances[i] = processinstance.Row{
			ID: uuid.New(), CreatedAt: now, UpdatedAt: now, ProjectID: projectID, DefinitionID: definitions[0].ID,
			Status: "active", Tokens: none, CompletedNodes: none, CompensatedNodes: none, MultiInstance: empty, Joins: empty,
		}
		tasks[i] = task.Row{
			ID: uuid.New(), CreatedAt: now, UpdatedAt: now, ProjectID: projectID, InstanceID: instances[0].ID,
			NodeID: "approve", Name: "Approve", Type: "userTask", Status: "unclaimed",
			Assignee:       runtime.Null[string]{V: "alice", Valid: true},
			CandidateUsers: runtime.JSON(`["alice"]`), CandidateGroups: runtime.JSON(`["clerks"]`),
		}
		decisions[i] = decisiondefinition.Row{
			ID: uuid.New(), CreatedAt: now, UpdatedAt: now, ProjectID: projectID,
			Key: fmt.Sprintf("decision-%d", n), Name: fmt.Sprintf("Decision %d", n), Version: 1, HitPolicy: "FIRST",
			RequiredDecisions: none, Inputs: none, Outputs: none, Rules: none, Tests: none,
		}
		notifications[i] = notification.Row{
			ID: uuid.New(), CreatedAt: now, UpdatedAt: now, UserID: "alice", Type: "TaskAssignment",
			Title: fmt.Sprintf("Approve %d", n), Message: fmt.Sprintf("Request %d", n),
			ProjectID: runtime.Null[[16]byte]{V: projectID, Valid: true},
		}
	}
	seeded := func(table string, _ int64, err error) {
		t.Helper()
		if err != nil {
			t.Fatalf("seed %s: %v", table, err)
		}
	}
	n, err := processdefinition.InsertAll(ctx, ex, definitions)
	seeded("definitions", n, err)
	n, err = processinstance.InsertAll(ctx, ex, instances)
	seeded("instances", n, err)
	n, err = task.InsertAll(ctx, ex, tasks)
	seeded("tasks", n, err)
	n, err = decisiondefinition.InsertAll(ctx, ex, decisions)
	seeded("decisions", n, err)
	n, err = notification.InsertAll(ctx, ex, notifications)
	seeded("notifications", n, err)
	return instances[0].ID
}

func taskIDOf(m models.TaskModel) models.UUID                    { return m.ID }
func instanceIDOf(m models.ProcessInstanceModel) models.UUID     { return m.ID }
func definitionIDOf(m models.ProcessDefinitionModel) models.UUID { return m.ID }
func decisionIDOf(m models.DecisionDefinitionModel) models.UUID  { return m.ID }
func notificationIDOf(m models.NotificationModel) models.UUID    { return m.ID }

func idsOf[T any](items []T, id func(T) models.UUID) []uuid.UUID {
	out := make([]uuid.UUID, len(items))
	for i, item := range items {
		out[i] = uuid.UUID(id(item))
	}
	return out
}
