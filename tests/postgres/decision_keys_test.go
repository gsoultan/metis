package postgres_test

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/gsoultan/metis/server/repositories/store/decisiondefinition"
	"github.com/gsoultan/metis/server/repositories/store/decisionrelease"
	projectstore "github.com/gsoultan/metis/server/repositories/store/project"
	"github.com/gsoultan/storm/runtime"

	"github.com/google/uuid"
	"github.com/gsoultan/metis/server/repositories"
	"github.com/gsoultan/metis/server/repositories/contracts"
	"github.com/gsoultan/metis/server/repositories/models"
	"github.com/gsoultan/metis/tests/testutils"
)

// Each decision key is listed once, as the version its newest release whose
// moment has come made live, or as its newest version when none did or the
// live one is deleted; searched by the shown version's name or the key, paged
// by key, and scoped to the projects the caller may see.
func TestDecisionKeysShowTheLiveVersionOrTheNewest(t *testing.T) {
	db := testutils.SetupTestDB(t)
	repo := repositories.NewRepository(db)
	ctx, _, project := testutils.ScopedProject(t, repo)
	_, _, elsewhere := testutils.ScopedProject(t, repo)
	sibling := uuid.Must(uuid.NewV7())
	ex := testutils.Executor(db)
	home, found, err := projectstore.New().IDEq(project).One(ctx, ex)
	if err != nil || !found {
		t.Fatalf("read the organization: %v (found=%v)", err, found)
	}
	sib := projectstore.Create()
	sib.SetID(sibling)
	sib.SetOrganizationID(home.OrganizationID)
	sib.SetName("Sibling")
	if _, err := sib.Insert(ctx, ex); err != nil {
		t.Fatalf("seed the sibling project: %v", err)
	}

	now := time.Now().UTC().Truncate(time.Second)
	saved := now.Add(-3 * time.Hour)
	empty := runtime.JSON("[]")
	def := func(project uuid.UUID, key string, version int, required string, gone bool) {
		t.Helper()
		row := decisiondefinition.Row{
			ID: uuid.Must(uuid.NewV7()), CreatedAt: saved.Add(time.Duration(version) * time.Minute), UpdatedAt: saved,
			ProjectID: project, Key: key, Name: fmt.Sprintf("%s v%d", key, version), Version: int64(version), HitPolicy: "FIRST",
			RequiredDecisions: runtime.JSON(required), Inputs: empty, Outputs: empty, Rules: empty, Tests: empty,
		}
		if gone {
			row.DeletedAt = runtime.Null[time.Time]{V: now, Valid: true}
		}
		if _, err := decisiondefinition.InsertAll(ctx, ex, []decisiondefinition.Row{row}); err != nil {
			t.Fatalf("seed %s v%d: %v", key, version, err)
		}
	}
	release := func(project uuid.UUID, key string, version int, at time.Time, gone bool) {
		t.Helper()
		stamp := time.Now()
		row := decisionrelease.Row{ID: uuid.Must(uuid.NewV7()), CreatedAt: stamp, UpdatedAt: stamp,
			ProjectID: project, DecisionKey: key, ActivateAt: at, Version: int64(version)}
		if gone {
			row.DeletedAt = runtime.Null[time.Time]{V: now, Valid: true}
		}
		if _, err := decisionrelease.InsertAll(ctx, ex, []decisionrelease.Row{row}); err != nil {
			t.Fatalf("seed the release of %s v%d: %v", key, version, err)
		}
	}
	def(project, "alpha", 1, `[]`, false)
	def(project, "alpha", 2, `["beta"]`, false)
	def(project, "alpha", 3, `[]`, false)
	release(project, "alpha", 2, now.Add(-time.Hour), false)
	def(project, "beta", 1, `[]`, false)
	def(project, "beta", 2, `[]`, false)
	release(project, "beta", 1, now.Add(24*time.Hour), false)
	def(project, "gamma", 1, `[]`, false)
	def(project, "gamma", 2, `[]`, true)
	release(project, "gamma", 2, now.Add(-time.Hour), false)
	def(project, "delta", 1, `[]`, false)
	def(project, "delta", 2, `[]`, false)
	release(project, "delta", 2, now.Add(-2*time.Hour), false)
	release(project, "delta", 1, now.Add(-time.Hour), false)
	def(project, "eps_100%", 1, `[]`, false)
	def(project, "eps_100%", 2, `[]`, false)
	release(project, "eps_100%", 1, now.Add(-time.Hour), true)
	def(sibling, "alpha", 1, `[]`, false)
	def(elsewhere, "alpha", 1, `[]`, false)

	list := func(projectID uuid.UUID, search string, p contracts.Pagination) contracts.Page[models.DecisionSummaryModel] {
		t.Helper()
		page, err := repo.Decision().ListKeysByProject(ctx, projectID, search, p)
		if err != nil {
			t.Fatalf("list %q: %v", search, err)
		}
		return page
	}
	describe := func(items []models.DecisionSummaryModel) string {
		var out []string
		for _, m := range items {
			out = append(out, fmt.Sprintf("%s:%s/live %d/newest %d/needs %v", m.Key, m.Name, m.LiveVersion, m.NewestVersion, m.RequiredDecisions))
		}
		return strings.Join(out, " ")
	}

	page := list(project, "", contracts.Pagination{})
	want := "alpha:alpha v2/live 2/newest 3/needs [beta] beta:beta v2/live 0/newest 2/needs [] " +
		"delta:delta v1/live 1/newest 2/needs [] eps_100%:eps_100% v2/live 0/newest 2/needs [] " +
		"gamma:gamma v1/live 0/newest 1/needs []"
	if got := describe(page.Items); got != want || page.Total != 5 {
		t.Errorf("keys (%d):\n got %s\nwant %s", page.Total, got, want)
	}
	if len(page.Items) == 5 {
		if got := page.Items[0].LastChangedAt.UTC(); !got.Equal(now.Add(-time.Hour)) {
			t.Errorf("alpha last changed %v, want its release at %v", got, now.Add(-time.Hour))
		}
		if got := page.Items[1].LastChangedAt.UTC(); !got.Equal(saved.Add(2 * time.Minute)) {
			t.Errorf("beta last changed %v, want its newest save at %v", got, saved.Add(2*time.Minute))
		}
	}

	page = list(project, "", contracts.Pagination{Page: 2, PageSize: 2})
	if got := describe(page.Items); !strings.HasPrefix(got, "delta:") || !strings.Contains(got, " eps_100%:") || len(page.Items) != 2 || page.Total != 5 {
		t.Errorf("page 2 (%d): %s", page.Total, got)
	}

	for search, keys := range map[string]string{
		"V2":   "alpha beta eps_100%",
		"v3":   "",
		"100%": "eps_100%",
		"%":    "eps_100%",
		"gam":  "gamma",
	} {
		page := list(project, search, contracts.Pagination{})
		var got []string
		for _, m := range page.Items {
			got = append(got, m.Key)
		}
		if strings.Join(got, " ") != keys || page.Total != int64(len(got)) {
			t.Errorf("search %q: %v (%d), want %q", search, got, page.Total, keys)
		}
	}

	page = list(uuid.Nil, "", contracts.Pagination{})
	if page.Total != 6 || len(page.Items) != 6 || page.Items[1].Key != "alpha" || uuid.UUID(page.Items[1].ProjectID) == uuid.UUID(page.Items[0].ProjectID) {
		t.Errorf("every project the caller may see (%d): %s", page.Total, describe(page.Items))
	}
}
