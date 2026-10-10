package deviation_test

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/google/uuid"

	httptransport "github.com/go-kit/kit/transport/http"

	pkgauth "github.com/gsoultan/metis/internal/pkg/auth"
	"github.com/gsoultan/metis/server/domains/entities"
	"github.com/gsoultan/metis/server/endpoints/deviation"
	"github.com/gsoultan/metis/server/transports/https/common"
	"github.com/gsoultan/metis/server/transports/https/deviations"
)

// object decodes a reply into the fields it is written with.
func object(t *testing.T, raw string) map[string]any {
	t.Helper()
	var written map[string]any
	if err := json.Unmarshal([]byte(raw), &written); err != nil {
		t.Fatalf("decode: %v (%s)", err, raw)
	}
	return written
}

// oneStep starts an instance that waits at "step", a task alice holds.
func (h *deviationHarness) oneStep(t *testing.T) uuid.UUID {
	t.Helper()
	return h.startOneStep(t, entities.Node{Name: "Approve", Type: entities.UserTask, Assignee: "alice"})
}

// Design §8: the four request routes are an administrator's of the request's
// organization. Everybody else is refused, on every one of them, and after
// each refusal no table holds anything it did not hold before. Another
// organization's administrator is told there is no such request — in the
// words an administrator of this one is told of a request that does not
// exist — and sees an empty queue.
func TestOnlyAnOrganizationsAdministratorsSeeAndDecideItsRequests(t *testing.T) {
	h := newDeviationRouteHarness(t)
	instanceID := h.oneStep(t)
	boss, deputy := h.signIn(t, "boss", entities.RoleAdmin), h.signIn(t, "deputy", entities.RoleAdmin)
	// Everybody signs in before anything is compared: signing in is not what
	// is being asked about.
	type caller struct {
		name, token string
		headers     []string
		status      int
		reply       string
		// seesAnEmptyQueue is an administrator of another organization: the
		// queue is theirs to read, and holds nothing of this one.
		seesAnEmptyQueue bool
	}
	forbidden := refusal("forbidden", needsTheAdministratorRole)
	notFound := refusal("not found", noSuchRequest)
	callers := []caller{}
	for _, role := range []string{entities.RoleUser, entities.RoleOperator, entities.RoleDesigner} {
		callers = append(callers, caller{name: "a " + role, token: h.signIn(t, "refused-"+strings.ToLower(role), role),
			status: http.StatusForbidden, reply: forbidden})
	}
	both, elsewhere := h.signInToBoth(t, "boss-elsewhere")
	outsider := h.signInElsewhere(t, "outsider-boss", entities.RoleAdmin)
	callers = append(callers,
		caller{name: "an administrator of another organization, acting in this one", token: both,
			headers: []string{"X-Organization-ID", h.orgID.String()}, status: http.StatusForbidden, reply: forbidden},
		caller{name: "the same account, acting in its own organization", token: both,
			headers: []string{"X-Organization-ID", elsewhere.String()}, status: http.StatusNotFound, reply: notFound, seesAnEmptyQueue: true},
		caller{name: "another organization's administrator", token: outsider,
			status: http.StatusNotFound, reply: notFound, seesAnEmptyQueue: true},
		// Naming an organization in the header does not put an account in it:
		// the header chooses among the caller's own.
		caller{name: "another organization's administrator, naming this organization in the header", token: outsider,
			headers: []string{"X-Organization-ID", h.orgID.String()}, status: http.StatusUnauthorized},
		caller{name: "nobody", status: http.StatusUnauthorized},
	)
	requestID := h.askToWaive(t, boss, instanceID)
	decision := fmt.Sprintf(`{"reason":%q}`, decisionReason)
	routes := []struct{ name, method, path, body string }{
		{"the queue", http.MethodGet, requestsPath, ""},
		{"the read", http.MethodGet, requestPath(requestID), ""},
		{"the approval", http.MethodPost, requestPath(requestID) + "/approve", decision},
		{"the rejection", http.MethodPost, requestPath(requestID) + "/reject", decision},
	}
	before := h.everyRow(t)

	for _, who := range callers {
		for _, route := range routes {
			status, raw, _ := h.call(t, route.method, who.token, route.path, route.body, who.headers...)
			wantStatus, wantReply := who.status, who.reply
			if who.seesAnEmptyQueue && route.path == requestsPath {
				wantStatus, wantReply = http.StatusOK, `{"requests":[],"total":0}`
			}
			if status != wantStatus {
				t.Fatalf("%s on %s: %d (%s), want %d", who.name, route.name, status, raw, wantStatus)
			}
			if wantReply != "" && !sameJSON(t, raw, wantReply) {
				t.Fatalf("%s on %s was told %s, want %s", who.name, route.name, raw, wantReply)
			}
			for _, told := range []string{"Approve", "alice", "boss\"", requestID, instanceID.String(), routeReason} {
				if strings.Contains(raw, told) {
					t.Fatalf("%s on %s was told something of the request (%s): %s", who.name, route.name, told, raw)
				}
			}
			h.requireUnchanged(t, before, who.name+" on "+route.name)
		}
	}

	// A request that is not there, asked for by an administrator of this
	// organization, is answered in the same words, byte for byte: the answer
	// to another organization's administrator does not say which it was.
	nowhere := uuid.Must(uuid.NewV7()).String()
	for _, route := range []struct{ name, method, suffix, body string }{
		{"the read", http.MethodGet, "", ""},
		{"the approval", http.MethodPost, "/approve", decision},
		{"the rejection", http.MethodPost, "/reject", decision},
	} {
		missingStatus, missing, _ := h.call(t, route.method, deputy, requestPath(nowhere)+route.suffix, route.body)
		foreignStatus, foreign, _ := h.call(t, route.method, outsider, requestPath(requestID)+route.suffix, route.body)
		if missingStatus != http.StatusNotFound || foreignStatus != http.StatusNotFound || !sameJSON(t, missing, notFound) || missing != foreign {
			t.Fatalf("%s: a request that is not there %d (%s), another organization's %d (%s); want 404 %s for each",
				route.name, missingStatus, missing, foreignStatus, foreign, notFound)
		}
	}
	h.requireUnchanged(t, before, "requests for a request the caller cannot see")
	if h.requestStatus(t, requestID) != "pending_approval" || !h.stepIsOpen(t, instanceID) {
		t.Fatal("refused calls changed the request or the instance")
	}

	// And the comparison every refusal above passed does see a decision: an
	// administrator of the organization is not refused, and the tables an
	// approval writes to are not what they were.
	if status, approved, raw := h.decide(t, deputy, requestID, "approve", decisionReason); status != http.StatusOK || !approved.Applied {
		t.Fatalf("the organization's second administrator approves: %d (%s)", status, raw)
	}
	after := h.everyRow(t)
	for _, table := range []string{"deviation_requests", "instance_deviations", "tasks", "process_instances", "audit_logs"} {
		if after[table] == before[table] {
			t.Errorf("the approval left %s as it was (%s): the comparison would not have seen a refused request acting", table, after[table])
		}
	}
}

