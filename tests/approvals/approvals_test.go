package approvals_test

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/google/uuid"

	"github.com/gsoultan/metis/internal/app"
	"github.com/gsoultan/metis/internal/pkg/health"
	"github.com/gsoultan/metis/server/domains/entities"
	observersimpl "github.com/gsoultan/metis/server/domains/observers/impl"
	"github.com/gsoultan/metis/server/domains/services"
	"github.com/gsoultan/metis/server/domains/services/impl"
	"github.com/gsoultan/metis/server/endpoints"
	"github.com/gsoultan/metis/server/repositories"
	"github.com/gsoultan/metis/server/repositories/models"
	"github.com/gsoultan/metis/server/repositories/pg"
	"github.com/gsoultan/metis/tests/testutils"
)

const password = "a-password-long-enough"

type world struct {
	handler http.Handler
	project uuid.UUID
	tokens  map[string]string
}

// newWorld is an organization with one project and the four people of an
// expense approval: Dana designs it, Rina asks, Budi supervises, Sari heads
// the unit.
func newWorld(t *testing.T) world {
	t.Helper()
	conn := testutils.SetupTestConn(t)
	repo := repositories.NewRepository(conn)
	sse := observersimpl.NewSSEObserver()
	svc := services.NewServiceFacade(repo, observersimpl.NewEventDispatcher(), sse, "approval-lines-test-secret",
		impl.NewWorkflowUserService(pg.NewWorkflowUserRepository(conn)), nil, nil)
	handler, _ := app.BuildAPIHandler(svc, endpoints.MakeEndpoints(svc), sse, nil, map[string]health.Checker{}, conn)

	system := entities.WithSystemContext(t.Context())
	org := uuid.Must(uuid.NewV7())
	if err := repo.Organization().Create(system, models.OrganizationModel{Base: models.Base{ID: models.UUID(org)}, Name: "Approvals Org"}); err != nil {
		t.Fatal(err)
	}
	tenant := entities.WithTenantContext(system, entities.TenantContext{TenantID: org.String()})
	project, err := svc.CreateProject(tenant, org, "Expenses", "")
	if err != nil {
		t.Fatal(err)
	}
	groups := map[string]uuid.UUID{}
	for _, name := range []string{"supervisors", "unit-heads"} {
		id := uuid.Must(uuid.NewV7())
		if err := repo.Group().Create(system, models.GroupModel{Base: models.Base{ID: models.UUID(id)}, OrganizationID: models.UUID(org), Name: name}); err != nil {
			t.Fatal(err)
		}
		groups[name] = id
	}
	w := world{handler: handler, project: project.ID, tokens: map[string]string{}}
	for _, p := range []struct{ name, role, group string }{
		{"dana", entities.RoleDesigner, ""},
		{"rina", entities.RoleUser, ""},
		{"budi", entities.RoleUser, "supervisors"},
		{"sari", entities.RoleUser, "unit-heads"},
	} {
		id := uuid.Must(uuid.NewV7())
		if err := svc.CreateUser(system, entities.User{
			ID: id, Username: p.name, Roles: []string{p.role}, Organizations: []*entities.Organization{{ID: org}},
		}, password); err != nil {
			t.Fatalf("seed %s: %v", p.name, err)
		}
		if p.group != "" {
			if err := repo.Group().AddMembership(system, id, groups[p.group]); err != nil {
				t.Fatal(err)
			}
		}
		_, token, err := svc.Login(t.Context(), p.name, password)
		if err != nil {
			t.Fatalf("sign %s in: %v", p.name, err)
		}
		w.tokens[p.name] = token
	}
	return w
}

func (w world) do(t *testing.T, as, method, path string, body any) (int, string) {
	t.Helper()
	var buf bytes.Buffer
	if body != nil {
		if err := json.NewEncoder(&buf).Encode(body); err != nil {
			t.Fatal(err)
		}
	}
	req := httptest.NewRequestWithContext(t.Context(), method, path, &buf)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+w.tokens[as])
	rec := httptest.NewRecorder()
	w.handler.ServeHTTP(rec, req)
	out, _ := io.ReadAll(rec.Body)
	return rec.Code, string(out)
}

