package task_test

import (
	"context"
	"slices"
	"testing"

	"github.com/gsoultan/metis/server/repositories/db"
	taskstore "github.com/gsoultan/metis/server/repositories/store/task"
	"github.com/gsoultan/storm/runtime"

	"github.com/google/uuid"
	"github.com/gsoultan/metis/server/domains/entities"
	"github.com/gsoultan/metis/server/repositories"
	repocontracts "github.com/gsoultan/metis/server/repositories/contracts"
	"github.com/gsoultan/metis/server/repositories/models"
	"github.com/gsoultan/metis/tests/testutils"
)

// The repository's two readings of who a task is offered to: the unclaimed
// work a person could pick up, and a person's open work in one project. Both
// read the candidate lists as the text of a JSON array, so these pin what that
// text is asked: a name quoted, so "ada" is not "adam"; any of a person's
// groups; and "nobody", whichever way a row spells it.

type candidateFixture struct {
	db        *db.Conn
	repo      repositories.Repository
	ctx       context.Context
	projectID uuid.UUID
	names     map[uuid.UUID]string
}

func newCandidateFixture(t *testing.T) *candidateFixture {
	t.Helper()
	db := testutils.SetupTestDB(t)
	repo := repositories.NewRepository(db)
	ctx, orgID := testutils.ScopedContext(t, repo)
	projectID := uuid.Must(uuid.NewV7())
	if err := repo.Project().Create(entities.WithSystemContext(ctx), models.ProjectModel{
		Base:           models.Base{ID: models.UUID(projectID)},
		OrganizationID: models.UUID(orgID),
		Name:           "Candidates",
	}); err != nil {
		t.Fatalf("seed project: %v", err)
	}
	return &candidateFixture{db: db, repo: repo, ctx: ctx, projectID: projectID, names: map[uuid.UUID]string{}}
}

func (f *candidateFixture) seed(t *testing.T, name string, task entities.Task) uuid.UUID {
	t.Helper()
	task.Name = name
	if task.Status == "" {
		task.Status = entities.TaskUnclaimed
	}
	task.Node = &entities.Node{ID: "step"}
	id := seedTask(t, f.repo, f.ctx, f.projectID, task)
	f.names[id] = name
	return id
}

// spell rewrites a task's columns the way a row written by something other
// than this repository can hold them.
func (f *candidateFixture) spell(t *testing.T, id uuid.UUID, change func(*taskstore.Mut)) {
	t.Helper()
	rewriteTask(t, f.db, id, change)
}

func (f *candidateFixture) named(tasks []models.TaskModel) []string {
	var got []string
	for _, m := range tasks {
		got = append(got, f.names[uuid.UUID(m.ID)])
	}
	slices.Sort(got)
	return got
}

func users(names ...string) []*entities.User {
	var out []*entities.User
	for _, n := range names {
		out = append(out, &entities.User{Username: n})
	}
	return out
}

func groups(names ...string) []*entities.Group {
	var out []*entities.Group
	for _, n := range names {
		out = append(out, &entities.Group{Name: n})
	}
	return out
}

