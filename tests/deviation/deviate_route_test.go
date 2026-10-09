package deviation_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/gsoultan/metis/internal/app"
	"github.com/gsoultan/metis/internal/pkg/health"
	"github.com/gsoultan/metis/server/domains/entities"
	observersimpl "github.com/gsoultan/metis/server/domains/observers/impl"
	"github.com/gsoultan/metis/server/domains/services"
	serviceimpl "github.com/gsoultan/metis/server/domains/services/impl"
	"github.com/gsoultan/metis/server/endpoints"
	"github.com/gsoultan/metis/server/repositories"
	"github.com/gsoultan/metis/tests/testutils"
)

const routeReason = "the approver has left; the director agreed in writing"

// newDeviationRouteHarness is newDeviationHarness with the two observers that
// write when the engine raises an event on a running server: the one that
// keeps the audit trail and the one that leaves a notification
// (internal/app.setupService registers both). A test that asks what a request
// left behind has to have them writing, or it asks of fewer tables than
// production writes to.
//
// It repeats newDeviationHarness because that one builds its dispatcher where
// nothing else can reach it.
func newDeviationRouteHarness(t *testing.T) *deviationHarness {
	t.Helper()
	db := testutils.SetupTestDB(t)
	conn := testutils.StormConn(db)
	repo := repositories.NewRepository(conn)
	sse := observersimpl.NewSSEObserver()
	dispatcher := observersimpl.NewEventDispatcher()
	dispatcher.Register(observersimpl.NewAuditLogObserver(repo.Audit()))
	dispatcher.Register(observersimpl.NewNotificationObserver(serviceimpl.NewNotificationService(repo.Notification())))
	svc := services.NewServiceFacade(repo, dispatcher, sse, "deviation-test-secret", nil, nil, nil)
	handler, _ := app.BuildAPIHandler(svc, endpoints.MakeEndpoints(svc), sse, nil, map[string]health.Checker{}, conn)
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)

	org, err := svc.CreateOrganization(context.Background(), "Deviation Org", "")
	if err != nil {
		t.Fatalf("create organization: %v", err)
	}
	tctx := entities.WithTenantContext(context.Background(), entities.TenantContext{TenantID: org.ID.String()})
	project, err := svc.CreateProject(tctx, org.ID, "Deviation Project", "")
	if err != nil {
		t.Fatalf("create project: %v", err)
	}
	return &deviationHarness{server: server, svc: svc, repo: repo, db: db, orgID: org.ID, projID: project.ID}
}

// deviateReply is what the route answers with a 200, and with the 202 of a
// waive that waits for a second administrator.
type deviateReply struct {
	Plan struct {
		NodeID                 string         `json:"node_id"`
		VisitKey               string         `json:"visit_key"`
		Outputs                map[string]any `json:"outputs"`
		RequiresSecondApprover bool           `json:"requires_second_approver"`
		Refusals               []string       `json:"refusals"`
		Warnings               []string       `json:"warnings"`
		Applicable             bool           `json:"applicable"`
	} `json:"plan"`
	Applied   bool           `json:"applied"`
	Replayed  bool           `json:"replayed"`
	Deviation map[string]any `json:"deviation"`
	// PendingApproval is the request a 202 waits on; nil in every other reply.
	PendingApproval *pendingView `json:"pending_approval"`
}

func deviationsPath(instanceID uuid.UUID) string {
	return "/api/v1/instances/" + instanceID.String() + "/deviations"
}

// send posts body exactly as it is written, so a test can send what no
// encoder would, and answers the status and the reply as it came.
func (h *deviationHarness) send(t *testing.T, token, path, body string, headers ...string) (int, string) {
	t.Helper()
	req, err := http.NewRequestWithContext(context.Background(), http.MethodPost, h.server.URL+path, strings.NewReader(body))
	if err != nil {
		t.Fatalf("build request: %v", err)
	}
	req.Header.Set("Content-Type", "application/json")
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	for i := 0; i+1 < len(headers); i += 2 {
		req.Header.Set(headers[i], headers[i+1])
	}
	resp, err := routeClient.Do(req)
	if err != nil {
		t.Fatalf("do request: %v", err)
	}
	defer resp.Body.Close()
	var out bytes.Buffer
	if _, err := out.ReadFrom(resp.Body); err != nil {
		t.Fatalf("read the reply: %v", err)
	}
	return resp.StatusCode, strings.TrimSpace(out.String())
}

