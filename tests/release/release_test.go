package release_test

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/gsoultan/metis/internal/app"
	"github.com/gsoultan/metis/internal/pkg/apierr"
	"github.com/gsoultan/metis/server/domains/entities"
	"github.com/gsoultan/metis/server/domains/observers/impl"
	"github.com/gsoultan/metis/server/domains/services"
	"github.com/gsoultan/metis/server/domains/services/contracts"
	"github.com/gsoultan/metis/server/repositories"
	"github.com/gsoultan/metis/server/repositories/db"
	"github.com/gsoultan/metis/server/repositories/models"
	"github.com/gsoultan/metis/tests/testutils"
)

// world is a development runtime (the installation's own) and a production
// environment beside it, one project in both.
type world struct {
	svc       services.ServiceFacade
	dev, prod context.Context
	projectID uuid.UUID
	conn      *db.Conn
}

func newWorld(t *testing.T) world {
	t.Helper()
	conn := testutils.SetupTestDB(t)
	prodDB := testutils.SetupTestDB(t)
	prodID := uuid.New()
	conn.RegisterEnvironment(prodID, prodDB.Main())
	repo := repositories.NewRepository(conn)
	sse := impl.NewSSEObserver()
	svc := services.NewServiceFacade(repo, impl.NewEventDispatcher(), sse, "release-test", nil, nil, nil)

	dev, orgID, projectID := testutils.ScopedProject(t, repo)
	if err := app.SeedEnvironmentIdentity(t.Context(), prodDB.Main(),
		models.ProjectModel{Base: models.Base{ID: models.FromUUID(projectID)}, OrganizationID: models.FromUUID(orgID), Name: "P"},
		models.OrganizationModel{Base: models.Base{ID: models.FromUUID(orgID)}, Name: "O"}); err != nil {
		t.Fatalf("seed production's identity rows: %v", err)
	}
	return world{svc: svc, dev: dev, prod: db.Bind(dev, prodID), projectID: projectID, conn: conn}
}

func (w world) deployDecision(t *testing.T, ctx context.Context, answer string) {
	t.Helper()
	if _, err := w.svc.CreateDecision(ctx, entities.DecisionDefinition{
		Project: &entities.Project{ID: w.projectID}, Key: "limit", Name: "Limit", HitPolicy: "FIRST",
		Outputs: []entities.DecisionOutput{{ID: "o", Label: "Limit", Name: "limit", Type: "string"}},
		Rules:   []entities.DecisionRule{{ID: "r", Outputs: []any{answer}}},
	}); err != nil {
		t.Fatalf("deploy the decision: %v", err)
	}
}

func (w world) deployProcess(t *testing.T, ctx context.Context, name string, extra map[string]any) {
	t.Helper()
	props := map[string]any{"decision_key": "limit"}
	for k, v := range extra {
		props[k] = v
	}
	if _, err := w.svc.CreateDefinition(ctx, &entities.ProcessDefinition{
		Project: &entities.Project{ID: w.projectID}, Key: "claims", Name: name,
		Nodes: []*entities.Node{
			{ID: "start", Type: entities.StartEvent, Name: "Start"},
			{ID: "decide", Type: entities.BusinessRuleTask, Name: "Decide", Properties: props},
			{ID: "end", Type: entities.EndEvent, Name: "End"},
		},
		Flows: []*entities.SequenceFlow{
			{ID: "f1", SourceRef: "start", TargetRef: "decide"},
			{ID: "f2", SourceRef: "decide", TargetRef: "end"},
		},
	}); err != nil {
		t.Fatalf("deploy the process: %v", err)
	}
}

func (w world) liveProcessName(t *testing.T, ctx context.Context) (string, int) {
	t.Helper()
	versions, err := w.svc.ListDefinitionVersions(ctx, w.projectID, "claims")
	if err != nil {
		t.Fatalf("list versions: %v", err)
	}
	for _, v := range versions {
		if v.Live {
			return v.Name, v.Version
		}
	}
	return "", 0
}

