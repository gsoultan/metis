package impl

import (
	"context"
	"errors"
	"reflect"
	"testing"

	"github.com/google/uuid"

	"github.com/gsoultan/metis/internal/pkg/apierr"
	pkgauth "github.com/gsoultan/metis/internal/pkg/auth"
	"github.com/gsoultan/metis/server/domains/entities"
	servicecontracts "github.com/gsoultan/metis/server/domains/services/contracts"
)

func withRoles(ctx context.Context, roles ...string) context.Context {
	return context.WithValue(ctx, pkgauth.UserContextKey, entities.User{ID: uuid.New(), Roles: roles})
}

// Trying a step runs it against the connection its project saved — the caller
// supplies what is sent, never where it goes — with its mappings, and answers
// with what the step would store.
func TestTryingAStepRunsItAgainstTheProjectsSavedConnection(t *testing.T) {
	executor := &answeringExecutor{reply: map[string]any{"credit_score": 42.0, "status": "active"}}
	svc, def, ctx, connectorID := connectorUnderTest(t, executor, map[string]any{"url": "https://partner.example.com"})

	stored, err := svc.TryConnectorStep(withRoles(ctx, entities.RoleDesigner), def.Project.ID, entities.Node{
		ID: "company", Type: entities.ServiceTask, Properties: map[string]any{
			"connector_id":   connectorID,
			"input_mapping":  map[string]any{"company_number": "companyNumber"},
			"output_mapping": map[string]any{"creditScore": "credit_score"},
			// A step cannot redirect its connection by naming a setting.
			"url": "https://attacker.example.com",
		},
	}, map[string]any{"companyNumber": "09876543", "supplierName": "Northwind"})
	if err != nil {
		t.Fatalf("try: %v", err)
	}
	if executor.config["url"] != "https://partner.example.com" {
		t.Errorf("the step ran against %v, not its project's saved connection", executor.config["url"])
	}
	if !reflect.DeepEqual(executor.sent, map[string]any{"company_number": "09876543"}) {
		t.Errorf("sent %v", executor.sent)
	}
	if !reflect.DeepEqual(stored, map[string]any{"creditScore": 42.0}) {
		t.Errorf("would store %v", stored)
	}
}

// A lookup runs SQL its author wrote, so trying one needs what deploying one
// needs.
func TestTryingALookupNeedsTheQueryAuthorRole(t *testing.T) {
	executor := &recordingRequestExecutor{}
	svc, def, ctx, connectorID := connectorUnderTest(t, executor, map[string]any{})
	step := entities.Node{ID: "lookup", Type: entities.ServiceTask, Properties: map[string]any{
		"connector_id":                          connectorID,
		servicecontracts.StatementProperty:      "SELECT 1 AS one",
		servicecontracts.ResultVariableProperty: "answer",
	}}

	if _, err := svc.TryConnectorStep(withRoles(ctx, entities.RoleDesigner), def.Project.ID, step, nil); !errors.Is(err, apierr.ErrForbidden) {
		t.Fatalf("a designer tried a lookup: %v", err)
	}
	if executor.request.Statement != "" {
		t.Fatal("the refused lookup reached its connector")
	}
	if _, err := svc.TryConnectorStep(withRoles(ctx, entities.RoleDesigner, entities.RoleQueryAuthor), def.Project.ID, step, nil); err != nil {
		t.Fatalf("a query author was refused: %v", err)
	}
	if executor.request.Statement != "SELECT 1 AS one" {
		t.Fatal("the query author's try did not reach the connector")
	}
}

func TestTryingAStepThatNamesNoConnectorSaysSo(t *testing.T) {
	svc, def, ctx, _ := connectorUnderTest(t, &answeringExecutor{}, map[string]any{})
	_, err := svc.TryConnectorStep(withRoles(ctx, entities.RoleDesigner), def.Project.ID,
		entities.Node{ID: "empty", Type: entities.ServiceTask, Properties: map[string]any{}}, nil)
	if !errors.Is(err, apierr.ErrInvalidArgument) {
		t.Fatalf("got %v", err)
	}
}
