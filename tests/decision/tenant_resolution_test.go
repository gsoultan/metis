package decision_test

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/gsoultan/metis/server/domains/entities"
	observersimpl "github.com/gsoultan/metis/server/domains/observers/impl"
	"github.com/gsoultan/metis/server/domains/services"
	"github.com/gsoultan/metis/server/repositories"
	"github.com/gsoultan/metis/server/repositories/models"
	"github.com/gsoultan/metis/tests/testutils"
)

// A business rule task names its table by key, and keys are unique per
// project, not per installation. The lookup was bounded only by the caller's
// tenant scope — every project of the organization for a request, and nothing
// at all under the system identity the job worker, timers and message
// dispatch run with. So a business rule task reached after a timer evaluated
// whichever tenant's table with that key had the highest version, and wrote
// that tenant's answer into this instance. DMN 1.3 §6.3.2: a decision is
// invoked from the model that references it — here, the process's project.

type decisionWorld struct {
	svc  services.ServiceFacade
	repo repositories.Repository
}

func newDecisionWorld(t *testing.T) decisionWorld {
	t.Helper()
	db := testutils.SetupTestDB(t)
	repo := repositories.NewRepository(testutils.StormConn(db))
	svc := services.NewServiceFacade(repo, observersimpl.NewEventDispatcher(), observersimpl.NewSSEObserver(),
		"decision-resolution-secret", nil, nil, nil)
	return decisionWorld{svc: svc, repo: repo}
}

// project seeds an organization with one project, and returns a request
// context in that organization with the project's id.
func (w decisionWorld) project(t *testing.T, org string) (context.Context, uuid.UUID) {
	t.Helper()
	orgID := uuid.Must(uuid.NewV7())
	if err := w.repo.Organization().Create(entities.WithSystemContext(t.Context()), models.OrganizationModel{
		Base: models.Base{ID: models.UUID(orgID)}, Name: org,
	}); err != nil {
		t.Fatalf("seed %s: %v", org, err)
	}
	ctx := entities.WithTenantContext(t.Context(), entities.TenantContext{TenantID: orgID.String()})
	return ctx, w.anotherProject(t, ctx, orgID, org+" project")
}

func (w decisionWorld) anotherProject(t *testing.T, ctx context.Context, orgID uuid.UUID, name string) uuid.UUID {
	t.Helper()
	p, err := w.svc.CreateProject(ctx, orgID, name, "")
	if err != nil {
		t.Fatalf("create %s: %v", name, err)
	}
	return p.ID
}

// bandTable answers every applicant with the same band, so which table ran is
// visible in the result.
func (w decisionWorld) bandTable(t *testing.T, ctx context.Context, project uuid.UUID, band string) {
	t.Helper()
	if _, err := w.svc.CreateDecision(ctx, entities.DecisionDefinition{
		ID:      uuid.Must(uuid.NewV7()),
		Project: &entities.Project{ID: project},
		Key:     "credit-band",
		Name:    "Credit band",
		Inputs:  []entities.DecisionInput{{ID: "in", Label: "Score", Expression: "score", Type: "number"}},
		Outputs: []entities.DecisionOutput{{ID: "out", Label: "Band", Name: "band", Type: "string"}},
		Rules:   []entities.DecisionRule{{ID: "any", Inputs: []string{"-"}, Outputs: []any{band}}},
	}); err != nil {
		t.Fatalf("create the %s table: %v", band, err)
	}
}

func (w decisionWorld) ruleProcess(t *testing.T, ctx context.Context, project uuid.UUID) {
	t.Helper()
	def := entities.ProcessDefinition{
		Project: &entities.Project{ID: project},
		Key:     "credit-check",
		Name:    "Credit check",
		Nodes: []*entities.Node{
			{ID: "start", Type: entities.StartEvent, Outgoing: []string{"f1"}},
			{ID: "decide", Type: entities.BusinessRuleTask, Incoming: []string{"f1"}, Outgoing: []string{"f2"},
				Properties: map[string]any{"decision_key": "credit-band"}},
			{ID: "end", Type: entities.EndEvent, Incoming: []string{"f2"}},
		},
		Flows: []*entities.SequenceFlow{
			{ID: "f1", SourceRef: "start", TargetRef: "decide"},
			{ID: "f2", SourceRef: "decide", TargetRef: "end"},
		},
	}
	if _, err := w.svc.CreateDefinition(ctx, &def); err != nil {
		t.Fatalf("deploy the process: %v", err)
	}
}

func (w decisionWorld) bandDecidedFor(t *testing.T, ctx context.Context, project uuid.UUID) any {
	t.Helper()
	id, err := w.svc.StartProcess(ctx, project, "credit-check", map[string]any{"score": 700})
	if err != nil {
		t.Fatalf("start: %v", err)
	}
	instance, err := w.svc.GetInstance(entities.WithSystemContext(ctx), id)
	if err != nil {
		t.Fatalf("read the instance: %v", err)
	}
	return instance.Variables["band"]
}

func TestABusinessRuleTaskRunByTheSystemUsesItsOwnTenantsTable(t *testing.T) {
	w := newDecisionWorld(t)

	ctxA, projectA := w.project(t, "Tenant A")
	w.bandTable(t, ctxA, projectA, "A's rules")
	w.ruleProcess(t, ctxA, projectA)

	// Another tenant uses the same key, and has revised its table more often,
	// so its version outranks A's.
	ctxB, projectB := w.project(t, "Tenant B")
	for range 3 {
		w.bandTable(t, ctxB, projectB, "B's rules")
	}

	// The job worker, timers and message dispatch all run as the system.
	if got := w.bandDecidedFor(t, entities.WithSystemContext(t.Context()), projectA); got != "A's rules" {
		t.Fatalf("a process in tenant A was decided by %v", got)
	}
}

func TestABusinessRuleTaskUsesItsOwnProjectsTable(t *testing.T) {
	w := newDecisionWorld(t)

	ctx, projectOne := w.project(t, "One organization")
	orgID, _ := uuid.Parse(mustTenant(t, ctx))
	projectTwo := w.anotherProject(t, ctx, orgID, "Second project")

	w.bandTable(t, ctx, projectOne, "project one's rules")
	w.ruleProcess(t, ctx, projectOne)
	for range 2 {
		w.bandTable(t, ctx, projectTwo, "project two's rules")
	}

	if got := w.bandDecidedFor(t, ctx, projectOne); got != "project one's rules" {
		t.Fatalf("a process in project one was decided by %v", got)
	}
}

func mustTenant(t *testing.T, ctx context.Context) string {
	t.Helper()
	tenant, ok := entities.TenantContextFrom(ctx)
	if !ok {
		t.Fatal("the context names no organization")
	}
	return tenant.TenantID
}
