package task_test

import (
	"bytes"
	"context"
	"net/http"
	"testing"

	"github.com/google/uuid"
	pkgauth "github.com/gsoultan/metis/internal/pkg/auth"
	"github.com/gsoultan/metis/server/domains/entities"
	"github.com/gsoultan/metis/server/repositories"
	"github.com/gsoultan/metis/server/repositories/models"
	"github.com/gsoultan/metis/tests/testutils"
)

// heldByAlice is a one-step approval the process gives to alice.
func heldByAlice() entities.Node {
	return entities.Node{
		Name: "Approve the refund", Type: entities.UserTask, Assignee: "alice",
		Properties: testutils.FormDeclaring("approved"),
	}
}

// raw sends a body exactly as written, for the requests a JSON encoder would
// never produce.
func (h *taskHarness) raw(t *testing.T, method, token, path, body string) (int, string) {
	t.Helper()
	req, err := http.NewRequestWithContext(context.Background(), method, h.server.URL+path, bytes.NewBufferString(body))
	if err != nil {
		t.Fatalf("build request: %v", err)
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("do request: %v", err)
	}
	defer resp.Body.Close()
	var out bytes.Buffer
	_, _ = out.ReadFrom(resp.Body)
	return resp.StatusCode, out.String()
}

// seedMember creates an account in the organization ctx names: somebody a
// task there can be handed to.
func seedMember(t *testing.T, repo repositories.Repository, ctx context.Context, username string) {
	t.Helper()
	orgID := testutils.OrgIDFrom(t, ctx)
	if err := repo.User().Create(ctx, models.UserModel{
		Base:          models.Base{ID: models.UUID(uuid.Must(uuid.NewV7()))},
		Username:      username,
		FullName:      username,
		Organizations: []models.OrganizationModel{{Base: models.Base{ID: models.UUID(orgID)}}},
	}, "hash"); err != nil {
		t.Fatalf("seed member %s: %v", username, err)
	}
}

// asAdministrator returns ctx carrying a signed-in administrator, the way the
// auth interceptor leaves a request.
func asAdministrator(ctx context.Context, username string) context.Context {
	return context.WithValue(ctx, pkgauth.UserContextKey,
		entities.User{Username: username, Roles: []string{entities.RoleAdmin}})
}

// seedCase seeds a running case whose definition has the one step "approve",
// for a seeded task to belong to: a hand-over reads the step a task was
// created from, and is not made for a task that has none.
func seedCase(t *testing.T, repo repositories.Repository, ctx context.Context, projectID uuid.UUID) *entities.ProcessInstance {
	t.Helper()
	definitionID, instanceID := uuid.Must(uuid.NewV7()), uuid.Must(uuid.NewV7())
	if err := repo.Definition().Create(ctx, models.ProcessDefinitionModel{
		Base: models.Base{ID: models.UUID(definitionID)}, ProjectID: models.UUID(projectID),
		Key: "refund", Name: "Refund", Version: 1,
		Nodes: []models.FlowNode{{ID: "approve", Name: "Approve the refund", Type: models.NodeType(entities.UserTask)}},
	}); err != nil {
		t.Fatalf("seed definition: %v", err)
	}
	if _, err := repo.Process().Create(ctx, models.ProcessInstanceModel{
		Base: models.Base{ID: models.UUID(instanceID)}, ProjectID: models.UUID(projectID),
		DefinitionID: models.UUID(definitionID), Status: models.ProcessActive,
	}); err != nil {
		t.Fatalf("seed instance: %v", err)
	}
	return &entities.ProcessInstance{ID: instanceID}
}