func (h *deviationHarness) deviate(t *testing.T, token string, instanceID uuid.UUID, body map[string]any) (int, deviateReply, string) {
	t.Helper()
	encoded, err := json.Marshal(body)
	if err != nil {
		t.Fatalf("encode body: %v", err)
	}
	return h.deviateWith(t, token, instanceID, string(encoded))
}

// deviateWith is deviate for a body already written.
func (h *deviationHarness) deviateWith(t *testing.T, token string, instanceID uuid.UUID, body string) (int, deviateReply, string) {
	t.Helper()
	status, raw := h.send(t, token, deviationsPath(instanceID), body)
	var reply deviateReply
	if status == http.StatusOK || status == http.StatusAccepted {
		if err := json.Unmarshal([]byte(raw), &reply); err != nil {
			t.Fatalf("decode: %v (%s)", err, raw)
		}
	}
	return status, reply, raw
}

// previewed asks for the plan as an administrator and answers the request
// that applies it: the same body, naming the plan's visit key and saying
// "dry_run": false.
func (h *deviationHarness) previewed(t *testing.T, admin string, instanceID uuid.UUID, body map[string]any) (map[string]any, deviateReply) {
	t.Helper()
	status, planned, raw := h.deviate(t, admin, instanceID, body)
	if status != http.StatusOK || planned.Applied || planned.Replayed || planned.Deviation != nil || planned.Plan.VisitKey == "" {
		t.Fatalf("the preview of %v: %d (%s), want a 200 plan with a visit key and nothing applied", body, status, raw)
	}
	apply := map[string]any{"visit_key": planned.Plan.VisitKey, "dry_run": false}
	for name, value := range body {
		apply[name] = value
	}
	return apply, planned
}

// refusal is the reply a request is refused with: the route's error body
// around a sentence.
func refusal(class, sentence string) string {
	encoded, err := json.Marshal(map[string]string{"error": class + ": " + sentence})
	if err != nil {
		panic(err)
	}
	return string(encoded)
}

func invalid(sentence string) string { return refusal("invalid argument", sentence) }

// sameJSON reports whether two replies say the same thing, however each
// writer escaped it.
func sameJSON(t *testing.T, got, want string) bool {
	t.Helper()
	var a, b any
	if err := json.Unmarshal([]byte(got), &a); err != nil {
		return false
	}
	if err := json.Unmarshal([]byte(want), &b); err != nil {
		t.Fatalf("the expected reply is not JSON: %v (%s)", err, want)
	}
	left, _ := json.Marshal(a)
	right, _ := json.Marshal(b)
	return bytes.Equal(left, right)
}

func (h *deviationHarness) stepIsOpen(t *testing.T, instanceID uuid.UUID) bool {
	t.Helper()
	return h.openTasksOn(t, instanceID, "step") > 0
}

// everyRow is what the test's database holds (testutils.EveryRow): two
// readings that are equal mean nothing was written between them — to any
// table, by any path.
func (h *deviationHarness) everyRow(t *testing.T) map[string]string {
	t.Helper()
	return testutils.EveryRow(t, h.db)
}

// requireUnchanged fails the test when any table holds something it did not
// hold at before.
func (h *deviationHarness) requireUnchanged(t *testing.T, before map[string]string, after string) {
	t.Helper()
	if changed := testutils.TablesThatDiffer(before, h.everyRow(t)); len(changed) != 0 {
		t.Fatalf("%s changed %v", after, changed)
	}
}

