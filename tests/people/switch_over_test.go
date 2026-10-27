package people_test

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/google/uuid"

	"github.com/gsoultan/metis/server/domains/entities"
	"github.com/gsoultan/metis/server/repositories/models"
)

type gapsReply struct {
	Gaps []entities.DirectoryGap `json:"gaps"`
}

func (w world) gaps(t *testing.T) map[string]entities.DirectoryGap {
	t.Helper()
	code, body := w.do(t, http.MethodGet, "/api/v1/participants/gaps?project_id="+w.project.String(), w.userToken, nil)
	if code != http.StatusOK {
		t.Fatalf("read the switch-over report: %d %s", code, body)
	}
	var reply gapsReply
	if err := json.Unmarshal([]byte(body), &reply); err != nil {
		t.Fatalf("read %s: %v", body, err)
	}
	out := map[string]entities.DirectoryGap{}
	for _, g := range reply.Gaps {
		out[g.Kind+":"+g.Name] = g
	}
	return out
}

// TestTheSwitchOverReportFindsWhatPeopleLackAndFillsIt is the report on the
// tables: the names open tasks and live models use that the project's People
// do not have, what each can be filled from, and filling them.
func TestTheSwitchOverReportFindsWhatPeopleLackAndFillsIt(t *testing.T) {
	w := newWorld(t)
	system := entities.WithSystemContext(t.Context())

	// Eve is a User of the organization; ghost is a User of another one, and
	// must read as nobody here.
	if err := w.svc.CreateUser(system, entities.User{
		ID: uuid.Must(uuid.NewV7()), Username: "eve", DisplayName: "Eve Moneypenny", Email: "eve@example.com",
		Roles: []string{entities.RoleUser}, Organizations: []*entities.Organization{{ID: w.org}},
	}, userPassword); err != nil {
		t.Fatalf("seed eve: %v", err)
	}
	elsewhere := uuid.Must(uuid.NewV7())
	if err := w.repo.Organization().Create(system, models.OrganizationModel{Base: models.Base{ID: models.UUID(elsewhere)}, Name: "Elsewhere"}); err != nil {
		t.Fatal(err)
	}
	if err := w.svc.CreateUser(system, entities.User{
		ID: uuid.Must(uuid.NewV7()), Username: "ghost", Email: "ghost@elsewhere.example",
		Roles: []string{entities.RoleAdmin}, Organizations: []*entities.Organization{{ID: elsewhere}},
	}, userPassword); err != nil {
		t.Fatalf("seed ghost: %v", err)
	}
	// The organization's reviewers group: dana and eve.
	reviewers := uuid.Must(uuid.NewV7())
	if err := w.repo.Group().Create(system, models.GroupModel{Base: models.Base{ID: models.UUID(reviewers)}, OrganizationID: models.UUID(w.org), Name: "reviewers"}); err != nil {
		t.Fatalf("seed reviewers: %v", err)
	}
	for _, name := range []string{"dana", "eve"} {
		u, err := w.repo.User().GetByUsername(system, name)
		if err != nil {
			t.Fatal(err)
		}
		if err := w.repo.Group().AddMembership(system, uuid.UUID(u.ID), reviewers); err != nil {
			t.Fatal(err)
		}
	}

	w.startTask(t, w.project, "for-dana", entities.Node{Assignee: "dana"})
	w.startTask(t, w.project, "for-reviewers", entities.Node{
		CandidateUsers:  []*entities.User{{Username: "eve"}, {Username: "ghost"}, {Username: "ada"}, {Username: "${initiator}"}},
		CandidateGroups: []*entities.Group{{Name: "reviewers"}},
	})

	got := w.gaps(t)
	if len(got) != 4 {
		t.Fatalf("gaps = %v, want dana, eve, ghost and reviewers: ada is a person and an expression is nobody", got)
	}
	if g := got["person:dana"]; g.OpenTasks != 1 || !g.HasAccount {
		t.Errorf("dana: %+v, want one open task and an account", g)
	}
	if g := got["person:eve"]; g.OpenTasks != 1 || !g.HasAccount || g.DisplayName != "Eve Moneypenny" || len(g.Models) != 1 || g.Models[0] != "for-reviewers" {
		t.Errorf("eve: %+v, want one open task, her account, and the model that names her", g)
	}
	if g := got["person:ghost"]; g.HasAccount || g.Email != "" {
		t.Errorf("ghost: %+v; another organization's account was read out", g)
	}
	if g := got["group:reviewers"]; !g.HasPlatformGroup || len(g.Members) != 2 {
		t.Errorf("reviewers: %+v, want the organization's group with dana and eve", g)
	}

	code, body := w.do(t, http.MethodPost, "/api/v1/participants/gaps/fill", w.userToken, map[string]any{
		"project_id": w.project.String(), "people": []string{"dana", "ghost"}, "groups": []string{"reviewers"},
	})
	if code != http.StatusOK {
		t.Fatalf("fill the gaps: %d %s", code, body)
	}
	var filled struct {
		Result entities.DirectoryGapImport `json:"result"`
	}
	if err := json.Unmarshal([]byte(body), &filled); err != nil {
		t.Fatal(err)
	}
	if filled.Result.People != 2 || filled.Result.Groups != 1 || len(filled.Result.Skipped) != 1 || filled.Result.Skipped[0].Username != "ghost" {
		t.Fatalf("filled %+v, want dana and eve added, reviewers made, ghost skipped", filled.Result)
	}
	after := w.gaps(t)
	if len(after) != 1 || after["person:ghost"].Name != "ghost" {
		t.Fatalf("gaps after filling = %v, want only ghost", after)
	}

	// An administrator of another organization reads nothing of this.
	_, ghost, err := w.svc.Login(t.Context(), "ghost", userPassword)
	if err != nil {
		t.Fatal(err)
	}
	if code, body := w.do(t, http.MethodGet, "/api/v1/participants/gaps?project_id="+w.project.String(), ghost, nil); code == http.StatusOK {
		t.Errorf("another organization's administrator read the report: %s", body)
	}
	if code, body := w.do(t, http.MethodPost, "/api/v1/participants/gaps/fill", ghost, map[string]any{
		"project_id": w.project.String(), "people": []string{"ghost"},
	}); code == http.StatusOK {
		t.Errorf("another organization's administrator filled the directory: %s", body)
	}
}