func (w world) request(t *testing.T, code int, body string) entities.ApprovalRequest {
	t.Helper()
	if code != http.StatusOK {
		t.Fatalf("%d %s", code, body)
	}
	var out struct {
		Request entities.ApprovalRequest `json:"request"`
	}
	if err := json.Unmarshal([]byte(body), &out); err != nil {
		t.Fatalf("read %s: %v", body, err)
	}
	return out.Request
}

func (w world) define(t *testing.T) {
	t.Helper()
	code, body := w.do(t, "dana", http.MethodPost, "/api/v1/projects/"+w.project.String()+"/approval-lines", map[string]any{
		"key": "expense", "name": "Expense approval", "allow_revise": true,
		"fields": []map[string]any{{"id": "amount", "label": "Amount", "type": "number"}},
		"steps": []map[string]any{
			{"id": "supervisor", "name": "Supervisor approval", "groups": []string{"supervisors"}},
			{"id": "unit-head", "name": "Unit head approval", "groups": []string{"unit-heads"}},
		},
	})
	if code != http.StatusOK {
		t.Fatalf("define the line: %d %s", code, body)
	}
}

func (w world) submit(t *testing.T, vars map[string]any) entities.ApprovalRequest {
	t.Helper()
	code, body := w.do(t, "rina", http.MethodPost, "/api/v1/projects/"+w.project.String()+"/approval-lines/expense/requests", map[string]any{"variables": vars})
	return w.request(t, code, body)
}

func (w world) decide(t *testing.T, as string, task uuid.UUID, decision map[string]any) (int, string) {
	t.Helper()
	return w.do(t, as, http.MethodPost, "/api/v1/approval-tasks/"+task.String()+"/decision", decision)
}

