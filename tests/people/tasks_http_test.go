package people_test

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/gsoultan/metis/internal/pkg/apierr"
	"github.com/gsoultan/metis/server/domains/entities"
	"github.com/gsoultan/metis/server/repositories/pg"
	"github.com/gsoultan/metis/tests/testutils"
)

func (w world) tenant(t *testing.T) entities.TenantContext {
	t.Helper()
	return entities.TenantContext{TenantID: w.org.String()}
}

// startTask deploys a one-task process in project and starts it, returning
// the task it opened. node shapes who the task is for.
func (w world) startTask(t *testing.T, project uuid.UUID, key string, node entities.Node) entities.Task {
	t.Helper()
	ctx := entities.WithTenantContext(t.Context(), w.tenant(t))
	node.ID, node.Name, node.Type = "approve", "Approve", entities.UserTask
	node.Properties = testutils.FormDeclaring("approved")
	node.Incoming, node.Outgoing = []string{"f1"}, []string{"f2"}
	def := &entities.ProcessDefinition{
		Project: &entities.Project{ID: project},
		Key:     key,
		Name:    key,
		Nodes: []*entities.Node{
			{ID: "start", Type: entities.StartEvent, Outgoing: []string{"f1"}},
			&node,
			{ID: "end", Type: entities.EndEvent, Incoming: []string{"f2"}},
		},
		Flows: []*entities.SequenceFlow{
			{ID: "f1", SourceRef: "start", TargetRef: "approve"},
			{ID: "f2", SourceRef: "approve", TargetRef: "end"},
		},
	}
	if _, err := w.svc.CreateDefinition(ctx, def); err != nil {
		t.Fatalf("deploy %s: %v", key, err)
	}
	instance, err := w.svc.StartProcess(ctx, project, key, nil)
	if err != nil {
		t.Fatalf("start %s: %v", key, err)
	}
	tasks, err := w.svc.ListTasks(ctx, project)
	if err != nil {
		t.Fatalf("list the tasks of %s: %v", key, err)
	}
	for _, task := range tasks {
		if task.Instance != nil && task.Instance.ID == instance {
			return task
		}
	}
	t.Fatalf("%s opened no task", key)
	return entities.Task{}
}

func (w world) status(t *testing.T, id uuid.UUID) entities.TaskStatus {
	t.Helper()
	ctx := entities.WithTenantContext(t.Context(), w.tenant(t))
	task, err := w.svc.GetTask(ctx, id)
	if err != nil {
		t.Fatalf("read task %s: %v", id, err)
	}
	return task.Status
}

func listed(t *testing.T, body string) map[uuid.UUID]bool {
	t.Helper()
	var out struct {
		Tasks []entities.Task `json:"tasks"`
	}
	if err := json.Unmarshal([]byte(body), &out); err != nil {
		t.Fatalf("read the task list %s: %v", body, err)
	}
	ids := map[uuid.UUID]bool{}
	for _, task := range out.Tasks {
		ids[task.ID] = true
	}
	return ids
}

