package people_test

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/google/uuid"

	"github.com/gsoultan/metis/server/domains/entities"
	"github.com/gsoultan/metis/server/repositories/models"
	"github.com/gsoultan/metis/server/repositories/pg"
)

func (w world) stranded(t *testing.T, token string) (int, map[string]entities.StrandedTask) {
	t.Helper()
	code, body := w.do(t, http.MethodGet, "/api/v1/participants/stranded-tasks?project_id="+w.project.String(), token, nil)
	if code != http.StatusOK {
		return code, nil
	}
	var reply struct {
		Tasks []entities.StrandedTask `json:"tasks"`
	}
	if err := json.Unmarshal([]byte(body), &reply); err != nil {
		t.Fatalf("read %s: %v", body, err)
	}
	byTask := map[string]entities.StrandedTask{}
	for _, s := range reply.Tasks {
		byTask[s.TaskID.String()] = s
	}
	return code, byTask
}

// TestTasksNobodyCanActOnAreFound is the report on the tables: an open task
// whose holder left or was deactivated, or that is offered only to people and
// groups with nobody left who can take it, is listed with why; a task anybody
// can still act on is not.
func TestTasksNobodyCanActOnAreFound(t *testing.T) {
	w := newWorld(t)
	system := entities.WithSystemContext(t.Context())

	// The organization's reviewers group, with dana in it.
	reviewers := uuid.Must(uuid.NewV7())
	if err := w.repo.Group().Create(system, models.GroupModel{Base: models.Base{ID: models.UUID(reviewers)}, OrganizationID: models.UUID(w.org), Name: "reviewers"}); err != nil {
		t.Fatalf("seed reviewers: %v", err)
	}
	dana, err := w.repo.User().GetByUsername(system, "dana")
	if err != nil {
		t.Fatal(err)
	}
	if err := w.repo.Group().AddMembership(system, uuid.UUID(dana.ID), reviewers); err != nil {
		t.Fatal(err)
	}
	// Ada is an active person; Bo was one and has been deactivated. The
	// night shift is a People group with only Bo left in it.
	ada := w.seedPerson(t, "ada", "ada@example.com")
	bo := w.seedPerson(t, "bo", "bo@example.com")
	w.seedGroup(t, "night-shift", bo)
	w.seedGroup(t, "day-shift", ada)
	if _, err := pg.NewWorkflowUserRepository(w.conn).Upsert(system, entities.WorkflowUser{
		Project: &entities.Project{ID: w.project}, Username: "bo", Email: "bo@example.com", Active: false,
	}); err != nil {
		t.Fatalf("deactivate bo: %v", err)
	}

	stuck := map[string]entities.Task{}
	fine := map[string]entities.Task{}
	fine["held by an account"] = w.startTask(t, w.project, "held-by-dana", entities.Node{Assignee: "dana"})
	fine["held by a person"] = w.startTask(t, w.project, "held-by-ada", entities.Node{Assignee: "ada"})
	fine["offered to a group with an account in it"] = w.startTask(t, w.project, "for-reviewers", entities.Node{
		CandidateUsers: []*entities.User{{Username: "left"}}, CandidateGroups: []*entities.Group{{Name: "reviewers"}},
	})
	fine["offered to a People group with an active person"] = w.startTask(t, w.project, "for-day-shift", entities.Node{
		CandidateGroups: []*entities.Group{{Name: "day-shift"}},
	})
	fine["named nobody: an operator's"] = w.startTask(t, w.project, "for-nobody", entities.Node{})
	fine["an expression"] = w.startTask(t, w.project, "for-expression", entities.Node{
		CandidateUsers: []*entities.User{{Username: "${approver}"}},
	})
	stuck["held by a deactivated person"] = w.startTask(t, w.project, "held-by-bo", entities.Node{Assignee: "bo"})
	stuck["held by nobody of that name"] = w.startTask(t, w.project, "held-by-left", entities.Node{Assignee: "left"})
	stuck["offered to nobody who can act"] = w.startTask(t, w.project, "for-night-shift", entities.Node{
		CandidateUsers:  []*entities.User{{Username: "bo"}},
		CandidateGroups: []*entities.Group{{Name: "night-shift"}, {Name: "legal"}},
	})

	code, got := w.stranded(t, w.userToken)
	if code != http.StatusOK {
		t.Fatalf("read the report: %d", code)
	}
	for what, task := range fine {
		if s, ok := got[task.ID.String()]; ok {
			t.Errorf("%s: listed as stranded (%+v); somebody can act on it", what, s)
		}
	}
	if len(got) != len(stuck) {
		t.Errorf("got %d stranded tasks, want %d", len(got), len(stuck))
	}

	if s := got[stuck["held by a deactivated person"].ID.String()]; s.Kind != entities.StrandedAssigneeInactive || s.Assignee != "bo" ||
		len(s.Missing) != 1 || s.Missing[0].Why != "inactive" {
		t.Errorf("held by bo: %+v, want assignee_inactive naming bo", s)
	}
	if s := got[stuck["held by nobody of that name"].ID.String()]; s.Kind != entities.StrandedAssigneeGone || s.Assignee != "left" ||
		len(s.Missing) != 1 || s.Missing[0].Why != "gone" {
		t.Errorf("held by left: %+v, want assignee_gone naming left", s)
	}
	s := got[stuck["offered to nobody who can act"].ID.String()]
	why := map[string]string{}
	for _, m := range s.Missing {
		why[m.Kind+":"+m.Name] = m.Why
	}
	if s.Kind != entities.StrandedNoCandidate || why["person:bo"] != "inactive" || why["group:night-shift"] != "empty" || why["group:legal"] != "gone" {
		t.Errorf("for the night shift: %+v, want no_candidate with bo inactive, night-shift empty and legal gone", s)
	}
	if s.InstanceID == uuid.Nil || s.Since.IsZero() || s.Name == "" {
		t.Errorf("for the night shift: %+v, want the instance, the task's name and when it appeared", s)
	}
}

// TestTheStrandedReportIsAnOperatorsAndStaysInItsOrganization: somebody with
// no role in the project cannot read who the project's work waits for.
func TestTheStrandedReportIsAnOperatorsAndStaysInItsOrganization(t *testing.T) {
	w := newWorld(t)
	system := entities.WithSystemContext(t.Context())
	elsewhere := uuid.Must(uuid.NewV7())
	if err := w.repo.Organization().Create(system, models.OrganizationModel{Base: models.Base{ID: models.UUID(elsewhere)}, Name: "Elsewhere"}); err != nil {
		t.Fatal(err)
	}
	if err := w.svc.CreateUser(system, entities.User{
		ID: uuid.Must(uuid.NewV7()), Username: "ghost", Roles: []string{entities.RoleAdmin},
		Organizations: []*entities.Organization{{ID: elsewhere}},
	}, userPassword); err != nil {
		t.Fatalf("seed ghost: %v", err)
	}
	_, ghost, err := w.svc.Login(t.Context(), "ghost", userPassword)
	if err != nil {
		t.Fatal(err)
	}
	w.startTask(t, w.project, "held-by-left", entities.Node{Assignee: "left"})
	if code, _ := w.stranded(t, ghost); code == http.StatusOK {
		t.Fatal("another organization's administrator read this project's stranded tasks")
	}
}
