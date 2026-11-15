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
	var lookup, httpJSON entities.Connector
	for _, c := range all {
		switch c.Key {
		case sqlconnector.Key:
			lookup = c
		case "http-json":
			httpJSON = c
		}
	}
	want := []string{"connector_statement", "connector_params", "result_variable"}
	if keys := schemaKeys(lookup.NodeSchema); !slices.Equal(keys, want) {
		t.Fatalf("the lookup asks a step for %v, want %v", keys, want)
	}
	if len(httpJSON.NodeSchema) != 0 {
		t.Fatalf("a connector a step only names was given step fields: %v", httpJSON.NodeSchema)
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

// A connector that sends a message asks the step for it, and asks for nothing
// a step written before these fields existed would now be flagged for.
func TestTheCatalogueSaysWhatAMessageStepFillsIn(t *testing.T) {
	svc := serviceimpl.NewConnectorService(repositories.NewRepository(testutils.SetupTestConn(t)))
	ctx := t.Context()
	if err := svc.EnsureDefaultConnectors(ctx); err != nil {
		t.Fatalf("seed: %v", err)
	}
	all, err := svc.ListConnectors(ctx)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	want := map[string][]string{
		"slack-message":    {"connector_field_text"},
		"discord-message":  {"connector_field_content"},
		"ms-teams-message": {"connector_field_title", "connector_field_text"},
		"email-smtp":       {"connector_field_to", "connector_field_subject", "connector_field_body"},
		"sendgrid-email":   {"connector_field_to_email", "connector_field_subject", "connector_field_content"},
		"panmail-email":    {"connector_field_to", "connector_field_subject", "connector_field_body"},
		"fcm-push":         {"connector_field_token", "connector_field_title", "connector_field_body", "connector_field_link"},
	}
	for _, c := range all {
		keys, sends := want[c.Key]
		if !sends {
			continue
		}
		if got := schemaKeys(c.NodeSchema); !slices.Equal(got, keys) {
			t.Errorf("%s asks a step for %v, want %v", c.Key, got, keys)
		}
		for _, field := range c.NodeSchema {
			if field.Required {
				t.Errorf("%s marks %s required, which flags every step that sends it another way", c.Key, field.Key)
			}
		}
		delete(want, c.Key)
	}
	if len(want) > 0 {
		t.Errorf("not in the catalogue: %v", want)
	}
}