// signInToBoth creates an account that belongs to the harness's organization
// and to a second one, holds the administrator role in the second only, and
// answers its token and the second organization.
func (h *deviationHarness) signInToBoth(t *testing.T, name string) (token string, elsewhere uuid.UUID) {
	t.Helper()
	org, err := h.svc.CreateOrganization(context.Background(), "Elsewhere "+name, "")
	if err != nil {
		t.Fatalf("create the other organization: %v", err)
	}
	account := entities.User{
		ID: uuid.Must(uuid.NewV7()), Username: name,
		Organizations: []*entities.Organization{{ID: h.orgID}, {ID: org.ID}},
	}
	if err := h.svc.CreateUser(entities.WithSystemContext(context.Background()), account, harnessPassword); err != nil {
		t.Fatalf("create %s: %v", name, err)
	}
	there := entities.WithTenantContext(entities.WithSystemContext(context.Background()), entities.TenantContext{TenantID: org.ID.String()})
	if err := h.svc.SetOrganizationRoles(there, account.ID, []string{entities.RoleAdmin}); err != nil {
		t.Fatalf("make %s an administrator of the other organization: %v", name, err)
	}
	status, body := h.do(t, http.MethodPost, "", "/api/v1/login", map[string]string{"username": name, "password": harnessPassword})
	var out struct {
		Token string `json:"token"`
	}
	if err := json.Unmarshal([]byte(body), &out); status != http.StatusOK || err != nil || out.Token == "" {
		t.Fatalf("login %s: %d (%s)", name, status, body)
	}
	return out.Token, org.ID
}

// needsTheAdministratorRole is what the route's gate answers an account of
// the organization that is not its administrator. They are the gate's words,
// not the service's ("only an administrator can waive, cancel or hold an
// instance"): a request refused in them never reached the service.
const needsTheAdministratorRole = "this needs the ADMIN role, which your account does not hold in this organization; " +
	"an administrator here can grant it"

// noSuchInstance is what an instance the caller cannot see is answered,
// whether it is another organization's or nobody's.
const noSuchInstance = "no such process instance"

