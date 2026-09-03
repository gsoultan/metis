package impl

import (
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/gsoultan/metis/server/domains/entities"
	"github.com/gsoultan/metis/server/repositories"
	"github.com/gsoultan/metis/tests/testutils"
)

// The redaction has to happen on the path that stores the incident, not merely
// be available in a package somewhere.
//
// This is the test that would have caught the original: internal/pkg/redaction
// already had patterns for `api_key=` and for URL userinfo, and was already
// applied to transport responses and setup — just not here, which is the one
// place a connector's error is written to the database and shown to an
// operator. A redactor that exists and is not called is not a redactor.
func TestCreateIncidentRedactsTheStoredError(t *testing.T) {
	const secret = "sk-live-9f2c4a1e8b7d0f36a5c9e2b4"

	db := testutils.SetupTestDB(t)
	repo := repositories.NewRepository(db)
	service := &jobService{repo: repo}

	instanceID, definitionID := uuid.New(), uuid.New()
	job := &entities.Job{
		ID:         uuid.New(),
		Instance:   &entities.ProcessInstance{ID: instanceID},
		Definition: &entities.ProcessDefinition{ID: definitionID},
		Node:       &entities.Node{ID: "charge"},
	}

	// The shape net/http produces when a connector cannot reach its API.
	jobErr := &stubError{text: `connector "salesforce": Get "https://api.example.com/v1/leads?api_key=` + secret + `": dial tcp: connection refused`}

	service.createIncident(t.Context(), job, jobErr)

	incidents, err := repo.Incident().ListByInstance(t.Context(), instanceID)
	if err != nil {
		t.Fatalf("list incidents: %v", err)
	}
	if len(incidents) == 0 {
		t.Fatal("no incident was recorded, so this test proves nothing about what it stores")
	}

	stored := incidents[0].Error
	if strings.Contains(stored, secret) {
		t.Fatalf("the incident stored the credential in plaintext:\n  %s", stored)
	}
	if !strings.Contains(stored, "salesforce") {
		t.Errorf("redaction removed the connector name, which is what makes the incident actionable: %s", stored)
	}
}

type stubError struct{ text string }

func (e *stubError) Error() string { return e.text }