func (w world) liveDecisionAnswer(t *testing.T, ctx context.Context) string {
	t.Helper()
	res, err := w.svc.Evaluate(ctx, w.projectID, "limit", 0, map[string]any{})
	if err != nil {
		t.Fatalf("evaluate the live decision: %v", err)
	}
	got, _ := res.Values["limit"].(string)
	return got
}

// Designed and tested in development, a release reaches production staged —
// stored, not running — and goes live there in one step, and production can
// return to the release before it.
func TestAReleaseIsStagedInProductionThenGoesLiveTogetherAndRollsBack(t *testing.T) {
	w := newWorld(t)
	cut := func(name string) {
		t.Helper()
		if _, err := w.svc.CutRelease(w.dev, contracts.CutRelease{ProjectID: w.projectID, Name: name, Processes: []string{"claims"}}); err != nil {
			t.Fatalf("cut %s: %v", name, err)
		}
	}
	importIt := func(name string) entities.Release {
		t.Helper()
		got, err := w.svc.ImportReleaseFrom(w.prod, w.projectID, uuid.Nil, name)
		if err != nil {
			t.Fatalf("import %s: %v", name, err)
		}
		return got
	}

	w.deployDecision(t, w.dev, "low")
	w.deployProcess(t, w.dev, "Claims one", nil)
	cut("r1")

	// The keys are new in production, so importing stores nothing that runs.
	staged := importIt("r1")
	if staged.State != entities.ReleaseStaged {
		t.Fatalf("imported release is %s, want staged", staged.State)
	}
	if name, _ := w.liveProcessName(t, w.prod); name != "" {
		t.Fatalf("production runs %q before the release went live", name)
	}
	if again := importIt("r1"); again.ID != staged.ID {
		t.Fatal("importing the same release twice made a second one")
	}
	if _, err := w.svc.GoLive(w.prod, w.projectID, "r1"); err != nil {
		t.Fatalf("go live r1: %v", err)
	}
	if name, _ := w.liveProcessName(t, w.prod); name != "Claims one" {
		t.Fatalf("production runs %q, want r1's process", name)
	}
	if got := w.liveDecisionAnswer(t, w.prod); got != "low" {
		t.Fatalf("production's decision answers %q, want r1's", got)
	}

	// A second release stages beside the live one and replaces it together.
	w.deployDecision(t, w.dev, "high")
	w.deployProcess(t, w.dev, "Claims two", nil)
	cut("r2")
	importIt("r2")
	if name, _ := w.liveProcessName(t, w.prod); name != "Claims one" {
		t.Fatalf("staging r2 changed what production runs to %q", name)
	}
	if got := w.liveDecisionAnswer(t, w.prod); got != "low" {
		t.Fatalf("staging r2 changed production's decision to %q", got)
	}
	live, err := w.svc.GoLive(w.prod, w.projectID, "r2")
	if err != nil {
		t.Fatalf("go live r2: %v", err)
	}
	if live.Previous != "r1" {
		t.Fatalf("r2 went live after %q, want r1", live.Previous)
	}
	if name, _ := w.liveProcessName(t, w.prod); name != "Claims two" {
		t.Fatalf("production runs %q, want r2's process", name)
	}
	if got := w.liveDecisionAnswer(t, w.prod); got != "high" {
		t.Fatalf("production's decision answers %q, want r2's", got)
	}

	back, err := w.svc.RollBack(w.prod, w.projectID)
	if err != nil {
		t.Fatalf("roll back: %v", err)
	}
	if back.Name != "r1" {
		t.Fatalf("rolled back to %s, want r1", back.Name)
	}
	if name, _ := w.liveProcessName(t, w.prod); name != "Claims one" {
		t.Fatalf("after the rollback production runs %q, want r1's process", name)
	}
	if got := w.liveDecisionAnswer(t, w.prod); got != "low" {
		t.Fatalf("after the rollback production's decision answers %q, want r1's", got)
	}
	r2, err := w.svc.GetRelease(w.prod, w.projectID, "r2")
	if err != nil || r2.State != entities.ReleaseSuperseded {
		t.Fatalf("r2 after the rollback: %v %v, want superseded", r2.State, err)
	}

	// Development was never changed by any of it.
	if name, _ := w.liveProcessName(t, w.dev); name != "Claims two" {
		t.Fatalf("development runs %q, want its own latest", name)
	}
}

