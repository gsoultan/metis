package bpmn_test

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gsoultan/metis/server/domains/entities"
)

// A Teams step that says what to send, in the field the step panel shows,
// sends it with the process's values filled in.
func TestAMessageStepSendsWhatItsFieldSays(t *testing.T) {
	h := newServiceTaskHarness(t)
	received := make(chan string, 1)
	webhook := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		received <- string(raw)
		w.WriteHeader(http.StatusAccepted)
	}))
	t.Cleanup(webhook.Close)

	ctx := entities.WithSystemContext(h.ctx)
	catalogue, err := h.connectors.ListConnectors(ctx)
	if err != nil {
		t.Fatalf("list connectors: %v", err)
	}
	var teams *entities.Connector
	for i := range catalogue {
		if catalogue[i].Key == "ms-teams-message" {
			teams = &catalogue[i]
		}
	}
	if teams == nil {
		t.Fatal("the catalogue has no Teams connector")
	}
	if len(teams.NodeSchema) == 0 {
		t.Error("the Teams connector declares no step fields, so the step panel shows none")
	}
	if _, err := h.connectors.CreateConnectorInstance(h.ctx, entities.ConnectorInstance{
		Project:   &entities.Project{ID: h.projectID},
		Connector: &entities.Connector{ID: teams.ID},
		Name:      "Approvers channel",
		Config:    map[string]any{"webhook_url": webhook.URL},
	}); err != nil {
		t.Fatalf("create connection: %v", err)
	}

	instance := h.run(t, entities.Node{ID: "notify", Type: entities.ServiceTask, Name: "Tell the approvers",
		Properties: map[string]any{
			entities.ConnectionProperty: "Approvers channel",
			"connector_field_text":      "Approve {{po_number}} for {{amount}} EUR",
		}}, map[string]any{"po_number": "PO-17", "amount": 4000})

	select {
	case body := <-received:
		var card map[string]any
		if err := json.Unmarshal([]byte(body), &card); err != nil {
			t.Fatalf("Teams was not sent JSON: %q", body)
		}
		if !strings.Contains(body, "Approve PO-17 for 4000 EUR") {
			t.Errorf("Teams was sent %s, want the filled-in message", body)
		}
	default:
		t.Fatal("nothing was sent to Teams")
	}
	if instance.Status != entities.ProcessCompleted {
		t.Errorf("instance status = %s, want completed", instance.Status)
	}
}