// behindNoGate serves the deviation routes with nothing in front of them: the
// real handlers, decoders and endpoints over the real service, and no chain
// that signs anybody in or checks a role. Every request to it is made as
// caller, acting in the harness's organization.
//
// It is how a test asks what the service itself answers a caller the gate
// would have turned away, or one the gate cannot produce — through the route,
// so that the status is the route's.
func (h *deviationHarness) behindNoGate(t *testing.T, caller entities.User) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()
	deviations.RegisterHandlers(mux, deviation.MakeEndpoints(h.svc), []httptransport.ServerOption{
		httptransport.ServerErrorEncoder(common.EncodeError),
		httptransport.ServerBefore(func(ctx context.Context, _ *http.Request) context.Context {
			return context.WithValue(entities.WithTenantContext(ctx, entities.TenantContext{TenantID: h.orgID.String()}), pkgauth.UserContextKey, caller)
		}),
	})
	server := httptest.NewServer(mux)
	t.Cleanup(server.Close)
	return server
}

// ask sends one request to server and answers the status and the reply.
func ask(t *testing.T, server *httptest.Server, method, path, body string) (int, string) {
	t.Helper()
	req, err := http.NewRequestWithContext(context.Background(), method, server.URL+path, strings.NewReader(body))
	if err != nil {
		t.Fatalf("build request: %v", err)
	}
	resp, err := routeClient.Do(req)
	if err != nil {
		t.Fatalf("do request: %v", err)
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read the reply: %v", err)
	}
	return resp.StatusCode, strings.TrimSpace(string(raw))
}

