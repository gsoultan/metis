package decision_test

import (
	"context"
	"slices"
	"testing"

	"github.com/google/uuid"
	"github.com/gsoultan/metis/server/domains/entities"
	servicecontracts "github.com/gsoultan/metis/server/domains/services/contracts"
	serviceimpl "github.com/gsoultan/metis/server/domains/services/impl"
	"github.com/gsoultan/metis/server/repositories"
	repocontracts "github.com/gsoultan/metis/server/repositories/contracts"
	"github.com/gsoultan/metis/server/repositories/models"
	"github.com/gsoultan/metis/tests/testutils"
)

// The decision list, its dependency graph and a step's decision picker used to
// load every version of every table in full — lines and examples included — a
// thousand at a time, before the page showed a row. The graph and the picker
// need each key once, and the list needs a page that can be searched.

type listWorld struct {
	svc     servicecontracts.DecisionService
	repo    repositories.Repository
	ctx     context.Context
	project uuid.UUID
}

func newListWorld(t *testing.T) listWorld {
	t.Helper()
	db := testutils.SetupTestDB(t)
	repo := repositories.NewRepository(testutils.StormConn(db))
	svc := serviceimpl.NewDecisionService(repo, serviceimpl.NewDecisionTableEvaluator(serviceimpl.NewFEELEvaluator()))
	ctx, _, projectID := testutils.ScopedProject(t, repo)
	return listWorld{svc: svc, repo: repo, ctx: ctx, project: projectID}
}

// deploy stores a new version of key in project, with a table that has lines
// and an example, so a summary that carried them would show it.
func (w listWorld) deploy(t *testing.T, project uuid.UUID, key, name string, requires ...string) {
	t.Helper()
	if _, err := w.svc.CreateDecision(w.ctx, entities.DecisionDefinition{
		Project:           &entities.Project{ID: project},
		Key:               key,
		Name:              name,
		HitPolicy:         entities.HitPolicyFirst,
		RequiredDecisions: requires,
		Inputs:            []entities.DecisionInput{{ID: "in", Label: "Score", Expression: "score", Type: "number"}},
		Outputs:           []entities.DecisionOutput{{ID: "out", Label: "Band", Name: "band", Type: "string"}},
		Rules:             []entities.DecisionRule{{ID: "r1", Inputs: []string{"> 10"}, Outputs: []any{"HIGH"}}},
		Tests:             []entities.DecisionTest{{ID: "t1", Name: "a high score", Inputs: map[string]any{"score": 20}}},
	}); err != nil {
		t.Fatalf("deploy %s: %v", key, err)
	}
}

// siblingProject is a second project of the same organization.
func (w listWorld) siblingProject(t *testing.T) uuid.UUID {
	t.Helper()
	id := uuid.Must(uuid.NewV7())
	if err := w.repo.Project().Create(entities.WithSystemContext(w.ctx), models.ProjectModel{
		Base:           models.Base{ID: models.UUID(id)},
		OrganizationID: models.UUID(testutils.OrgIDFrom(t, w.ctx)),
		Name:           "Another project",
	}); err != nil {
		t.Fatalf("seed a second project: %v", err)
	}
	return id
}

func TestDecisionSummariesNameEachKeyOnceAsItsNewestVersion(t *testing.T) {
	w := newListWorld(t)
	w.deploy(t, w.project, "credit-band", "Credit band")
	w.deploy(t, w.project, "credit-band", "Credit band, revised", "risk")
	w.deploy(t, w.project, "risk", "Risk")
	w.deploy(t, w.siblingProject(t), "elsewhere", "In another project")

	page, err := w.svc.ListDecisionSummaries(w.ctx, w.project, repocontracts.Pagination{})
	if err != nil {
		t.Fatalf("list the summaries: %v", err)
	}
	want := []entities.DecisionSummary{
		{Key: "credit-band", Name: "Credit band, revised", Version: 2, RequiredDecisions: []string{"risk"}},
		{Key: "risk", Name: "Risk", Version: 1},
	}
	if page.Total != int64(len(want)) || len(page.Items) != len(want) {
		t.Fatalf("got %d summaries of %d, want %d: %+v", len(page.Items), page.Total, len(want), page.Items)
	}
	for i, got := range page.Items {
		if got.ID == uuid.Nil {
			t.Errorf("summary %d has no id to open it by", i)
		}
		got.ID = uuid.Nil
		if got.Key != want[i].Key || got.Name != want[i].Name || got.Version != want[i].Version ||
			!slices.Equal(got.RequiredDecisions, want[i].RequiredDecisions) {
			t.Errorf("summary %d = %+v, want %+v", i, got, want[i])
		}
	}

	// The id is the newest version's, so opening a summary opens what the
	// engine evaluates.
	newest, err := w.repo.Decision().GetByKey(w.ctx, w.project, "credit-band")
	if err != nil {
		t.Fatalf("read the newest credit-band: %v", err)
	}
	if page.Items[0].ID != uuid.UUID(newest.ID) {
		t.Errorf("credit-band opens %v, want the newest version %v", page.Items[0].ID, uuid.UUID(newest.ID))
	}
}

func TestDecisionSummariesArePagedByKey(t *testing.T) {
	w := newListWorld(t)
	for _, key := range []string{"c-key", "a-key", "b-key"} {
		w.deploy(t, w.project, key, key)
	}

	var keys []string
	for number := 1; number <= 2; number++ {
		page, err := w.svc.ListDecisionSummaries(w.ctx, w.project, repocontracts.Pagination{Page: number, PageSize: 2})
		if err != nil {
			t.Fatalf("list page %d: %v", number, err)
		}
		if page.Total != 3 {
			t.Fatalf("page %d says there are %d keys, want 3", number, page.Total)
		}
		if page.HasMore() != (number == 1) {
			t.Errorf("page %d: has more = %v", number, page.HasMore())
		}
		for _, summary := range page.Items {
			keys = append(keys, summary.Key)
		}
	}
	if !slices.Equal(keys, []string{"a-key", "b-key", "c-key"}) {
		t.Fatalf("keys across the pages = %v, want each once in order", keys)
	}
}

func TestDecisionListIsSearchedOnTheServer(t *testing.T) {
	w := newListWorld(t)
	w.deploy(t, w.project, "credit_band", "Credit band")
	w.deploy(t, w.project, "tier", "Customer TIER")
	w.deploy(t, w.project, "discount", "50% off")
	w.deploy(t, w.project, "creditXband", "Not the one with an underscore")

	cases := []struct {
		search string
		want   []string
	}{
		{search: "", want: []string{"creditXband", "credit_band", "discount", "tier"}},
		{search: "tier", want: []string{"tier"}},           // by key and, ignoring case, by name
		{search: "customer", want: []string{"tier"}},       // by name alone
		{search: "credit_", want: []string{"credit_band"}}, // an underscore is not a wildcard
		{search: "50%", want: []string{"discount"}},        // nor a percent sign
		{search: "nothing like it", want: nil},
	}
	for _, c := range cases {
		page, err := w.svc.ListDecisionsPaged(w.ctx, w.project, c.search, repocontracts.Pagination{PageSize: 10})
		if err != nil {
			t.Fatalf("search %q: %v", c.search, err)
		}
		var keys []string
		for _, decision := range page.Items {
			keys = append(keys, decision.Key)
		}
		slices.Sort(keys)
		if !slices.Equal(keys, c.want) || page.Total != int64(len(c.want)) {
			t.Errorf("search %q found %v (total %d), want %v", c.search, keys, page.Total, c.want)
		}
	}
}
