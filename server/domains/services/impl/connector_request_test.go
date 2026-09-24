package impl

import (
	"context"
	"maps"
	"reflect"
	"testing"

	"github.com/google/uuid"
	"github.com/gsoultan/metis/server/domains/entities"
	servicecontracts "github.com/gsoultan/metis/server/domains/services/contracts"
)

// recordingExecutor is a connector that takes no request: it is called the way
// every executor was called before requests existed.
type recordingExecutor struct {
	config  map[string]any
	payload map[string]any
}

func (e *recordingExecutor) Execute(_ context.Context, config, payload map[string]any) (map[string]any, error) {
	e.config, e.payload = config, payload
	return map[string]any{}, nil
}

// recordingRequestExecutor takes one.
type recordingRequestExecutor struct {
	recordingExecutor
	request servicecontracts.ConnectorRequest
}

func (e *recordingRequestExecutor) ExecuteRequest(_ context.Context, config map[string]any, req servicecontracts.ConnectorRequest) (map[string]any, error) {
	e.config, e.request = config, req
	return map[string]any{}, nil
}

// connectorUnderTest registers executor under a catalogue entry of its own and
// configures an instance of it for the project, with the given settings.
func connectorUnderTest(t *testing.T, executor servicecontracts.ConnectorExecutor, settings map[string]any) (*jobService, *entities.ProcessDefinition, context.Context, string) {
	t.Helper()
	svc, projectID, ctx, _ := jobServiceForConnectorTest(t)

	key := "request-test-" + uuid.NewString()[:8]
	connector, err := svc.connectorSvc.CreateConnector(ctx, entities.Connector{Key: key, Name: "Request test", Type: "utility"})
	if err != nil {
		t.Fatalf("create connector: %v", err)
	}
	if _, err := svc.connectorSvc.CreateConnectorInstance(ctx, entities.ConnectorInstance{
		Name:      "Request test",
		Project:   &entities.Project{ID: projectID},
		Connector: &entities.Connector{ID: connector.ID},
		Config:    settings,
	}); err != nil {
		t.Fatalf("create instance: %v", err)
	}
	svc.connectorSvc.RegisterExecutor(key, executor)
	return svc, &entities.ProcessDefinition{Project: &entities.Project{ID: projectID}}, ctx, connector.ID.String()
}

// A step that asks for a statement, parameters and a result variable, calling a
// connector that takes none of them. Every executor written before requests
// existed is one of these, so this is the promise that they were not changed:
// they receive the process variables, all of them and only them.
func TestAConnectorThatTakesNoRequestIsCalledExactlyAsBefore(t *testing.T) {
	executor := &recordingExecutor{}
	svc, def, ctx, connectorID := connectorUnderTest(t, executor, map[string]any{"url": "https://partner.example.com"})

	variables := map[string]any{"orderId": "A-1", "amount": 120.0}
	node := entities.Node{ID: "call", Type: entities.ServiceTask, Properties: map[string]any{
		"connector_id":        connectorID,
		"connector_statement": "SELECT 1",
		"connector_params":    map[string]any{"order": "orderId"},
		"result_variable":     "answer",
	}}

	if _, err := svc.resolveAndExecuteConnector(ctx, def, node, maps.Clone(variables)); err != nil {
		t.Fatalf("execute: %v", err)
	}
	if !reflect.DeepEqual(executor.payload, variables) {
		t.Fatalf("the executor received %v, want exactly the process variables %v", executor.payload, variables)
	}
	if executor.config["url"] != "https://partner.example.com" {
		t.Fatalf("the executor did not receive its connection's settings: %v", executor.config)
	}
}

// A connector that takes a request gets what the step asked for, with every
// parameter already resolved to a value — a bare variable name and a FEEL
// expression alike.
func TestAConnectorThatTakesARequestGetsTheStepsOwn(t *testing.T) {
	executor := &recordingRequestExecutor{}
	svc, def, ctx, connectorID := connectorUnderTest(t, executor, map[string]any{})

	variables := map[string]any{"applicant": map[string]any{"id": "C-7"}, "amount": 120.0}
	node := entities.Node{ID: "lookup", Type: entities.ServiceTask, Properties: map[string]any{
		"connector_id":        connectorID,
		"connector_statement": "SELECT tier FROM customers WHERE id = :customer_id AND limit_ >= :needed",
		"connector_params": map[string]any{
			"customer_id": "applicant.id",
			"needed":      "amount * 2",
		},
		"result_variable": "customer",
	}}

	if _, err := svc.resolveAndExecuteConnector(ctx, def, node, variables); err != nil {
		t.Fatalf("execute: %v", err)
	}
	got := executor.request
	if got.Statement != node.Properties["connector_statement"] {
		t.Errorf("statement = %q", got.Statement)
	}
	if got.ResultVariable != "customer" {
		t.Errorf("result variable = %q", got.ResultVariable)
	}
	if got.Params["customer_id"] != "C-7" {
		t.Errorf("customer_id resolved to %#v, want the applicant's id", got.Params["customer_id"])
	}
	if needed, ok := got.Params["needed"].(float64); !ok || needed != 240 {
		t.Errorf("needed resolved to %#v, want 240", got.Params["needed"])
	}
	if !reflect.DeepEqual(got.Variables, variables) {
		t.Errorf("the request lost the variables: %v", got.Variables)
	}
}

// Nothing a step sets can reach its connection's settings. A step is authored
// in the designer; the settings hold the credential. A node property or a
// parameter named after a setting must leave the setting exactly as the
// administrator stored it.
func TestAStepCannotOverrideItsConnectionsSettings(t *testing.T) {
	const stored = "postgres://svc:real@db.internal:5432/crm"
	executor := &recordingRequestExecutor{}
	svc, def, ctx, connectorID := connectorUnderTest(t, executor, map[string]any{"dsn": stored})

	node := entities.Node{ID: "lookup", Type: entities.ServiceTask, Properties: map[string]any{
		"connector_id":        connectorID,
		"connector_statement": "SELECT 1",
		"connector_params":    map[string]any{"dsn": "target"},
		"result_variable":     "answer",
		"dsn":                 "postgres://attacker@evil.example.com/steal",
	}}

	if _, err := svc.resolveAndExecuteConnector(ctx, def, node, map[string]any{"target": "postgres://attacker@evil.example.com/steal"}); err != nil {
		t.Fatalf("execute: %v", err)
	}
	if executor.config["dsn"] != stored {
		t.Fatalf("the step replaced its connection's dsn with %v", executor.config["dsn"])
	}
}