// AGENTS.md §2.3: a fast path that skips a check is a vulnerability. The
// routes are gated, and the service asks again who is calling: reached with
// no gate in front of it, each of the four refuses whoever is not an
// administrator of the organization, and an administrator whose sign-in
// carries no account id — the requester and the approver are told apart by
// account, so a caller with none cannot be shown to be somebody else. Each is
// a 403, in the service's words, and writes nothing.
func TestTheServiceRefusesWhoeverTheGateWouldHaveBehindEachRequestRoute(t *testing.T) {
	h := newDeviationRouteHarness(t)
	instanceID := h.oneStep(t)
	boss := h.signIn(t, "boss", entities.RoleAdmin)
	h.signIn(t, "deputy", entities.RoleAdmin)
	requestID := h.askToWaive(t, boss, instanceID)
	decision := fmt.Sprintf(`{"reason":%q}`, decisionReason)
	routes := []struct{ name, method, path, body string }{
		{"the queue", http.MethodGet, requestsPath, ""},
		{"the read", http.MethodGet, requestPath(requestID), ""},
		{"the approval", http.MethodPost, requestPath(requestID) + "/approve", decision},
		{"the rejection", http.MethodPost, requestPath(requestID) + "/reject", decision},
	}
	notAnAdministrator := refusal("forbidden", "only an administrator can ask for or decide a request for a second administrator")
	noAccount := refusal("forbidden", "asking for and giving a second administrator's approval needs an account, and this request carries none")
	callers := []struct {
		name  string
		who   entities.User
		reply string
	}{
		{"a member", entities.User{ID: h.accountID(t, "deputy"), Username: "deputy", Roles: []string{entities.RoleUser}}, notAnAdministrator},
		{"an operator and designer", entities.User{ID: uuid.Must(uuid.NewV7()), Username: "ops", Roles: []string{entities.RoleOperator, entities.RoleDesigner}}, notAnAdministrator},
		{"an administrator with no name", entities.User{ID: uuid.Must(uuid.NewV7()), Roles: []string{entities.RoleAdmin}}, notAnAdministrator},
		{"an administrator of another organization only", entities.User{ID: uuid.Must(uuid.NewV7()), Username: "elsewhere",
			RolesByOrganization: map[uuid.UUID][]string{uuid.Must(uuid.NewV7()): {entities.RoleAdmin}}}, notAnAdministrator},
		{"an administrator with no account id", entities.User{Username: "deputy", Roles: []string{entities.RoleAdmin}}, noAccount},
	}
	before := h.everyRow(t)
	for _, caller := range callers {
		server := h.behindNoGate(t, caller.who)
		for _, route := range routes {
			status, raw := ask(t, server, route.method, route.path, route.body)
			if status != http.StatusForbidden || !sameJSON(t, raw, caller.reply) {
				t.Fatalf("%s on %s, with no gate in front: %d (%s), want 403 %s", caller.name, route.name, status, raw, caller.reply)
			}
			h.requireUnchanged(t, before, caller.name+" on "+route.name+", with no gate in front")
		}
	}
	if h.requestStatus(t, requestID) != "pending_approval" || !h.stepIsOpen(t, instanceID) {
		t.Fatal("refused calls changed the request or the instance")
	}
}
