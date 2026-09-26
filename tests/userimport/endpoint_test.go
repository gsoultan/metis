package userimport_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/google/uuid"
	"github.com/gsoultan/metis/server/domains/entities"
	"github.com/gsoultan/metis/server/domains/services"
	servicecontracts "github.com/gsoultan/metis/server/domains/services/contracts"
	"github.com/gsoultan/metis/server/endpoints/participant"
	participanthttp "github.com/gsoultan/metis/server/transports/https/participants"
)

// serve mounts the participant routes over a real service and a real database.
//
// The endpoints rather than the service, because the interesting part is what
// crosses the wire: an uploaded file has to arrive as multipart, a remote source
// as JSON, and both have to come back as the same summary.
func serveParticipants(t *testing.T) (*httptest.Server, uuid.UUID) {
	t.Helper()
	_, svc, projectID := fixture(t)

	eps := participant.MakeEndpoints(&facadeStub{participants: svc})
	mux := http.NewServeMux()
	participanthttp.RegisterHandlers(mux, eps, nil)
	server := httptest.NewServer(mux)
	t.Cleanup(server.Close)
	return server, projectID
}

func TestImportingAFileOverHTTP(t *testing.T) {
	server, projectID := serveParticipants(t)

	var body bytes.Buffer
	form := multipart.NewWriter(&body)
	_ = form.WriteField("project_id", projectID.String())
	part, err := form.CreateFormFile("file", "staff.csv")
	if err != nil {
		t.Fatalf("form: %v", err)
	}
	_, _ = part.Write([]byte("username,display_name,email,groups\nada,Ada Lovelace,ada@example.com,approvers\nbob,,not-an-address,\n"))
	_ = form.Close()

	request, err := http.NewRequestWithContext(t.Context(), http.MethodPost, server.URL+"/api/v1/participants/import", &body)
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	request.Header.Set("Content-Type", form.FormDataContentType())

	summary := postForSummary(t, request)
	if summary.Created != 1 || summary.Groups != 1 {
		t.Fatalf("ada should import with her team, got %+v", summary)
	}
	// The partial import is reported, not swallowed.
	if len(summary.Problems) != 1 || summary.Problems[0].Username != "bob" {
		t.Fatalf("bob's address should be reported, got %v", summary.Problems)
	}

	// And the directory is readable back through the list endpoint.
	people := listParticipants(t, server, projectID)
	if len(people) != 1 || people[0].Username != "ada" {
		t.Fatalf("expected ada in the directory, got %+v", people)
	}
}

