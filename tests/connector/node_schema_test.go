package connector_test

import (
	"encoding/json"
	"slices"
	"strings"
	"testing"

	"github.com/gsoultan/metis/server/domains/entities"
	serviceimpl "github.com/gsoultan/metis/server/domains/services/impl"
	"github.com/gsoultan/metis/server/domains/services/impl/sqlconnector"
	"github.com/gsoultan/metis/server/repositories"
	"github.com/gsoultan/metis/tests/testutils"
)

// The designer draws a lookup step's fields from node_schema on the catalogue,
// and the job reads those fields under the same names. This is that contract,
// from the service down to the JSON the Connectors API sends.
func TestTheCatalogueSaysWhatALookupStepFillsIn(t *testing.T) {
	svc := serviceimpl.NewConnectorService(repositories.NewRepository(testutils.SetupTestConn(t)))
	ctx := t.Context()
	if err := svc.EnsureDefaultConnectors(ctx); err != nil {
		t.Fatalf("seed: %v", err)
	}

	all, err := svc.ListConnectors(ctx)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	var lookup, slack entities.Connector
	for _, c := range all {
		switch c.Key {
		case sqlconnector.Key:
			lookup = c
		case "slack-message":
			slack = c
		}
	}
	want := []string{"connector_statement", "connector_params", "result_variable"}
	if keys := schemaKeys(lookup.NodeSchema); !slices.Equal(keys, want) {
		t.Fatalf("the lookup asks a step for %v, want %v", keys, want)
	}
	if len(slack.NodeSchema) != 0 {
		t.Fatalf("a connector a step only names was given step fields: %v", slack.NodeSchema)
	}

	one, err := svc.GetConnector(ctx, lookup.ID)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if !slices.Equal(schemaKeys(one.NodeSchema), want) {
		t.Fatal("reading one connector does not carry its step fields")
	}
	body, err := json.Marshal(one)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(body), `"node_schema"`) {
		t.Fatalf("the API would not send node_schema: %s", body)
	}
}

func schemaKeys(schema []entities.ConnectorProperty) []string {
	keys := make([]string, len(schema))
	for i, field := range schema {
		keys[i] = field.Key
	}
	return keys
}