// TestAnApprovalLineIsDefinedAsDataAndRunsEndToEnd is the line from submit to
// approved: sent back once, resubmitted with a correction, then approved by
// both approvers, with every decision on its trail.
func TestAnApprovalLineIsDefinedAsDataAndRunsEndToEnd(t *testing.T) {
	w := newWorld(t)
	w.define(t)

	code, body := w.do(t, "rina", http.MethodGet, "/api/v1/projects/"+w.project.String()+"/approval-lines/expense", nil)
	var read struct {
		Line entities.ApprovalLine `json:"line"`
	}
	if code != http.StatusOK || json.Unmarshal([]byte(body), &read) != nil || len(read.Line.Steps) != 2 ||
		read.Line.Steps[1].ID != "unit-head" || read.Line.Steps[1].Groups[0] != "unit-heads" || !read.Line.AllowRevise || read.Line.Version != 1 ||
		len(read.Line.Fields) != 1 || read.Line.Fields[0].ID != "amount" {
		t.Fatalf("read the line back: %d %s", code, body)
	}

	req := w.submit(t, map[string]any{"amount": 1200})
	if req.Status != entities.ApprovalInProgress || req.SubmittedBy != "rina" || req.Round != 1 ||
		req.Steps[0].Status != entities.ApprovalStepWaiting || req.Steps[1].Status != entities.ApprovalStepPending {
		t.Fatalf("submitted: %+v", req)
	}
	first := req.Steps[0].Waiting.TaskID

	// Sari is not a supervisor, and sending back needs a reason.
	if code, body := w.decide(t, "sari", first, map[string]any{"decision": "approve"}); code == http.StatusOK {
		t.Fatalf("sari decided the supervisor's step: %s", body)
	}
	if code, _ := w.decide(t, "budi", first, map[string]any{"decision": "revise"}); code != http.StatusBadRequest {
		t.Fatalf("a revise with no comment: %d, want 400", code)
	}
	if code, _ := w.decide(t, "budi", first, map[string]any{"decision": "approve", "variables": map[string]any{"amount": 1}}); code != http.StatusBadRequest {
		t.Fatalf("an approver changing the amount: %d, want 400", code)
	}

	code, body = w.decide(t, "budi", first, map[string]any{"decision": "revise", "comment": "Attach the receipt"})
	req = w.request(t, code, body)
	if req.Status != entities.ApprovalSentBack || req.Revision == nil || req.Revision.Assignee != "rina" || req.Steps[0].Status != entities.ApprovalStepSentBack {
		t.Fatalf("sent back: %+v", req)
	}

	code, body = w.decide(t, "rina", req.Revision.TaskID, map[string]any{"decision": "resubmit", "variables": map[string]any{"amount": 900}})
	req = w.request(t, code, body)
	if req.Status != entities.ApprovalInProgress || req.Round != 2 || req.Steps[0].Status != entities.ApprovalStepWaiting || req.Variables["amount"] != float64(900) {
		t.Fatalf("resubmitted: %+v", req)
	}

	code, body = w.decide(t, "budi", req.Steps[0].Waiting.TaskID, map[string]any{"decision": "approve"})
	req = w.request(t, code, body)
	if req.Steps[0].Status != entities.ApprovalStepApproved || req.Steps[0].Decision.By != "budi" || req.Steps[1].Status != entities.ApprovalStepWaiting {
		t.Fatalf("supervisor approved: %+v", req)
	}
	code, body = w.decide(t, "sari", req.Steps[1].Waiting.TaskID, map[string]any{"decision": "approve", "comment": "Fine"})
	req = w.request(t, code, body)
	if req.Status != entities.ApprovalApproved {
		t.Fatalf("approved by both: %+v", req)
	}
	var trail []string
	for _, d := range req.Trail {
		trail = append(trail, d.Step+":"+d.Decision+":"+d.By)
	}
	want := []string{"supervisor:revise:budi", "revise:resubmit:rina", "supervisor:approve:budi", "unit-head:approve:sari"}
	if len(trail) != len(want) {
		t.Fatalf("trail %v, want %v", trail, want)
	}
	for i := range want {
		if trail[i] != want[i] {
			t.Fatalf("trail %v, want %v", trail, want)
		}
	}

	code, body = w.do(t, "budi", http.MethodGet, "/api/v1/projects/"+w.project.String()+"/approval-lines/expense/requests?submitted_by=rina", nil)
	var listed struct {
		Requests []entities.ApprovalRequest `json:"requests"`
	}
	if code != http.StatusOK || json.Unmarshal([]byte(body), &listed) != nil || len(listed.Requests) != 1 ||
		listed.Requests[0].Status != entities.ApprovalApproved || listed.Requests[0].Steps[1].Decision.By != "sari" {
		t.Fatalf("list the line's requests: %d %s", code, body)
	}
	code, body = w.do(t, "rina", http.MethodGet, "/api/v1/approval-requests/"+req.InstanceID.String(), nil)
	if again := w.request(t, code, body); again.Status != entities.ApprovalApproved || len(again.Trail) != 4 {
		t.Fatalf("read the request back: %+v", again)
	}
}

// TestARejectedOrWithdrawnRequestEnds: a rejection needs a reason and ends the
// request; a request sent back can be withdrawn by its submitter.
func TestARejectedOrWithdrawnRequestEnds(t *testing.T) {
	w := newWorld(t)
	w.define(t)

	req := w.submit(t, map[string]any{"amount": 5})
	if code, _ := w.decide(t, "budi", req.Steps[0].Waiting.TaskID, map[string]any{"decision": "reject"}); code != http.StatusBadRequest {
		t.Fatalf("a reject with no comment: %d, want 400", code)
	}
	code, body := w.decide(t, "budi", req.Steps[0].Waiting.TaskID, map[string]any{"decision": "reject", "comment": "Not a business expense"})
	if got := w.request(t, code, body); got.Status != entities.ApprovalRejected || got.Steps[0].Status != entities.ApprovalStepRejected {
		t.Fatalf("rejected: %+v", got)
	}

	req = w.submit(t, map[string]any{"amount": 6})
	code, body = w.decide(t, "budi", req.Steps[0].Waiting.TaskID, map[string]any{"decision": "revise", "comment": "Which project?"})
	req = w.request(t, code, body)
	if code, _ := w.decide(t, "budi", req.Revision.TaskID, map[string]any{"decision": "withdraw", "comment": "x"}); code == http.StatusOK {
		t.Fatal("budi withdrew rina's request")
	}
	code, body = w.decide(t, "rina", req.Revision.TaskID, map[string]any{"decision": "withdraw", "comment": "Not needed after all"})
	if got := w.request(t, code, body); got.Status != entities.ApprovalWithdrawn {
		t.Fatalf("withdrawn: %+v", got)
	}
}