// Review Focus 5; rulings §6. Waive, cancel and hold are an administrator's
// (design §8). Everybody else is refused — a preview as well as an apply,
// because a preview reads the instance — and each refused apply is one that
// would have been made had its sender been allowed: it names the visit key of
// a plan an administrator previewed and says "dry_run": false. After every one
// of them no table holds anything it did not hold before.
func TestOnlyAnAdministratorOfTheOrganizationDeviatesAnInstance(t *testing.T) {
	h := newDeviationRouteHarness(t)
	instanceID := h.startOneStep(t, entities.Node{Name: "Approve", Type: entities.UserTask, Assignee: "alice"})
	admin := h.signIn(t, "boss", entities.RoleAdmin)
	// Everybody signs in before anything is compared: signing in is not what
	// is being asked about.
	type caller struct {
		name, token string
		headers     []string
		status      int
		reply       string
	}
	forbidden := refusal("forbidden", needsTheAdministratorRole)
	callers := []caller{}
	for _, role := range []string{entities.RoleUser, entities.RoleOperator, entities.RoleDesigner} {
		callers = append(callers, caller{name: "a " + role, token: h.signIn(t, "refused-"+strings.ToLower(role), role),
			status: http.StatusForbidden, reply: forbidden})
	}
	// An administrator elsewhere who also belongs here is no administrator
	// here: the role counts where it is held.
	both, elsewhere := h.signInToBoth(t, "boss-elsewhere")
	outsider := h.signInElsewhere(t, "outsider-boss", entities.RoleAdmin)
	// The second administrator, who approves the waive at the end, signs in
	// here with the others.
	h.secondAdministrator(t)
	callers = append(callers,
		caller{name: "an administrator of another organization, acting in this one", token: both,
			headers: []string{"X-Organization-ID", h.orgID.String()}, status: http.StatusForbidden, reply: forbidden},
		caller{name: "the same account, acting in its own organization", token: both,
			headers: []string{"X-Organization-ID", elsewhere.String()}, status: http.StatusNotFound, reply: refusal("not found", noSuchInstance)},
		caller{name: "another organization's administrator", token: outsider,
			status: http.StatusNotFound, reply: refusal("not found", noSuchInstance)},
		// Naming an organization in the header does not put an account in it:
		// the header chooses among the caller's own.
		caller{name: "another organization's administrator, naming this organization in the header", token: outsider,
			headers: []string{"X-Organization-ID", h.orgID.String()}, status: http.StatusUnauthorized},
		caller{name: "nobody", status: http.StatusUnauthorized},
	)

	requests := map[string]string{}
	for _, kind := range []string{"waive", "cancel", "hold"} {
		preview := map[string]any{"kind": kind, "node_id": "step", "reason": routeReason}
		apply, _ := h.previewed(t, admin, instanceID, preview)
		for name, body := range map[string]map[string]any{"preview": preview, "apply": apply} {
			encoded, err := json.Marshal(body)
			if err != nil {
				t.Fatalf("encode: %v", err)
			}
			requests[kind+" "+name] = string(encoded)
		}
	}
	before := h.everyRow(t)

	for _, who := range callers {
		for what, body := range requests {
			status, raw := h.send(t, who.token, deviationsPath(instanceID), body, who.headers...)
			if status != who.status {
				t.Fatalf("%s sending a %s: %d (%s), want %d", who.name, what, status, raw, who.status)
			}
			if who.reply != "" && !sameJSON(t, raw, who.reply) {
				t.Fatalf("%s sending a %s was told %s, want %s", who.name, what, raw, who.reply)
			}
			if strings.Contains(raw, "Approve") || strings.Contains(raw, "alice") {
				t.Fatalf("%s sending a %s was told something of the instance: %s", who.name, what, raw)
			}
			h.requireUnchanged(t, before, who.name+" sending a "+what)
		}
	}
	if !h.stepIsOpen(t, instanceID) {
		t.Fatal("refused requests closed the step")
	}

	// The administrator is not refused: the waive they ask for waits, a second
	// administrator approves it, and it is applied. The reply names them both
	// and carries no account id.
	status, applied, raw := h.secondedWith(t, admin, instanceID, requests["waive apply"])
	if status != http.StatusOK || !applied.Applied || applied.Replayed || applied.Deviation["kind"] != "waive" || applied.Deviation["actor"] != "boss" {
		t.Fatalf("the administrator's apply, approved by a second: %d (%s)", status, raw)
	}
	if applied.Deviation["approved_by"] != seconderName {
		t.Fatalf("the waive does not say who approved it: %s", raw)
	}
	if applied.Deviation["actor_is_server"] != false {
		t.Fatalf("an administrator's waive reads as the server's: %s", raw)
	}
	for _, forbidden := range []string{`"actor_id"`, `"approved_by_id"`, `"requested_by_id"`, `"decided_by_id"`,
		h.accountID(t, "boss").String(), h.accountID(t, seconderName).String()} {
		if strings.Contains(raw, forbidden) {
			t.Fatalf("the reply carries %s: %s", forbidden, raw)
		}
	}
	if h.stepIsOpen(t, instanceID) {
		t.Fatal("the waived step is still open")
	}
	// And the comparison every refusal above passed does see an apply: the
	// tables an apply writes to are not what they were.
	after := h.everyRow(t)
	for _, table := range []string{"instance_deviations", "tasks", "process_instances", "audit_logs"} {
		if after[table] == before[table] {
			t.Errorf("the administrator's apply left %s as it was (%s): the comparison would not have seen a refused request acting", table, after[table])
		}
	}
}