// A release that calls a connection production does not have is refused at
// import, naming it, and nothing of it is stored.
func TestAReleaseIsRefusedWhereItsConnectionIsMissing(t *testing.T) {
	w := newWorld(t)
	w.deployDecision(t, w.dev, "low")
	w.deployProcess(t, w.dev, "Claims", map[string]any{entities.ConnectionProperty: "payments-api"})
	if _, err := w.svc.CutRelease(w.dev, contracts.CutRelease{ProjectID: w.projectID, Name: "r1", Processes: []string{"claims"}}); err != nil {
		t.Fatalf("cut: %v", err)
	}
	_, err := w.svc.ImportReleaseFrom(w.prod, w.projectID, uuid.Nil, "r1")
	if !errors.Is(err, apierr.ErrInvalidArgument) || !strings.Contains(err.Error(), "payments-api") {
		t.Fatalf("import without the connection got %v, want a refusal naming payments-api", err)
	}
	if _, err := w.svc.GetRelease(w.prod, w.projectID, "r1"); !errors.Is(err, apierr.ErrNotFound) {
		t.Fatalf("the refused release is recorded in production: %v", err)
	}
}

// A step that pins a version number is refused at cut: the number would name
// another model where the release arrives.
func TestAReleaseRefusesAPinnedVersion(t *testing.T) {
	w := newWorld(t)
	w.deployDecision(t, w.dev, "low")
	w.deployProcess(t, w.dev, "Claims", map[string]any{"decision_version": float64(1)})
	_, err := w.svc.CutRelease(w.dev, contracts.CutRelease{ProjectID: w.projectID, Name: "r1", Processes: []string{"claims"}})
	if !errors.Is(err, apierr.ErrInvalidArgument) || !strings.Contains(err.Error(), "decide") {
		t.Fatalf("cut with a pinned version got %v, want a refusal naming the step", err)
	}
}

// A release cut in development cannot be made live there: it is what already
// runs there.
func TestAReleaseDoesNotGoLiveWhereItWasCut(t *testing.T) {
	w := newWorld(t)
	w.deployDecision(t, w.dev, "low")
	w.deployProcess(t, w.dev, "Claims", nil)
	rel, err := w.svc.CutRelease(w.dev, contracts.CutRelease{ProjectID: w.projectID, Name: "r1", Processes: []string{"claims"}})
	if err != nil {
		t.Fatalf("cut: %v", err)
	}
	if len(rel.Items) != 2 || len(rel.Bundle.Decisions) != 1 {
		t.Fatalf("the release carries %+v, want the process and the decision it consults", rel.Items)
	}
	if _, err := w.svc.GoLive(w.dev, w.projectID, "r1"); !errors.Is(err, apierr.ErrInvalidArgument) {
		t.Fatalf("go live where it was cut got %v, want a refusal", err)
	}
}

// A release changed after it was cut no longer matches its hash, and is not
// imported: what reaches production is what was tested.
func TestAReleaseChangedAfterItWasCutIsNotImported(t *testing.T) {
	w := newWorld(t)
	w.deployDecision(t, w.dev, "low")
	w.deployProcess(t, w.dev, "Claims", nil)
	if _, err := w.svc.CutRelease(w.dev, contracts.CutRelease{ProjectID: w.projectID, Name: "r1", Processes: []string{"claims"}}); err != nil {
		t.Fatalf("cut: %v", err)
	}
	if _, err := w.conn.Main().Exec(t.Context(),
		`UPDATE releases SET bundle = jsonb_set(bundle, '{decisions,0,rules,0,outputs,0}', '"unlimited"')`); err != nil {
		t.Fatalf("change the release: %v", err)
	}
	_, err := w.svc.ImportReleaseFrom(w.prod, w.projectID, uuid.Nil, "r1")
	if !errors.Is(err, apierr.ErrInvalidArgument) || !strings.Contains(err.Error(), "changed") {
		t.Fatalf("import of a changed release got %v, want a refusal", err)
	}
}
