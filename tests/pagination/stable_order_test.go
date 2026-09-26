package pagination_test

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/gsoultan/metis/server/repositories"
	"github.com/gsoultan/metis/server/repositories/contracts"
	"github.com/gsoultan/metis/server/repositories/models"
	"github.com/gsoultan/metis/tests/testutils"
	"gorm.io/gorm"
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
	repo := repositories.NewRepository(testutils.StormConn(db))
	ctx, _, projectID := testutils.ScopedProject(t, repo)
	instanceID := uuid.Must(uuid.NewV7())
	seedAtOneMoment(t, db, ctx, projectID, instanceID)

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
			page, err := repo.Task().ListByCandidatesPaged(ctx, "alice", []string{"clerks"}, p)
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

// seedAtOneMoment writes sameMoment tasks, instances, definitions and decisions,
// each table in one statement, so each shares one created_at. Definitions come
// first: an instance names one.
func seedAtOneMoment(t *testing.T, db *gorm.DB, ctx context.Context, projectID, instanceID uuid.UUID) {
	t.Helper()
	for _, seed := range []struct {
		table     string
		statement string
		args      []any
	}{
		{"definitions", `
			INSERT INTO process_definitions (id, created_at, updated_at, project_id, key, name, version, nodes, flows)
			SELECT gen_random_uuid(), now(), now(), ?, 'process-' || n, 'Process ' || n, 1, '[]', '[]'
			  FROM generate_series(1, ?) AS n`,
			[]any{projectID, sameMoment}},
		{"instances", `
			INSERT INTO process_instances (id, created_at, updated_at, project_id, definition_id, status, variables,
			                               tokens, completed_nodes, compensated_nodes, multi_instance, joins)
			SELECT gen_random_uuid(), now(), now(), ?,
			       (SELECT id FROM process_definitions WHERE project_id = ? ORDER BY id LIMIT 1),
			       'active', '', '[]', '[]', '[]', '{}', '{}'
			  FROM generate_series(1, ?) AS n`,
			[]any{projectID, projectID, sameMoment}},
		{"tasks", `
			INSERT INTO tasks (id, created_at, updated_at, project_id, instance_id, node_id, name, type,
			                   status, assignee, candidate_users, candidate_groups, priority, variables)
			SELECT gen_random_uuid(), now(), now(), ?, ?, 'approve', 'Approve', 'userTask',
			       'unclaimed', 'alice', '["alice"]', '["clerks"]', 0, ''
			  FROM generate_series(1, ?) AS n`,
			[]any{projectID, instanceID, sameMoment}},
		{"decisions", `
			INSERT INTO decision_definitions (id, created_at, updated_at, project_id, key, name, version, hit_policy,
			                                  required_decisions, inputs, outputs, rules, tests)
			SELECT gen_random_uuid(), now(), now(), ?, 'decision-' || n, 'Decision ' || n, 1, 'FIRST',
			       '[]', '[]', '[]', '[]', '[]'
			  FROM generate_series(1, ?) AS n`,
			[]any{projectID, sameMoment}},
		{"notifications", `
			INSERT INTO notifications (id, created_at, updated_at, user_id, type, title, message, is_read, project_id)
			SELECT gen_random_uuid(), now(), now(), 'alice', 'TaskAssignment', 'Approve ' || n, 'Request ' || n, false, ?
			  FROM generate_series(1, ?) AS n`,
			[]any{projectID, sameMoment}},
	} {
		if err := db.WithContext(ctx).Exec(seed.statement, seed.args...).Error; err != nil {
			t.Fatalf("seed %s: %v", seed.table, err)
		}
	}
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