func TestCandidateTasksAreTheOnesOfferedToThePersonTheirGroupsOrNobody(t *testing.T) {
	f := newCandidateFixture(t)
	f.seed(t, "offered to ada", entities.Task{CandidateUsers: users("bob", "ada")})
	f.seed(t, "offered to adam", entities.Task{CandidateUsers: users("adam")})
	f.seed(t, "offered to finance", entities.Task{CandidateGroups: groups("finance")})
	f.seed(t, "offered to legal", entities.Task{CandidateGroups: groups("legal")})
	f.seed(t, "offered to an unrelated team", entities.Task{CandidateGroups: groups("sales")})
	f.seed(t, "named nobody", entities.Task{})
	jsonNull := f.seed(t, "named nobody, lists the JSON null", entities.Task{})
	f.spell(t, jsonNull, func(m *taskstore.Mut) {
		m.SetCandidateUsers(runtime.JSON("null"))
		m.SetCandidateGroups(runtime.JSON("null"))
	})
	empty := f.seed(t, "named nobody, assignee empty", entities.Task{})
	f.spell(t, empty, func(m *taskstore.Mut) { m.SetAssignee("") })
	f.seed(t, "claimed by ada", entities.Task{Status: entities.TaskClaimed, Assignee: &entities.User{Username: "ada"}, CandidateUsers: users("ada")})
	given := f.seed(t, "given to carol while unclaimed", entities.Task{CandidateUsers: users("carol")})
	f.spell(t, given, func(m *taskstore.Mut) {
		m.SetAssignee("carol")
		m.SetCandidateUsers(runtime.JSON("[]"))
	})

	ada := repocontracts.Candidacy{User: "ada", Groups: []string{"finance", "legal"}}
	got, err := f.repo.Task().ListByCandidates(f.ctx, ada)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"offered to ada", "offered to finance", "offered to legal"}
	if names := f.named(got); !slices.Equal(names, want) {
		t.Errorf("ada's candidate tasks are %q, want %q", names, want)
	}

	ada.Unnamed = true
	page, err := f.repo.Task().ListByCandidatesPaged(f.ctx, ada, repocontracts.Pagination{Page: 1, PageSize: 50})
	if err != nil {
		t.Fatal(err)
	}
	want = append(want, "named nobody", "named nobody, assignee empty", "named nobody, lists the JSON null")
	slices.Sort(want)
	if names := f.named(page.Items); !slices.Equal(names, want) || page.Total != int64(len(want)) {
		t.Errorf("with the unnamed ones, ada's candidate tasks are %q (total %d), want %q", names, page.Total, want)
	}
}

func TestAPersonsOpenTasksAreTheirsAndTheOnesOfferedToThemOrTheirGroups(t *testing.T) {
	f := newCandidateFixture(t)
	f.seed(t, "claimed by ada", entities.Task{Status: entities.TaskClaimed, Assignee: &entities.User{Username: "ada"}})
	f.seed(t, "escalated to ada", entities.Task{Status: entities.TaskEscalated, Assignee: &entities.User{Username: "ada"}})
	f.seed(t, "completed by ada", entities.Task{Status: entities.TaskCompleted, Assignee: &entities.User{Username: "ada"}})
	f.seed(t, "offered to ada", entities.Task{CandidateUsers: users("ada")})
	f.seed(t, "offered to a_a", entities.Task{CandidateGroups: groups("a_a")})
	f.seed(t, "offered to aXa", entities.Task{CandidateGroups: groups("aXa")})
	empty := f.seed(t, "offered to ada, assignee empty", entities.Task{CandidateUsers: users("ada")})
	f.spell(t, empty, func(m *taskstore.Mut) { m.SetAssignee("") })
	given := f.seed(t, "offered to ada but given to bob", entities.Task{CandidateUsers: users("ada")})
	f.spell(t, given, func(m *taskstore.Mut) { m.SetAssignee("bob") })
	f.seed(t, "named nobody", entities.Task{})

	page, err := f.repo.Task().ListOpenForPersonPaged(f.ctx, f.projectID, "ada", []string{"a_a"},
		repocontracts.Pagination{Page: 1, PageSize: 50})
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"claimed by ada", "escalated to ada", "offered to a_a", "offered to ada", "offered to ada, assignee empty"}
	slices.Sort(want)
	if names := f.named(page.Items); !slices.Equal(names, want) || page.Total != int64(len(want)) {
		t.Errorf("ada's open tasks are %q (total %d), want %q", names, page.Total, want)
	}

	// Without groups, only what is hers by name.
	page, err = f.repo.Task().ListOpenForPersonPaged(f.ctx, f.projectID, "ada", nil,
		repocontracts.Pagination{Page: 1, PageSize: 50})
	if err != nil {
		t.Fatal(err)
	}
	want = []string{"claimed by ada", "escalated to ada", "offered to ada", "offered to ada, assignee empty"}
	slices.Sort(want)
	if names := f.named(page.Items); !slices.Equal(names, want) {
		t.Errorf("without groups, ada's open tasks are %q, want %q", names, want)
	}
}
