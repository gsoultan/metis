package sqlconnector_test

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/gsoultan/metis/internal/pkg/apierr"
	"github.com/gsoultan/metis/server/domains/entities"
	"github.com/gsoultan/metis/server/domains/services/contracts"
	serviceimpl "github.com/gsoultan/metis/server/domains/services/impl"
	"github.com/gsoultan/metis/server/repositories"
	"github.com/gsoultan/metis/tests/testutils"
)

// A lookup carries SQL its author wrote, run with whatever the connection's
// login may read, so deploying one is a permission of its own. Every case here
// deploys through CreateDefinition, which is the path the definition endpoint
// and a deployment's resources both take.
func TestWhoMayDeployALookup(t *testing.T) {
	for name, tc := range map[string]struct {
		ctx     func(context.Context) context.Context
		allowed bool
	}{
		"a designer who may author queries": {func(c context.Context) context.Context {
			return as(c, entities.RoleDesigner, entities.RoleQueryAuthor)
		}, true},
		"an administrator": {func(c context.Context) context.Context { return as(c, entities.RoleAdmin) }, true},
		"a designer alone": {func(c context.Context) context.Context { return as(c, entities.RoleDesigner) }, false},
		"an operator":      {func(c context.Context) context.Context { return as(c, entities.RoleOperator) }, false},
		// Absent constraint means deny: a request that says who it is from is
		// the only kind that can hold a role.
		"nobody": {func(c context.Context) context.Context { return c }, false},
		// Nor is work marked as the system's own a way round it.
		"system work": {entities.WithSystemContext, false},
	} {
		t.Run(name, func(t *testing.T) {
			ctx, deploy := deployer(t)
			err := deploy(tc.ctx(ctx), definitionWith(lookupStep("lookup", "SELECT 1 AS one", "answer")))
			switch {
			case tc.allowed && err != nil:
				t.Fatalf("refused: %v", err)
			case !tc.allowed && !errors.Is(err, apierr.ErrForbidden):
				t.Fatalf("got %v, want a refusal", err)
			}
		})
	}
}

// Moving the step into a sub-process must not move it out of the check.
func TestALookupInsideASubProcessIsStillALookup(t *testing.T) {
	ctx, deploy := deployer(t)
	inner := lookupStep("inner-lookup", "SELECT 1 AS one", "answer")
	sub := &entities.Node{
		ID: "sub", Type: entities.SubProcess, Name: "Check the customer",
		Nodes: []*entities.Node{
			{ID: "sub-start", Type: entities.StartEvent},
			inner,
			{ID: "sub-end", Type: entities.EndEvent},
		},
		Flows: []*entities.SequenceFlow{
			{ID: "s1", SourceRef: "sub-start", TargetRef: "inner-lookup"},
			{ID: "s2", SourceRef: "inner-lookup", TargetRef: "sub-end"},
		},
	}
	if err := deploy(as(ctx, entities.RoleDesigner), definitionWith(sub)); !errors.Is(err, apierr.ErrForbidden) {
		t.Fatalf("a designer deployed a lookup nested in a sub-process: %v", err)
	}
}

// Everything else deploys exactly as it did.
func TestAProcessWithNoLookupNeedsNoNewRole(t *testing.T) {
	ctx, deploy := deployer(t)
	step := &entities.Node{ID: "notify", Type: entities.ServiceTask, Name: "Notify", Properties: map[string]any{
		"http_url": "https://example.com/hook",
	}}
	if err := deploy(as(ctx, entities.RoleDesigner), definitionWith(step)); err != nil {
		t.Fatalf("a process with no lookup now needs more than a designer: %v", err)
	}
}

// For somebody who may deploy one, a lookup that cannot be right on any server
// is refused at deploy, naming the step, rather than failing as an incident.
func TestALookupThatCannotBeRightIsRefusedAtDeploy(t *testing.T) {
	for name, step := range map[string]*entities.Node{
		"a write":               lookupStep("tidy", "DELETE FROM customers", "answer"),
		"two statements":        lookupStep("tidy", "SELECT 1; SELECT 2", "answer"),
		"no variable to answer": lookupStep("tidy", "SELECT 1 AS one", ""),
	} {
		t.Run(name, func(t *testing.T) {
			ctx, deploy := deployer(t)
			err := deploy(as(ctx, entities.RoleQueryAuthor), definitionWith(step))
			if !errors.Is(err, apierr.ErrInvalidArgument) || !strings.Contains(err.Error(), "Tidy up") {
				t.Fatalf("got %v, want a refusal naming the step", err)
			}
		})
	}
}

// deployer is a project to deploy into, and the deploy path every caller takes.
func deployer(t *testing.T) (context.Context, func(context.Context, *entities.ProcessDefinition) error) {
	t.Helper()
	repo := repositories.NewRepository(testutils.SetupTestConn(t))
	ctx, _, projectID := testutils.ScopedProject(t, repo)
	defSvc := serviceimpl.NewDefinitionService(repo)
	return ctx, func(ctx context.Context, def *entities.ProcessDefinition) error {
		def.Project = &entities.Project{ID: projectID}
		_, err := defSvc.CreateDefinition(ctx, def)
		return err
	}
}

func lookupStep(id, statement, resultVariable string) *entities.Node {
	return &entities.Node{ID: id, Type: entities.ServiceTask, Name: "Tidy up", Properties: map[string]any{
		"connector_id":                   uuid.NewString(),
		contracts.StatementProperty:      statement,
		contracts.ResultVariableProperty: resultVariable,
	}}
}

// definitionWith is start → step → end.
func definitionWith(step *entities.Node) *entities.ProcessDefinition {
	return &entities.ProcessDefinition{
		Key:  "permission-" + uuid.NewString()[:8],
		Name: "Permission check",
		Nodes: []*entities.Node{
			{ID: "start", Type: entities.StartEvent},
			step,
			{ID: "end", Type: entities.EndEvent},
		},
		Flows: []*entities.SequenceFlow{
			{ID: "f1", SourceRef: "start", TargetRef: step.ID},
			{ID: "f2", SourceRef: step.ID, TargetRef: "end"},
		},
	}
}
