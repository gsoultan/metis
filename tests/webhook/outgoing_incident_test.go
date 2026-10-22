package webhook_test

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/gsoultan/metis/server/domains/entities"
	"github.com/gsoultan/metis/server/repositories/models"
)

// deployCharge deploys start → a step a worker performs → end.
func (h *outgoingHarness) deployCharge(t *testing.T) {
	t.Helper()
	if _, err := h.svc.CreateDefinition(h.ctx, &entities.ProcessDefinition{
		Project: &entities.Project{ID: h.projectID},
		Key:     "charge-card",
		Name:    "Charge the customer",
		Nodes: []*entities.Node{
			{ID: "start", Type: entities.StartEvent},
			{ID: "charge", Type: entities.ServiceTask, Name: "Charge the card", ExternalTopic: "charge"},
			{ID: "end", Type: entities.EndEvent},
		},
		Flows: []*entities.SequenceFlow{
			{ID: "f1", SourceRef: "start", TargetRef: "charge"},
			{ID: "f2", SourceRef: "charge", TargetRef: "end"},
		},
	}); err != nil {
		t.Fatalf("deploy: %v", err)
	}
}

// A worker that gives up raises an incident, and a receiver that asked for
// incidents is owed one that names it, its step and its process, and says
// nothing of the error, which can quote what the call sent and was answered.
func TestARaisedIncidentIsOwedToTheReceiversThatAskedForIt(t *testing.T) {
	h := newOutgoingHarness(t)
	h.deployCharge(t)
	pager := h.register(t, "https://pager.example.com/hooks", entities.OutgoingIncidentRaised)

	instanceID, err := h.svc.StartProcess(h.ctx, h.projectID, "charge-card", nil)
	if err != nil {
		t.Fatalf("start: %v", err)
	}
	fetched, err := h.svc.FetchAndLock(h.ctx, "charge", "worker-1", 1, 60_000)
	if err != nil || len(fetched) != 1 {
		t.Fatalf("fetch: %d tasks, %v", len(fetched), err)
	}
	if err := h.svc.HandleFailure(h.ctx, fetched[0].ID, "worker-1", "card 4111 declined", "issuer said no to IBAN DE89", 0, 0); err != nil {
		t.Fatalf("report the failure: %v", err)
	}

	incidents, err := h.repo.Incident().ListByInstance(h.ctx, instanceID)
	if err != nil || len(incidents) != 1 || incidents[0].Status != models.IncidentOpen {
		t.Fatalf("incidents: %+v, err=%v; want the one the worker raised", incidents, err)
	}
	due := h.claimAll(t, time.Now())
	if len(due) != 1 || due[0].Webhook.ID != pager.ID || due[0].Event != entities.OutgoingIncidentRaised {
		t.Fatalf("owed %d deliveries, want one incident.raised to the pager: %+v", len(due), due)
	}
	for _, quoted := range []string{"4111", "declined", "issuer", "DE89"} {
		if strings.Contains(string(due[0].Body), quoted) {
			t.Errorf("the delivery quotes %q from the incident's error: %s", quoted, due[0].Body)
		}
	}
	var payload entities.OutgoingWebhookPayload
	if err := json.Unmarshal(due[0].Body, &payload); err != nil {
		t.Fatalf("delivery body: %v", err)
	}
	if payload.Incident == nil || payload.Incident.ID != uuid.UUID(incidents[0].ID) {
		t.Errorf("incident = %+v, want the one raised, %s", payload.Incident, uuid.UUID(incidents[0].ID))
	}
	if payload.Node == nil || payload.Node.ID != "charge" || payload.Node.Name != "Charge the card" {
		t.Errorf("node = %+v, want the step the incident is at", payload.Node)
	}
	if payload.Instance == nil || payload.Instance.ID != instanceID || payload.Instance.ProcessKey != "charge-card" {
		t.Errorf("instance = %+v, want the case and its process", payload.Instance)
	}
	if payload.ProjectID != h.projectID {
		t.Errorf("project = %s, want %s", payload.ProjectID, h.projectID)
	}
}

// A worker with tries left raises no incident, and nobody is told of one.
func TestAFailureWithRetriesLeftIsOwedToNobody(t *testing.T) {
	h := newOutgoingHarness(t)
	h.deployCharge(t)
	h.register(t, "https://pager.example.com/hooks", entities.OutgoingIncidentRaised)

	if _, err := h.svc.StartProcess(h.ctx, h.projectID, "charge-card", nil); err != nil {
		t.Fatalf("start: %v", err)
	}
	fetched, err := h.svc.FetchAndLock(h.ctx, "charge", "worker-1", 1, 60_000)
	if err != nil || len(fetched) != 1 {
		t.Fatalf("fetch: %d tasks, %v", len(fetched), err)
	}
	if err := h.svc.HandleFailure(h.ctx, fetched[0].ID, "worker-1", "timeout", "", 2, 1000); err != nil {
		t.Fatalf("report the failure: %v", err)
	}
	if due := h.claimAll(t, time.Now()); len(due) != 0 {
		t.Errorf("owed %d deliveries for a failure that will be tried again, want none", len(due))
	}
}