// TestAPersonWorksTheirTasksThroughTheirOwnRoutes is the People inbox end to
// end, on the tables: a task given to ada by name, a task offered to a People
// group she is in, and the work she must not see — a task given to someone
// else in her project, and a task given to another ada in another project.
func TestAPersonWorksTheirTasksThroughTheirOwnRoutes(t *testing.T) {
	w := newWorld(t)
	system := entities.WithSystemContext(t.Context())

	// The approvers People group, with ada in it.
	var adaID uuid.UUID
	if err := w.conn.Main().QueryRow(t.Context(),
		"SELECT id FROM workflow_users WHERE project_id = $1 AND username = 'ada'", w.project).Scan(&adaID); err != nil {
		t.Fatalf("read ada: %v", err)
	}
	var groupID uuid.UUID
	if err := w.conn.Main().QueryRow(t.Context(),
		"INSERT INTO workflow_groups (project_id, name) VALUES ($1, 'approvers') RETURNING id", w.project).Scan(&groupID); err != nil {
		t.Fatalf("seed the approvers: %v", err)
	}
	if _, err := w.conn.Main().Exec(t.Context(),
		"INSERT INTO workflow_group_memberships (workflow_group_id, workflow_user_id) VALUES ($1, $2)", groupID, adaID); err != nil {
		t.Fatalf("put ada in the approvers: %v", err)
	}

	// Another project of the same organization, with an ada of its own.
	tenant := entities.WithTenantContext(system, w.tenant(t))
	other, err := w.svc.CreateProject(tenant, w.org, "Leave Requests", "")
	if err != nil {
		t.Fatalf("seed the other project: %v", err)
	}
	if _, err := pg.NewWorkflowUserRepository(w.conn).Upsert(system, entities.WorkflowUser{
		Project: &entities.Project{ID: other.ID}, Username: "ada", Active: true,
	}); err != nil {
		t.Fatalf("seed the other ada: %v", err)
	}

	named := w.startTask(t, w.project, "named", entities.Node{Assignee: "ada"})
	pool := w.startTask(t, w.project, "pool", entities.Node{CandidateGroups: []*entities.Group{{Name: "approvers"}}})
	someoneElses := w.startTask(t, w.project, "someone-elses", entities.Node{Assignee: "bob"})
	otherProjects := w.startTask(t, other.ID, "other-projects", entities.Node{Assignee: "ada"})

	token := w.signIn(t)

	code, body := w.do(t, http.MethodGet, "/api/v1/people/tasks", token, nil)
	if code != http.StatusOK {
		t.Fatalf("ada lists her tasks: %d %s", code, body)
	}
	ids := listed(t, body)
	if !ids[named.ID] || !ids[pool.ID] {
		t.Fatalf("ada's list %s is missing her named task or her group's pool task", body)
	}
	if ids[someoneElses.ID] || ids[otherProjects.ID] {
		t.Fatalf("ada's list %s shows work that is not hers", body)
	}

	for name, id := range map[string]uuid.UUID{"someone else's": someoneElses.ID, "another project's": otherProjects.ID} {
		path := "/api/v1/people/tasks/" + id.String()
		if code, body := w.do(t, http.MethodGet, path, token, nil); code != http.StatusNotFound {
			t.Errorf("ada reads %s task = %d %s, want 404", name, code, body)
		}
		if code, body := w.do(t, http.MethodPost, path+"/complete", token,
			map[string]any{"variables": map[string]any{"approved": true}}); code != http.StatusNotFound {
			t.Errorf("ada completes %s task = %d %s, want 404", name, code, body)
		}
	}
	if got := w.status(t, otherProjects.ID); got == entities.TaskCompleted {
		t.Fatal("another project's task was completed by ada")
	}

	// A User's token opens none of this, even naming a real task.
	if code, body := w.do(t, http.MethodGet, "/api/v1/people/tasks/"+named.ID.String(), w.userToken, nil); code != http.StatusUnauthorized {
		t.Errorf("a User's token on a People task route = %d %s, want 401", code, body)
	}

	// The pool task: claim, give it back, claim again, complete.
	poolPath := "/api/v1/people/tasks/" + pool.ID.String()
	if code, body := w.do(t, http.MethodGet, poolPath, token, nil); code != http.StatusOK || !strings.Contains(body, pool.ID.String()) {
		t.Fatalf("ada reads the pool task offered to her group: %d %s", code, body)
	}
	if code, body := w.do(t, http.MethodPost, poolPath+"/claim", token, nil); code != http.StatusOK {
		t.Fatalf("ada claims the pool task: %d %s", code, body)
	}
	if got := w.status(t, pool.ID); got != entities.TaskClaimed {
		t.Fatalf("pool task after ada's claim: %q, want claimed", got)
	}
	if code, body := w.do(t, http.MethodPost, poolPath+"/release", token, nil); code != http.StatusOK {
		t.Fatalf("ada gives the pool task back: %d %s", code, body)
	}
	if got := w.status(t, pool.ID); got != entities.TaskUnclaimed {
		t.Fatalf("pool task after ada released it: %q, want unclaimed", got)
	}
	if code, body := w.do(t, http.MethodPost, poolPath+"/claim", token, nil); code != http.StatusOK {
		t.Fatalf("ada claims the pool task again: %d %s", code, body)
	}
	if code, body := w.do(t, http.MethodPost, poolPath+"/complete", token,
		map[string]any{"variables": map[string]any{"approved": true}}); code != http.StatusOK {
		t.Fatalf("ada completes the pool task: %d %s", code, body)
	}
	if got := w.status(t, pool.ID); got != entities.TaskCompleted {
		t.Fatalf("pool task after ada completed it: %q, want completed", got)
	}

	// The named task: hers alone, so there is nobody to give it back to.
	namedPath := "/api/v1/people/tasks/" + named.ID.String()
	if code, body := w.do(t, http.MethodPost, namedPath+"/release", token, nil); code != http.StatusBadRequest {
		t.Errorf("ada releases a task given to her by name = %d %s, want 400", code, body)
	}
	if code, body := w.do(t, http.MethodPost, namedPath+"/complete", token,
		map[string]any{"variables": map[string]any{"approved": true}}); code != http.StatusOK {
		t.Fatalf("ada completes her named task: %d %s", code, body)
	}
	if got := w.status(t, named.ID); got != entities.TaskCompleted {
		t.Fatalf("named task after ada completed it: %q, want completed", got)
	}

	// Done work leaves the inbox.
	_, body = w.do(t, http.MethodGet, "/api/v1/people/tasks", token, nil)
	if ids := listed(t, body); ids[named.ID] || ids[pool.ID] {
		t.Fatalf("ada's list after finishing %s still shows finished work", body)
	}
}

// TestOnceUsersNoLongerDoTasksOnlyPeopleDo: with the switch-over window
// closed, a User's claim and completion are refused, and the person the task
// names still does it through their own routes.
func TestOnceUsersNoLongerDoTasksOnlyPeopleDo(t *testing.T) {
	w := newWorld(t)
	t.Setenv("METIS_TASKS_ALLOW_PLATFORM_ACTORS", "false")
	ctx := entities.WithTenantContext(t.Context(), w.tenant(t))

	named := w.startTask(t, w.project, "named", entities.Node{Assignee: "ada"})
	if err := w.svc.ClaimTask(ctx, named.ID, "ada"); !errors.Is(err, apierr.ErrForbidden) {
		t.Errorf("a User named ada claims ada's task = %v, want forbidden", err)
	}
	if err := w.svc.CompleteTask(ctx, named.ID, "ada", map[string]any{"approved": true}); !errors.Is(err, apierr.ErrForbidden) {
		t.Errorf("a User named ada completes ada's task = %v, want forbidden", err)
	}
	if got := w.status(t, named.ID); got == entities.TaskCompleted {
		t.Fatal("a User completed the task with the window closed")
	}

	token := w.signIn(t)
	if code, body := w.do(t, http.MethodPost, "/api/v1/people/tasks/"+named.ID.String()+"/complete", token,
		map[string]any{"variables": map[string]any{"approved": true}}); code != http.StatusOK {
		t.Fatalf("ada completes her task as a person: %d %s", code, body)
	}
}
