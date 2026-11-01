package release_test

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/gsoultan/metis/internal/pkg/apierr"
	pkgauth "github.com/gsoultan/metis/internal/pkg/auth"
	"github.com/gsoultan/metis/server/domains/entities"
	"github.com/gsoultan/metis/server/domains/services/contracts"
)

func as(ctx context.Context, username string) context.Context {
	return context.WithValue(ctx, pkgauth.UserContextKey, entities.User{
		ID: uuid.New(), Username: username, Roles: []string{entities.RoleReleaser},
	})
}

func (w world) cutAndImport(t *testing.T, dev, prod context.Context) {
	t.Helper()
	if _, err := w.svc.CutRelease(dev, contracts.CutRelease{ProjectID: w.projectID, Name: "r1", Processes: []string{"claims"}}); err != nil {
		t.Fatalf("cut: %v", err)
	}
	if _, err := w.svc.ImportReleaseFrom(prod, w.projectID, uuid.Nil, "r1"); err != nil {
		t.Fatalf("import: %v", err)
	}
}

// A decision table that fails its own tests does not go live, and nothing of
// the release does.
func TestAReleaseWhoseDecisionFailsItsTestsDoesNotGoLive(t *testing.T) {
	w := newWorld(t)
	if _, err := w.svc.CreateDecision(w.dev, entities.DecisionDefinition{
		Project: &entities.Project{ID: w.projectID}, Key: "limit", Name: "Limit", HitPolicy: "FIRST",
		Outputs: []entities.DecisionOutput{{ID: "o", Label: "Limit", Name: "limit", Type: "string"}},
		Rules:   []entities.DecisionRule{{ID: "r", Outputs: []any{"low"}}},
		Tests:   []entities.DecisionTest{{ID: "t", Name: "big claims", Expected: map[string]any{"limit": "high"}}},
	}); err != nil {
		t.Fatalf("deploy the decision: %v", err)
	}
	w.deployProcess(t, w.dev, "Claims", nil)
	w.cutAndImport(t, w.dev, w.prod)

	_, err := w.svc.GoLive(w.prod, w.projectID, "r1")
	if !errors.Is(err, apierr.ErrInvalidArgument) || !strings.Contains(err.Error(), "big claims") {
		t.Fatalf("go live with a failing decision test got %v, want a refusal naming the test", err)
	}
	if name, _ := w.liveProcessName(t, w.prod); name != "" {
		t.Fatalf("production runs %q after the refused go-live", name)
	}
	if _, err := w.svc.Evaluate(w.prod, w.projectID, "limit", 0, nil); err == nil {
		t.Fatal("the refused release's decision is in production")
	}
}

// A process that does not end a saved simulation case as it expects does not
// go live; one that does, does.
func TestAReleaseIsHeldToItsProcessesSavedSimulationCases(t *testing.T) {
	w := newWorld(t)
	w.deployDecision(t, w.dev, "low")
	w.deployProcess(t, w.dev, "Claims", nil)
	saved, err := w.svc.SaveSimulationCase(w.dev, entities.SimulationCase{
		Project: &entities.Project{ID: w.projectID}, ProcessKey: "claims", Name: "ends elsewhere",
		Scenario: entities.SimulationScenario{Variables: map[string]any{}},
		Expect:   entities.SimulationExpectation{Outcome: entities.SimulationOutcomeCompleted, EndsAt: "elsewhere"},
	})
	if err != nil {
		t.Fatalf("save the case: %v", err)
	}
	w.cutAndImport(t, w.dev, w.prod)

	_, err = w.svc.GoLive(w.prod, w.projectID, "r1")
	if !errors.Is(err, apierr.ErrInvalidArgument) || !strings.Contains(err.Error(), "ends elsewhere") {
		t.Fatalf("go live against a failing case got %v, want a refusal naming the case", err)
	}
	if name, _ := w.liveProcessName(t, w.prod); name != "" {
		t.Fatalf("production runs %q after the refused go-live", name)
	}

	saved.Expect.EndsAt = "end"
	if _, err := w.svc.SaveSimulationCase(w.dev, saved); err != nil {
		t.Fatalf("correct the case: %v", err)
	}
	if _, err := w.svc.GoLive(w.prod, w.projectID, "r1"); err != nil {
		t.Fatalf("go live against a passing case: %v", err)
	}
	if name, _ := w.liveProcessName(t, w.prod); name != "Claims" {
		t.Fatalf("production runs %q, want the release's process", name)
	}
}

// In a protected environment a release goes live once a second person, who
// neither cut nor imported it, has approved it.
func TestAProtectedEnvironmentNeedsASecondPersonToApprove(t *testing.T) {
	w := newWorldProtected(t, true)
	w.deployDecision(t, w.dev, "low")
	w.deployProcess(t, w.dev, "Claims", nil)
	alice, bob := as(w.prod, "alice"), as(w.prod, "bob")
	w.cutAndImport(t, w.dev, alice)

	if _, err := w.svc.GoLive(alice, w.projectID, "r1"); !errors.Is(err, apierr.ErrInvalidArgument) || !strings.Contains(err.Error(), "approved") {
		t.Fatalf("go live unapproved got %v, want a refusal asking for approval", err)
	}
	if _, err := w.svc.ApproveRelease(alice, w.projectID, "r1"); !errors.Is(err, apierr.ErrInvalidArgument) {
		t.Fatalf("the importer approving their own release got %v, want a refusal", err)
	}
	approved, err := w.svc.ApproveRelease(bob, w.projectID, "r1")
	if err != nil {
		t.Fatalf("bob approves: %v", err)
	}
	if approved.ApprovedBy != "bob" || approved.ImportedBy != "alice" {
		t.Fatalf("approved by %q, imported by %q; want bob and alice", approved.ApprovedBy, approved.ImportedBy)
	}
	if _, err := w.svc.GoLive(alice, w.projectID, "r1"); err != nil {
		t.Fatalf("go live once approved: %v", err)
	}
	if name, _ := w.liveProcessName(t, w.prod); name != "Claims" {
		t.Fatalf("production runs %q, want the release's process", name)
	}
}