// The gate comes before the service: whoever may not ask is told so whatever
// they asked for, and learns nothing from the answer — not whether there is
// such an instance, not whether what they asked for could be done.
//
// One thing comes before the gate, as on every route: reading the body. A body
// that cannot be read is answered as that, to anybody signed in. It says
// nothing of any instance — nothing has been looked at.
func TestSomebodyWhoMayNotDeviateIsToldNothingElse(t *testing.T) {
	h := newDeviationRouteHarness(t)
	instanceID := h.startOneStep(t, entities.Node{Name: "Approve", Type: entities.UserTask, Assignee: "alice"})
	member := h.signIn(t, "member", entities.RoleUser)
	before := h.everyRow(t)
	forbidden := refusal("forbidden", needsTheAdministratorRole)

	for name, request := range map[string]struct{ path, body string }{
		"an instance that is not there": {deviationsPath(uuid.Must(uuid.NewV7())), `{"kind":"cancel","reason":"x"}`},
		"an id that is not one":         {"/api/v1/instances/not-an-id/deviations", `{"kind":"cancel","reason":"x"}`},
		"a kind there is not":           {deviationsPath(instanceID), `{"kind":"skip","node_id":"step","reason":"x"}`},
		"an apply with no visit key":    {deviationsPath(instanceID), `{"kind":"waive","node_id":"step","reason":"x","dry_run":false}`},
	} {
		status, raw := h.send(t, member, request.path, request.body)
		if status != http.StatusForbidden || !sameJSON(t, raw, forbidden) {
			t.Errorf("a member asking for %s: %d (%s), want 403 %s", name, status, raw, forbidden)
		}
	}
	unreadable := invalid(requestCouldNotBeRead)
	if status, raw := h.send(t, member, deviationsPath(instanceID), `{"kind":"cancel","dry_run":"false"}`); status != http.StatusBadRequest || !sameJSON(t, raw, unreadable) {
		t.Errorf("a member sending what cannot be read: %d (%s), want 400 %s", status, raw, unreadable)
	}
	// Nobody signed in is asked who they are before anything: 401 whatever is sent.
	for name, body := range map[string]string{"a request": `{"kind":"cancel","reason":"x"}`, "what is not a request": `{"kind":`} {
		if status, raw := h.send(t, "", deviationsPath(instanceID), body); status != http.StatusUnauthorized {
			t.Errorf("nobody sending %s: %d (%s), want 401", name, status, raw)
		}
	}
	h.requireUnchanged(t, before, "requests from somebody who may not ask")
}

// A request for an instance of another organization and a request for an
// instance that is not there are answered in the same words, a preview and an
// apply alike: the answer does not say which it was.
func TestAnInstanceOfAnotherOrganizationIsAnsweredAsOneThatIsNotThere(t *testing.T) {
	h := newDeviationRouteHarness(t)
	instanceID := h.startOneStep(t, entities.Node{Name: "Approve", Type: entities.UserTask, Assignee: "alice"})
	admin := h.signIn(t, "boss", entities.RoleAdmin)
	outsider := h.signInElsewhere(t, "outsider-boss", entities.RoleAdmin)
	apply, _ := h.previewed(t, admin, instanceID, map[string]any{"kind": "waive", "node_id": "step", "reason": routeReason})
	before := h.everyRow(t)

	nowhere := uuid.Must(uuid.NewV7())
	for name, body := range map[string]map[string]any{
		"a preview": {"kind": "waive", "node_id": "step", "reason": routeReason},
		"an apply":  apply,
	} {
		foreignStatus, _, foreign := h.deviate(t, outsider, instanceID, body)
		missingStatus, _, missing := h.deviate(t, outsider, nowhere, body)
		ownStatus, _, own := h.deviate(t, admin, nowhere, body)
		if foreignStatus != http.StatusNotFound || missingStatus != http.StatusNotFound || ownStatus != http.StatusNotFound {
			t.Fatalf("%s: another organization's instance %d (%s), no instance %d (%s), no instance for its own administrator %d (%s); want 404 each",
				name, foreignStatus, foreign, missingStatus, missing, ownStatus, own)
		}
		want := refusal("not found", noSuchInstance)
		if !sameJSON(t, foreign, want) || foreign != missing || missing != own {
			t.Errorf("%s: another organization's instance is answered %s, and one that is not there %s and %s; want each %s", name, foreign, missing, own, want)
		}
		if strings.Contains(foreign, "Approve") || strings.Contains(foreign, "alice") {
			t.Errorf("%s of another organization's instance says something of it: %s", name, foreign)
		}
	}
	h.requireUnchanged(t, before, "requests for an instance the caller cannot see")
}