func TestImportingFromAnEndpointOverHTTP(t *testing.T) {
	t.Setenv("METIS_HTTP_ALLOW_PRIVATE_NETWORKS", "true")
	directory := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"data":[{"username":"carol","email":"carol@example.com","active":true}]}`))
	}))
	defer directory.Close()

	server, projectID := serveParticipants(t)
	summary := postJSON(t, server, map[string]any{
		"project_id": projectID.String(),
		"kind":       "http",
		"url":        directory.URL,
	})
	if summary.Created != 1 {
		t.Fatalf("carol should import, got %+v", summary)
	}

	people := listParticipants(t, server, projectID)
	if len(people) != 1 || people[0].Username != "carol" {
		t.Fatalf("expected carol, got %+v", people)
	}
}

// An import is additive: somebody absent from the second file is left alone,
// not deactivated. Taking people's work away because a file did not mention
// them is a change nobody asked for and nobody would see.
func TestASecondImportDoesNotRemoveWhoItOmits(t *testing.T) {
	server, projectID := serveParticipants(t)

	upload(t, server, projectID, "username\nada\nbob\n")
	upload(t, server, projectID, "username\nada\n")

	people := listParticipants(t, server, projectID)
	if len(people) != 2 {
		t.Fatalf("bob was not mentioned the second time and must still be there, got %d", len(people))
	}
	for _, person := range people {
		if !person.Active {
			t.Errorf("%s should still be active; an import does not deactivate", person.Username)
		}
	}
}

// Asking whether a project has anybody in its directory meant downloading all
// of it: the list took no limit, and the getting-started checklist in Help
// asked every time it was opened. A limit answers with that many people,
// alphabetically, and without one the whole directory still comes back. A
// limit that is not a positive number is read as no limit, the way a malformed
// page number is.
func TestTheDirectoryCanBeAskedForJustOnePerson(t *testing.T) {
	server, projectID := serveParticipants(t)
	upload(t, server, projectID, "username\ncarol\nada\nbob\n")

	if people := listParticipantsWhere(t, server, projectID, "&limit=1"); len(people) != 1 || people[0].Username != "ada" {
		t.Fatalf("limit=1 should answer with ada alone, got %v", usernames(people))
	}
	if people := listParticipantsWhere(t, server, projectID, "&limit=2"); len(people) != 2 {
		t.Fatalf("limit=2 should answer with two, got %v", usernames(people))
	}
	if people := listParticipants(t, server, projectID); len(people) != 3 {
		t.Fatalf("without a limit the whole directory comes back, got %v", usernames(people))
	}
	for _, malformed := range []string{"&limit=0", "&limit=-1", "&limit=all"} {
		if people := listParticipantsWhere(t, server, projectID, malformed); len(people) != 3 {
			t.Fatalf("%s should read as no limit, got %v", malformed, usernames(people))
		}
	}
}

func usernames(people []entities.WorkflowUser) []string {
	names := make([]string, 0, len(people))
	for _, person := range people {
		names = append(names, person.Username)
	}
	return names
}

// An unusable source is refused as a source, and nothing lands.
func TestAnUnusableSourceIsRefusedOverHTTP(t *testing.T) {
	server, projectID := serveParticipants(t)

	for _, tc := range []struct {
		name string
		body map[string]any
	}{
		{"unknown kind", map[string]any{"project_id": projectID.String(), "kind": "carrier-pigeon"}},
		{"endpoint on a private network", map[string]any{"project_id": projectID.String(), "kind": "http", "url": "http://169.254.169.254/"}},
		{"query with no database", map[string]any{"project_id": projectID.String(), "kind": "postgres", "query": "SELECT 1 AS username"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			body, _ := json.Marshal(tc.body)
			request, err := http.NewRequestWithContext(t.Context(), http.MethodPost,
				server.URL+"/api/v1/participants/import", bytes.NewReader(body))
			if err != nil {
				t.Fatalf("request: %v", err)
			}
			request.Header.Set("Content-Type", "application/json")
			response, err := http.DefaultClient.Do(request)
			if err != nil {
				t.Fatalf("do: %v", err)
			}
			defer func() { _ = response.Body.Close() }()
			if response.StatusCode < 400 {
				t.Fatalf("%s should be refused, got %s", tc.name, response.Status)
			}
		})
	}

	if people := listParticipants(t, server, projectID); len(people) != 0 {
		t.Fatalf("nothing should have imported, got %d", len(people))
	}
}

// ── helpers ────────────────────────────────────────────────────────────────

// facadeStub satisfies the whole facade while only answering the participant
// half.
//
// The embedded interface is nil on purpose: anything these endpoints call that
// is not about participants would panic, which is a louder and more useful
// failure than a stub that quietly returns zero values for a method the test
// did not mean to exercise.
type facadeStub struct {
	services.ServiceFacade
	participants servicecontracts.WorkflowUserService
}

func (f *facadeStub) ListWorkflowUsers(ctx context.Context, projectID uuid.UUID, limit int) ([]entities.WorkflowUser, error) {
	return f.participants.ListWorkflowUsers(ctx, projectID, limit)
}

func (f *facadeStub) ImportWorkflowUsers(ctx context.Context, projectID uuid.UUID, csv io.Reader) (entities.ImportSummary, error) {
	return f.participants.ImportWorkflowUsers(ctx, projectID, csv)
}

func (f *facadeStub) SyncWorkflowUsersFromHTTP(ctx context.Context, projectID uuid.UUID, url, method string) (entities.ImportSummary, error) {
	return f.participants.SyncWorkflowUsersFromHTTP(ctx, projectID, url, method)
}

func (f *facadeStub) SyncWorkflowUsersFromPostgres(ctx context.Context, projectID uuid.UUID, dsn, query string) (entities.ImportSummary, error) {
	return f.participants.SyncWorkflowUsersFromPostgres(ctx, projectID, dsn, query)
}

func upload(t *testing.T, server *httptest.Server, projectID uuid.UUID, csv string) {
	t.Helper()
	var body bytes.Buffer
	form := multipart.NewWriter(&body)
	_ = form.WriteField("project_id", projectID.String())
	part, _ := form.CreateFormFile("file", "staff.csv")
	_, _ = part.Write([]byte(csv))
	_ = form.Close()

	request, err := http.NewRequestWithContext(t.Context(), http.MethodPost, server.URL+"/api/v1/participants/import", &body)
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	request.Header.Set("Content-Type", form.FormDataContentType())
	postForSummary(t, request)
}

func postJSON(t *testing.T, server *httptest.Server, payload map[string]any) participant.ImportParticipantsResponse {
	t.Helper()
	body, _ := json.Marshal(payload)
	request, err := http.NewRequestWithContext(t.Context(), http.MethodPost,
		server.URL+"/api/v1/participants/import", bytes.NewReader(body))
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	request.Header.Set("Content-Type", "application/json")
	return postForSummary(t, request)
}

func postForSummary(t *testing.T, request *http.Request) participant.ImportParticipantsResponse {
	t.Helper()
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		t.Fatalf("do: %v", err)
	}
	defer func() { _ = response.Body.Close() }()

	var summary participant.ImportParticipantsResponse
	if err := json.NewDecoder(response.Body).Decode(&summary); err != nil {
		t.Fatalf("decode: %v", err)
	}
	return summary
}

func listParticipants(t *testing.T, server *httptest.Server, projectID uuid.UUID) []entities.WorkflowUser {
	t.Helper()
	return listParticipantsWhere(t, server, projectID, "")
}

// listParticipantsWhere lists with extra query parameters, such as "&limit=1".
func listParticipantsWhere(t *testing.T, server *httptest.Server, projectID uuid.UUID, extra string) []entities.WorkflowUser {
	t.Helper()
	request, err := http.NewRequestWithContext(t.Context(), http.MethodGet,
		fmt.Sprintf("%s/api/v1/participants?project_id=%s%s", server.URL, projectID, extra), nil)
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		t.Fatalf("do: %v", err)
	}
	defer func() { _ = response.Body.Close() }()

	var listed struct {
		Participants []entities.WorkflowUser `json:"participants"`
	}
	if err := json.NewDecoder(response.Body).Decode(&listed); err != nil {
		t.Fatalf("decode: %v", err)
	}
	return listed.Participants
}
