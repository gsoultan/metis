package impl

import (
	"context"
	"maps"
	"reflect"
	"testing"

	"github.com/gsoultan/metis/server/domains/entities"
)

// answeringExecutor records what it was sent and answers with a fixed reply.
type answeringExecutor struct {
	config map[string]any
	sent   map[string]any
	reply  map[string]any
}

func (e *answeringExecutor) Execute(_ context.Context, config, payload map[string]any) (map[string]any, error) {
	e.config, e.sent = config, payload
	return maps.Clone(e.reply), nil
}

// A connector step that maps its inputs sends only what it names, computed
// from the process; one that maps its outputs keeps only what it names,
// computed from the answer. The designer's tables for these were written under
// names nothing read, so a step configured to rename a field did not.
func TestAConnectorStepSendsAndKeepsWhatItsMappingsSay(t *testing.T) {
	executor := &answeringExecutor{reply: map[string]any{
		"registration_id": "09876543", "credit_score": 42.0, "status": "active",
	}}
	svc, def, ctx, connectorID := connectorUnderTest(t, executor, map[string]any{})

	node := entities.Node{ID: "company", Type: entities.ServiceTask, Properties: map[string]any{
		"connector_id": connectorID,
		// Their field ← from the process: a name, or a FEEL expression.
		"input_mapping": map[string]any{"company_number": "companyNumber", "limit": "requested * 2"},
		// Store it as ← from their answer.
		"output_mapping": map[string]any{"creditScore": "credit_score", "isActive": `status = "active"`},
	}}
	variables := map[string]any{"companyNumber": "09876543", "requested": 500.0, "supplierName": "Northwind"}

	stored, err := svc.resolveAndExecuteConnector(ctx, def, node, variables)
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	if want := map[string]any{"company_number": "09876543", "limit": 1000.0}; !reflect.DeepEqual(executor.sent, want) {
		t.Errorf("the partner was sent %v, want only %v — supplierName is the process's, not theirs", executor.sent, want)
	}
	if want := map[string]any{"creditScore": 42.0, "isActive": true}; !reflect.DeepEqual(stored, want) {
		t.Errorf("the step would store %v, want only %v", stored, want)
	}
}

// A step that maps nothing behaves exactly as every step did before: all the
// variables out, the whole answer back.
func TestAConnectorStepWithNoMappingsIsUnchanged(t *testing.T) {
	executor := &answeringExecutor{reply: map[string]any{"ok": true, "id": "M-1"}}
	svc, def, ctx, connectorID := connectorUnderTest(t, executor, map[string]any{})

	variables := map[string]any{"orderId": "A-1", "amount": 120.0}
	stored, err := svc.resolveAndExecuteConnector(ctx, def,
		entities.Node{ID: "notify", Type: entities.ServiceTask, Properties: map[string]any{"connector_id": connectorID}},
		maps.Clone(variables))
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	if !reflect.DeepEqual(executor.sent, variables) {
		t.Errorf("sent %v, want every variable %v", executor.sent, variables)
	}
	if !reflect.DeepEqual(stored, map[string]any{"ok": true, "id": "M-1"}) {
		t.Errorf("stored %v, want the whole answer", stored)
	}
}