// A.7. The only request that changes anything is one whose body says
// "dry_run": false — the JSON boolean, in a field named exactly that, once.
// Every body below names the visit key of a plan that can be applied, so the
// only thing between it and an apply is what it says of dry_run. None of them
// changes anything: what can be read is answered as a preview, and what
// cannot is refused, so that a client that meant to apply is not told "ok".
func TestOnlyARequestThatSaysDryRunFalseChangesAnything(t *testing.T) {
	h := newDeviationRouteHarness(t)
	instanceID := h.startOneStep(t, entities.Node{Name: "Approve", Type: entities.UserTask, Assignee: "alice"})
	admin := h.signIn(t, "boss", entities.RoleAdmin)
	apply, planned := h.previewed(t, admin, instanceID, map[string]any{"kind": "cancel", "node_id": "step", "reason": routeReason})
	rest := fmt.Sprintf(`"kind":"cancel","node_id":"step","reason":%q,"visit_key":%q`, routeReason, planned.Plan.VisitKey)
	before := h.everyRow(t)
	path := deviationsPath(instanceID)

	previews := map[string]struct{ path, body string }{
		"no dry_run":                      {path, `{` + rest + `}`},
		"dry_run true":                    {path, `{` + rest + `,"dry_run":true}`},
		"dry_run null":                    {path, `{` + rest + `,"dry_run":null}`},
		"dry_run false in the address":    {path + "?dry_run=false", `{` + rest + `}`},
		"dry_run false inside the reason": {path, `{"kind":"cancel","node_id":"step","reason":"\"dry_run\": false","visit_key":"` + planned.Plan.VisitKey + `"}`},
	}
	for name, request := range previews {
		status, raw := h.send(t, admin, request.path, request.body)
		var reply deviateReply
		if err := json.Unmarshal([]byte(raw), &reply); status != http.StatusOK || err != nil || reply.Applied || reply.Replayed || reply.Deviation != nil {
			t.Errorf("%s: %d (%s), want a 200 preview", name, status, raw)
		}
		if !strings.Contains(raw, `"applied":false`) || strings.Contains(raw, `"deviation"`) {
			t.Errorf("%s: the preview does not say it applied nothing, or carries a record: %s", name, raw)
		}
		h.requireUnchanged(t, before, name)
	}

	unreadable := invalid(requestCouldNotBeRead)
	refused := map[string]string{
		`dry_run "false", a string`:        `{` + rest + `,"dry_run":"false"}`,
		"dry_run 0":                        `{` + rest + `,"dry_run":0}`,
		"dry_run an empty string":          `{` + rest + `,"dry_run":""}`,
		"dry_run a list":                   `{` + rest + `,"dry_run":[false]}`,
		"dry_run False":                    `{` + rest + `,"dry_run":False}`,
		"DRY_RUN false":                    `{` + rest + `,"DRY_RUN":false}`,
		"dryRun false":                     `{` + rest + `,"dryRun":false}`,
		"dry_run said twice":               `{` + rest + `,"dry_run":true,"dry_run":false}`,
		"dry_run false said twice":         `{` + rest + `,"dry_run":false,"dry_run":false}`,
		"a second request after the first": `{` + rest + `}{` + rest + `,"dry_run":false}`,
		"words after the request":          `{` + rest + `,"dry_run":true} false`,
		"a list of requests":               `[{` + rest + `,"dry_run":false}]`,
		"the request inside another":       `{"request":{` + rest + `,"dry_run":false}}`,
		"a request cut short":              `{` + rest + `,"dry_run":false`,
		"no body":                          ``,
		"a body of spaces":                 `   `,
		"null":                             `null`,
		"false":                            `false`,
		"not JSON":                         `kind=cancel&node_id=step&dry_run=false`,
	}
	for name, body := range refused {
		status, raw := h.send(t, admin, path, body)
		if status != http.StatusBadRequest || !sameJSON(t, raw, unreadable) {
			t.Errorf("%s: %d (%s), want 400 %s", name, status, raw, unreadable)
		}
		h.requireUnchanged(t, before, name)
	}
	if !h.stepIsOpen(t, instanceID) {
		t.Fatal("a request that did not say dry_run false closed the step")
	}

	// And the one that does say it is the one that acts.
	status, applied, raw := h.deviate(t, admin, instanceID, apply)
	if status != http.StatusOK || !applied.Applied || applied.Deviation["kind"] != "cancel" {
		t.Fatalf("the request that says dry_run false: %d (%s), want it applied", status, raw)
	}
	instance, err := h.svc.GetInstance(h.tenantContext(), instanceID)
	if err != nil || instance.Status != entities.ProcessCancelled {
		t.Fatalf("the instance is %s (err %v), want cancelled", instance.Status, err)
	}
}