// TestWhatTheLineKeepsCannotBeSetAndOnlyADesignerDefinesOne.
func TestWhatTheLineKeepsCannotBeSetAndOnlyADesignerDefinesOne(t *testing.T) {
	w := newWorld(t)
	path := "/api/v1/projects/" + w.project.String() + "/approval-lines"
	line := map[string]any{"key": "expense", "name": "Expense", "steps": []map[string]any{{"id": "boss", "name": "Boss", "users": []string{"budi"}}}}
	if code, _ := w.do(t, "rina", http.MethodPost, path, line); code != http.StatusForbidden {
		t.Fatalf("rina defined a line: %d, want 403", code)
	}
	for what, bad := range map[string]map[string]any{
		"no steps":      {"key": "x", "name": "X", "steps": []any{}},
		"nobody named":  {"key": "x", "name": "X", "steps": []map[string]any{{"id": "a", "name": "A"}}},
		"an expression": {"key": "x", "name": "X", "steps": []map[string]any{{"id": "a", "name": "A", "users": []string{"${boss}"}}}},
		"a bad key":     {"key": "X Y", "name": "X", "steps": []map[string]any{{"id": "a", "name": "A", "users": []string{"budi"}}}},
		"a typo":        {"key": "x", "name": "X", "stepz": []any{}},
	} {
		if code, body := w.do(t, "dana", http.MethodPost, path, bad); code != http.StatusBadRequest {
			t.Errorf("%s: %d %s, want 400", what, code, body)
		}
	}
	if code, body := w.do(t, "dana", http.MethodPost, path, line); code != http.StatusOK {
		t.Fatalf("define: %d %s", code, body)
	}
	code, _ := w.do(t, "rina", http.MethodPost, path+"/expense/requests", map[string]any{"variables": map[string]any{"submittedBy": "sari"}})
	if code != http.StatusBadRequest {
		t.Fatalf("rina submitting as sari: %d, want 400", code)
	}
	if code, _ := w.do(t, "rina", http.MethodPost, path+"/nope/requests", map[string]any{}); code != http.StatusNotFound {
		t.Fatalf("a line that is not there: %d, want 404", code)
	}
}

// TestARunningRequestKeepsTheLineItWasSubmittedOn: changing the line deploys
// a new version; requests already running finish on theirs.
func TestARunningRequestKeepsTheLineItWasSubmittedOn(t *testing.T) {
	w := newWorld(t)
	w.define(t)
	old := w.submit(t, map[string]any{"amount": 10})

	code, body := w.do(t, "dana", http.MethodPost, "/api/v1/projects/"+w.project.String()+"/approval-lines", map[string]any{
		"key": "expense", "name": "Expense approval",
		"steps": []map[string]any{{"id": "unit-head", "name": "Unit head approval", "groups": []string{"unit-heads"}}},
	})
	if code != http.StatusOK {
		t.Fatalf("change the line: %d %s", code, body)
	}
	fresh := w.submit(t, map[string]any{"amount": 20})
	if fresh.Version != 2 || len(fresh.Steps) != 1 || fresh.Steps[0].Status != entities.ApprovalStepWaiting {
		t.Fatalf("a new request: %+v, want the one-step version 2", fresh)
	}
	code, body = w.do(t, "rina", http.MethodGet, "/api/v1/approval-requests/"+old.InstanceID.String(), nil)
	if got := w.request(t, code, body); got.Version != 1 || len(got.Steps) != 2 || got.Steps[0].ID != "supervisor" {
		t.Fatalf("the running request: %+v, want it still on version 1", got)
	}
	code, body = w.decide(t, "budi", old.Steps[0].Waiting.TaskID, map[string]any{"decision": "approve"})
	if got := w.request(t, code, body); got.Steps[1].Status != entities.ApprovalStepWaiting {
		t.Fatalf("the old request moves on along its own line: %+v", got)
	}
}