// requestCouldNotBeRead is what a body the server cannot read is answered.
const requestCouldNotBeRead = "this request could not be read: send one JSON object with kind, reason, node_id (a cancel may leave it out), " +
	"and for a waive outputs; to apply a plan add its visit_key and \"dry_run\": false; name each field exactly and once"

// A body the server cannot read is refused in words a person can act on —
// never the decoder's, which name the server's own types (slice 2's I-3).
func TestADeviationRequestThatCannotBeReadIsRefusedInPlainWords(t *testing.T) {
	h := newDeviationRouteHarness(t)
	instanceID := h.startOneStep(t, entities.Node{Name: "Approve", Type: entities.UserTask, Assignee: "alice"})
	admin := h.signIn(t, "boss", entities.RoleAdmin)
	before := h.everyRow(t)
	unreadable := invalid(requestCouldNotBeRead)

	for name, body := range map[string]string{
		"a field there is not":         `{"kind":"waive","node":"step","reason":"x"}`,
		"the instance in the body":     `{"kind":"waive","node_id":"step","reason":"x","instance_id":"` + instanceID.String() + `"}`,
		"a kind that is a number":      `{"kind":5,"node_id":"step","reason":"x"}`,
		"a reason that is a list":      `{"kind":"waive","node_id":"step","reason":["x"]}`,
		"outputs that are a list":      `{"kind":"waive","node_id":"step","reason":"x","outputs":["approved"]}`,
		"outputs that are a sentence":  `{"kind":"waive","node_id":"step","reason":"x","outputs":"approved"}`,
		"a visit key that is a number": `{"kind":"waive","node_id":"step","reason":"x","visit_key":7}`,
		"Kind, with a capital":         `{"Kind":"waive","node_id":"step","reason":"x"}`,
		"kind said twice":              `{"kind":"hold","kind":"cancel","node_id":"step","reason":"x"}`,
	} {
		status, raw := h.send(t, admin, deviationsPath(instanceID), body)
		if status != http.StatusBadRequest || !sameJSON(t, raw, unreadable) {
			t.Errorf("%s: %d (%s), want 400 %s", name, status, raw, unreadable)
		}
		for _, leak := range []string{"json:", "struct", "Go ", "deviation.DeviateInstanceRequest", "unmarshal"} {
			if strings.Contains(raw, leak) {
				t.Errorf("%s: the refusal leaks %q: %s", name, leak, raw)
			}
		}
	}

	// A request too large to be one is told so, rather than that it could not
	// be read. The outputs' own limit is far below this one.
	huge := `{"kind":"waive","node_id":"step","reason":"` + strings.Repeat("x", 300<<10) + `"}`
	tooLarge := invalid("this request is larger than 256 KiB, which is more than a waive, a cancel or a hold needs to say")
	if status, raw := h.send(t, admin, deviationsPath(instanceID), huge); status != http.StatusBadRequest || !sameJSON(t, raw, tooLarge) {
		t.Errorf("a request of 300 KiB: %d (%.200s), want 400 %s", status, raw, tooLarge)
	}
	h.requireUnchanged(t, before, "requests that could not be read")
}
