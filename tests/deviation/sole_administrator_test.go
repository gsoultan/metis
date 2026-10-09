package deviation_test

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/gsoultan/metis/internal/app"
	"github.com/gsoultan/metis/internal/pkg/health"
	"github.com/gsoultan/metis/internal/pkg/platformadmins"
	"github.com/gsoultan/metis/server/domains/entities"
	observersimpl "github.com/gsoultan/metis/server/domains/observers/impl"
	"github.com/gsoultan/metis/server/domains/services"
	serviceimpl "github.com/gsoultan/metis/server/domains/services/impl"
	"github.com/gsoultan/metis/server/endpoints"
	"github.com/gsoultan/metis/server/interceptors/tenant"
	"github.com/gsoultan/metis/tests/testutils"
)

// Rulings §4: an organization with one administrator.
//
// A second administrator approves every waive. The one exception is an
// organization the installation's operator has named, by id, while nobody
// else administers it: there the administrator who asked may approve, saying
// why, and what is recorded says no second person did. These tests go through the
// routes, against accounts and memberships that are really there — who counts
// as "somebody else" is asked of the accounts, so accounts are what it is
// tested against.

const (
	// soleWaits is what a sole administrator approving their own request is
	// told while the installation does not allow it.
	soleWaits = "You asked for this, and nobody else administers this organization, so it waits. " +
		"Make another account an administrator so they can approve it, reject it yourself, or let it expire. " +
		"An installation run by one administrator can allow approving your own request: " +
		"see \"A second administrator approves waivers and skips\" in docs/upgrading.md."
	// anotherMustApprove is what the requester is told while somebody else
	// administers the organization, whatever the installation allows.
	anotherMustApprove = "You asked for this. A different administrator has to approve it."
	// sayWhy is what a self-approval with no reason is told.
	sayWhy = "Say why you are approving your own request: with nobody else to approve it, the reason is the record."
	// selfApprovalLogged is a fragment of the line each self-approval logs,
	// and refusedSelfApprovalLogged of the line a refused attempt logs.
	selfApprovalLogged        = "approved their own request"
	refusedSelfApprovalLogged = "tried to approve their own request"
)

// retiredSwitch is the name the exception had while it was a switch for the
// whole installation. It is not a setting.
const retiredSwitch = "METIS_ALLOW_SOLE_ADMINISTRATOR_SELF_APPROVAL"

// restarted is the tests' server started again over the same database, as an
// operator restarts it after changing its environment: the services are put
// together afresh, and read their settings as they do when a server starts.
// What the database holds — the organization, its project, its accounts — is
// as it was.
func (h *deviationHarness) restarted(t *testing.T) *deviationHarness {
	t.Helper()
	sse := observersimpl.NewSSEObserver()
	dispatcher := observersimpl.NewEventDispatcher()
	dispatcher.Register(observersimpl.NewAuditLogObserver(h.repo.Audit()))
	dispatcher.Register(observersimpl.NewNotificationObserver(serviceimpl.NewNotificationService(h.repo.Notification())))
	svc := services.NewServiceFacade(h.repo, dispatcher, sse, "deviation-test-secret", nil, nil, nil)
	handler, _ := app.BuildAPIHandler(svc, endpoints.MakeEndpoints(svc), sse, nil, map[string]health.Checker{}, testutils.StormConn(h.db))
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)
	again := *h
	again.server, again.svc, again.seconder = server, svc, ""
	return &again
}

// at is the organization and project of where, reached through h's server.
func (h *deviationHarness) at(where *deviationHarness) *deviationHarness {
	there := *h
	there.orgID, there.projID, there.deployed, there.seconder = where.orgID, where.projID, where.deployed, ""
	return &there
}

// soleOrganizations starts the tests' server with the list of organizations
// written as list says, given the organization the harness made. An
// organization has its id before an operator can name it, so the server is
// started, the list written, and the server started again — which is when
// the list is read.
func soleOrganizations(t *testing.T, list func(organization uuid.UUID) string) *deviationHarness {
	t.Helper()
	h := newDeviationRouteHarness(t)
	t.Setenv(serviceimpl.EnvSoleAdministratorOrganizations, list(h.orgID))
	return h.restarted(t)
}

// withOrganizationNamed starts the tests' server with the harness's
// organization named as one whose only administrator may approve their own
// request.
func withOrganizationNamed(t *testing.T) *deviationHarness {
	t.Helper()
	return soleOrganizations(t, uuid.UUID.String)
}

// enrolled is an account as a test wants it to be: what it holds on the
// account, where it belongs, and what it holds there.
type enrolled struct {
	// id is the account's id, when the test has to know it before the
	// account exists; otherwise one is made.
	id uuid.UUID
	// global is the roles held on the account: in every organization it
	// belongs to.
	global []string
	// here says the account belongs to the organization under test, and
	// rolesHere what it holds in that organization alone.
	here      bool
	rolesHere []string
	// elsewhere says it belongs to another organization as well (or only),
	// and rolesElsewhere what it holds in that one alone.
	elsewhere      bool
	rolesElsewhere []string
}

// enrol creates the account name as spec says, through the account service
// and the grants an administrator makes, signs it in and answers its id and
// its token. Everything is granted before the account's first request, so no
// request is ever answered from what the account used to be.
func (h *deviationHarness) enrol(t *testing.T, name string, spec enrolled) (uuid.UUID, string) {
	t.Helper()
	system := entities.WithSystemContext(context.Background())
	account := entities.User{ID: spec.id, Username: name, Roles: spec.global}
	if account.ID == uuid.Nil {
		account.ID = uuid.Must(uuid.NewV7())
	}
	grants := map[uuid.UUID][]string{}
	if spec.here {
		account.Organizations = append(account.Organizations, &entities.Organization{ID: h.orgID})
		grants[h.orgID] = spec.rolesHere
	}
	if spec.elsewhere {
		org, err := h.svc.CreateOrganization(context.Background(), "Elsewhere of "+name, "")
		if err != nil {
			t.Fatalf("create the other organization of %s: %v", name, err)
		}
		account.Organizations = append(account.Organizations, &entities.Organization{ID: org.ID})
		grants[org.ID] = spec.rolesElsewhere
	}
	if err := h.svc.CreateUser(system, account, harnessPassword); err != nil {
		t.Fatalf("create %s: %v", name, err)
	}
	for organization, roles := range grants {
		if len(roles) == 0 {
			continue
		}
		there := entities.WithTenantContext(system, entities.TenantContext{TenantID: organization.String()})
		if err := h.svc.SetOrganizationRoles(there, account.ID, roles); err != nil {
			t.Fatalf("give %s the roles %v in %s: %v", name, roles, organization, err)
		}
	}
	return account.ID, h.login(t, name)
}

// remove deletes an account, as the account service deletes one.
func (h *deviationHarness) remove(t *testing.T, id uuid.UUID) {
	t.Helper()
	if err := h.svc.DeleteUser(entities.WithSystemContext(context.Background()), id); err != nil {
		t.Fatalf("delete account %s: %v", id, err)
	}
}

func (h *deviationHarness) login(t *testing.T, name string) string {
	t.Helper()
	status, body := h.do(t, http.MethodPost, "", "/api/v1/login", map[string]string{"username": name, "password": harnessPassword})
	var out struct {
		Token string `json:"token"`
	}
	if err := json.Unmarshal([]byte(body), &out); status != http.StatusOK || err != nil || out.Token == "" {
		t.Fatalf("login %s: %d (%s)", name, status, body)
	}
	return out.Token
}

// decideHere sends a decision as a request for the harness's organization,
// named in the header an account of several organizations chooses one with.
func (h *deviationHarness) decideHere(t *testing.T, token, requestID, verb, reason string) (int, requestReply, string) {
	t.Helper()
	body := "{}"
	if reason != "" {
		encoded, err := json.Marshal(map[string]string{"reason": reason})
		if err != nil {
			t.Fatalf("encode the decision: %v", err)
		}
		body = string(encoded)
	}
	status, raw, _ := h.call(t, http.MethodPost, token, requestPath(requestID)+"/"+verb, body, tenant.OrganizationHeader, h.orgID.String())
	var reply requestReply
	if status == http.StatusOK {
		if err := json.Unmarshal([]byte(raw), &reply); err != nil {
			t.Fatalf("decode: %v (%s)", err, raw)
		}
	}
	return status, reply, raw
}

// posted is what a request sent from a goroutine of a test's own was answered.
type posted struct {
	status int
	raw    string
}

// post sends one request and answers what came back, failing nothing: it is
// for a request a test sends from a goroutine of its own, which may not fail
// the test itself.
func (h *deviationHarness) post(token, path, body string) (posted, error) {
	req, err := http.NewRequestWithContext(context.Background(), http.MethodPost, h.server.URL+path, strings.NewReader(body))
	if err != nil {
		return posted{}, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+token)
	resp, err := routeClient.Do(req)
	if err != nil {
		return posted{}, err
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(resp.Body)
	return posted{status: resp.StatusCode, raw: strings.TrimSpace(string(raw))}, err
}

// entriesOf is the trail entries of one type an instance has.
func (h *deviationHarness) entriesOf(t *testing.T, instanceID uuid.UUID, eventType string) []entities.AuditEntry {
	t.Helper()
	entries, err := h.svc.GetAuditLogs(h.tenantContext(), instanceID)
	if err != nil {
		t.Fatalf("read the trail: %v", err)
	}
	var found []entities.AuditEntry
	for _, entry := range entries {
		if entry.Type == eventType {
			found = append(found, entry)
		}
	}
	return found
}

// Off by default, and off unless the organization is named. Unset, empty,
// naming another organization, written as the switch it is not, or set under
// a name that is not this setting's: an administrator's approval of their own
// request is refused exactly as it was before the setting existed — alone in
// the organization or not — and changes nothing.
func TestASoleAdministratorsOwnApprovalWaitsUnlessTheirOrganizationIsNamed(t *testing.T) {
	for name, environment := range map[string]func(organization uuid.UUID) map[string]string{
		"unset": nil,
		"empty": func(uuid.UUID) map[string]string {
			return map[string]string{serviceimpl.EnvSoleAdministratorOrganizations: ""}
		},
		"naming another organization": func(uuid.UUID) map[string]string {
			return map[string]string{serviceimpl.EnvSoleAdministratorOrganizations: uuid.Must(uuid.NewV7()).String()}
		},
		"true, as if it were a switch": func(uuid.UUID) map[string]string {
			return map[string]string{serviceimpl.EnvSoleAdministratorOrganizations: "true"}
		},
		"the organization's name, which is not its id": func(uuid.UUID) map[string]string {
			return map[string]string{serviceimpl.EnvSoleAdministratorOrganizations: "Deviation Org"}
		},
		"the switch it used to be, set to true": func(uuid.UUID) map[string]string {
			return map[string]string{retiredSwitch: "true"}
		},
		"the organization named under the spelling from before the rename": func(organization uuid.UUID) map[string]string {
			return map[string]string{"GOBPM_SOLE_ADMINISTRATOR_ORGANIZATIONS": organization.String()}
		},
	} {
		t.Run(name, func(t *testing.T) {
			h := newDeviationRouteHarness(t)
			// Setenv first, so that the test puts back what was there.
			t.Setenv(serviceimpl.EnvSoleAdministratorOrganizations, "")
			if err := os.Unsetenv(serviceimpl.EnvSoleAdministratorOrganizations); err != nil {
				t.Fatalf("unset the setting: %v", err)
			}
			if environment != nil {
				for variable, value := range environment(h.orgID) {
					t.Setenv(variable, value)
				}
			}
			h = h.restarted(t)
			logs := captureLogs(t)
			instanceID := h.oneStep(t)
			boss := h.signIn(t, "boss", entities.RoleAdmin)
			h.signIn(t, "clerk", entities.RoleUser)
			requestID := h.askToWaive(t, boss, instanceID)
			before := h.everyRow(t)

			want := refusal("forbidden", soleWaits)
			for _, reason := range []string{"", decisionReason} {
				if status, _, raw := h.decide(t, boss, requestID, "approve", reason); status != http.StatusForbidden || !sameJSON(t, raw, want) {
					t.Fatalf("a sole administrator approving their own request (reason %q): %d (%s), want 403 %s", reason, status, raw, want)
				}
			}
			h.requireUnchanged(t, before, "a sole administrator's refused approval")
			if refused, made := logs.said(refusedSelfApprovalLogged), logs.said(selfApprovalLogged); len(refused) != 2 || len(made) != 0 {
				t.Fatalf("two refused attempts logged %v as refused and %v as made; want the two refusals and nothing else", refused, made)
			}

			// With somebody else to ask, it is the other refusal.
			deputy := h.signIn(t, "deputy", entities.RoleAdmin)
			want = refusal("forbidden", anotherMustApprove)
			if status, _, raw := h.decide(t, boss, requestID, "approve", decisionReason); status != http.StatusForbidden || !sameJSON(t, raw, want) {
				t.Fatalf("the requester approving with another administrator there: %d (%s), want 403 %s", status, raw, want)
			}
			if status, approved, raw := h.decide(t, deputy, requestID, "approve", ""); status != http.StatusOK || approved.Request.SelfApproved {
				t.Fatalf("the other administrator approves: %d (%s)", status, raw)
			}
		})
	}
}

// The exception is an organization's, not the installation's. Where two
// organizations share an installation and one is named, the only
// administrator of the named one may approve their own request and the only
// administrator of the other may not. An entry beside it that is not an id
// names nobody and takes nothing from the entry that is.
//
// And it is why organizations are named at all: in the organization that is
// not named, an administrator who takes the other administrator's role away
// has made themselves the only one — and is still refused.
func TestTheExceptionIsForTheOrganizationsNamedAndForNoOther(t *testing.T) {
	base := newDeviationRouteHarness(t)
	unnamed := base.inAnotherOrganization(t, "Not Named")
	t.Setenv(serviceimpl.EnvSoleAdministratorOrganizations, "Acme Ltd, "+base.orgID.String()+" ,0199,,"+uuid.Nil.String())
	here := base.restarted(t)
	there := here.at(unnamed)

	// Named, and alone: approved.
	instanceID := here.oneStep(t)
	boss := here.signIn(t, "boss", entities.RoleAdmin)
	requestID := here.askToWaive(t, boss, instanceID)
	if status, approved, raw := here.decide(t, boss, requestID, "approve", decisionReason); status != http.StatusOK || !approved.Request.SelfApproved {
		t.Fatalf("the only administrator of the named organization: %d (%s), want it applied and self-approved", status, raw)
	}

	// Not named, and alone: it waits.
	elsewhere := there.oneStep(t)
	chief := there.signIn(t, "chief", entities.RoleAdmin)
	second, _ := there.enrol(t, "second", enrolled{here: true, rolesHere: []string{entities.RoleAdmin}})
	waiting := there.askToWaive(t, chief, elsewhere)
	want := refusal("forbidden", anotherMustApprove)
	if status, _, raw := there.decide(t, chief, waiting, "approve", decisionReason); status != http.StatusForbidden || !sameJSON(t, raw, want) {
		t.Fatalf("one of two administrators of the organization that is not named: %d (%s), want 403 %s", status, raw, want)
	}
	// The chief takes the second administrator's role away, over the API, as
	// an administrator may, and is the only one left.
	if status, raw, _ := there.call(t, http.MethodPut, chief, "/api/v1/users/"+second.String()+"/organization-roles", `{"roles":[]}`); status != http.StatusOK {
		t.Fatalf("the chief taking the second administrator's role away: %d (%s)", status, raw)
	}
	before := there.everyRow(t)
	want = refusal("forbidden", soleWaits)
	if status, _, raw := there.decide(t, chief, waiting, "approve", decisionReason); status != http.StatusForbidden || !sameJSON(t, raw, want) {
		t.Fatalf("an administrator who made themselves the only one, in an organization that is not named: %d (%s), want 403 %s", status, raw, want)
	}
	there.requireUnchanged(t, before, "the refused approval")
	if there.requestStatus(t, waiting) != "pending_approval" || !there.stepIsOpen(t, elsewhere) {
		t.Fatal("the refused self-approval changed something")
	}
}

// Named, and somebody else administers the organization: the requester is
// still refused, in the words that say a different administrator has to approve —
// with a reason or without — and nothing changes. The other administrator's
// approval is then an ordinary one: nothing in the record says "self".
func TestWithAnotherAdministratorTheRequesterIsRefusedWhateverTheSetting(t *testing.T) {
	h := withOrganizationNamed(t)
	logs := captureLogs(t)
	instanceID := h.oneStep(t)
	boss, deputy := h.signIn(t, "boss", entities.RoleAdmin), h.signIn(t, "deputy", entities.RoleAdmin)
	requestID := h.askToWaive(t, boss, instanceID)
	before := h.everyRow(t)

	want := refusal("forbidden", anotherMustApprove)
	for _, reason := range []string{"", decisionReason} {
		if status, _, raw := h.decide(t, boss, requestID, "approve", reason); status != http.StatusForbidden || !sameJSON(t, raw, want) {
			t.Fatalf("boss approving his own request (reason %q): %d (%s), want 403 %s", reason, status, raw, want)
		}
	}
	h.requireUnchanged(t, before, "the requester's refused approval")
	if h.requestStatus(t, requestID) != "pending_approval" || !h.stepIsOpen(t, instanceID) {
		t.Fatal("a refused self-approval changed something")
	}
	if refused := logs.said(refusedSelfApprovalLogged); len(refused) != 2 || refused[0]["actor"] != "boss" {
		t.Fatalf("the refused attempts logged %v, want one line each, naming boss", refused)
	}

	status, approved, raw := h.decide(t, deputy, requestID, "approve", "")
	if status != http.StatusOK || !approved.Applied || approved.Request.SelfApproved || approved.Request.DecidedBy != "deputy" {
		t.Fatalf("deputy approves: %d (%s)", status, raw)
	}
	if strings.Contains(raw, "self_approved\":true") || strings.Contains(raw, "other_administrators") {
		t.Fatalf("a second administrator's approval says self_approved, or counts the other administrators, somewhere: %s", raw)
	}
	if said := h.entriesOf(t, instanceID, serviceimpl.EventDeviationSelfApproved); len(said) != 0 {
		t.Fatalf("the trail says somebody approved their own request: %+v", said)
	}
	second := h.entriesOf(t, instanceID, serviceimpl.EventDeviationApproved)
	if len(second) != 1 {
		t.Fatalf("the trail holds %d entries of a second administrator's approval, want 1", len(second))
	}
	if _, counted := second[0].Data["other_administrators"]; counted {
		t.Fatalf("a second administrator's approval counts the other administrators: %v", second[0].Data)
	}
	if made := logs.said(selfApprovalLogged); len(made) != 0 {
		t.Fatalf("a second administrator's approval was logged as a self-approval: %v", made)
	}
}

// Named, and nobody else administers the organization: the administrator who
// asked may approve — saying why — and everything that records it says no
// second person did: the reply, the request, the ledger row, the trail and
// the server's log. Nothing reads as if somebody else had looked at it.
func TestASoleAdministratorApprovesTheirOwnRequestAndTheRecordSaysNobodyElseDid(t *testing.T) {
	h := withOrganizationNamed(t)
	logs := captureLogs(t)
	h.signIn(t, "alice", entities.RoleUser)
	instanceID := h.oneStep(t)
	boss := h.signIn(t, "boss", entities.RoleAdmin)
	h.signIn(t, "clerk", entities.RoleUser)
	requestID := h.askToWaive(t, boss, instanceID)
	before := h.everyRow(t)

	// A reason is required: with nobody else to approve it, it is the record.
	want := invalid(sayWhy)
	for _, reason := range []string{"", "   ", "\n\t"} {
		if status, _, raw := h.decideHere(t, boss, requestID, "approve", reason); status != http.StatusBadRequest || !sameJSON(t, raw, want) {
			t.Fatalf("a self-approval with the reason %q: %d (%s), want 400 %s", reason, status, raw, want)
		}
	}
	long := strings.Repeat("x", entities.MaxDeviationReasonLength+1)
	tooLong := invalid(fmt.Sprintf("The reason is longer than %d characters; say it more briefly.", entities.MaxDeviationReasonLength))
	if status, _, raw := h.decide(t, boss, requestID, "approve", long); status != http.StatusBadRequest || !sameJSON(t, raw, tooLong) {
		t.Fatalf("a self-approval with a reason too long to keep: %d (%.200s), want 400 %s", status, raw, tooLong)
	}
	h.requireUnchanged(t, before, "a self-approval that did not say why")
	if said := append(logs.said(selfApprovalLogged), logs.said("could not be applied")...); len(said) != 0 {
		t.Fatalf("self-approvals that were not made were logged: %v", said)
	}

	const why = "the only other administrator left last week; the director agreed in writing"
	status, approved, raw := h.decide(t, boss, requestID, "approve", "  "+why+"  ")
	if status != http.StatusOK || !approved.Applied || approved.Request.Status != "applied" || approved.Request.RequestedBy != "boss" ||
		approved.Request.DecidedBy != "boss" || approved.Request.DecisionReason != why || !approved.Request.SelfApproved ||
		approved.Request.DecidedAt.IsZero() {
		t.Fatalf("a sole administrator's self-approval: %d (%s), want it applied, by boss, for the reason given, and self-approved", status, raw)
	}
	written := object(t, raw)
	requireFields(t, written, "a self-approval", "request", "applied", "deviation", "plan")
	requireFields(t, written["request"], "a self-approved request",
		append([]string{"instance_id", "decided_by", "decided_at", "decision_reason"}, requestFields...)...)
	if nulls := nullsIn(written, "reply"); len(nulls) != 0 {
		t.Fatalf("the self-approval holds null at %v: %s", nulls, raw)
	}
	// The row the reply carries is the ledger's, and says the same.
	details, _ := approved.Deviation["details"].(map[string]any)
	if approved.Deviation["status"] != "applied" || approved.Deviation["actor"] != "boss" || approved.Deviation["approved_by"] != "boss" ||
		details["self_approved"] != true || fmt.Sprint(details["other_administrators"]) != "0" {
		t.Fatalf("the row of a self-approval: %v, want it applied, asked for and approved by boss, marked self_approved, "+
			"and saying no other administrator was found", approved.Deviation)
	}
	for who, account := range map[string]string{"the requester": h.accountID(t, "boss").String(), "the holder": h.accountID(t, "alice").String()} {
		if strings.Contains(raw, account) || strings.Contains(raw, "_by_id") {
			t.Errorf("the self-approval carries the account id of %s: %s", who, raw)
		}
	}
	if h.stepIsOpen(t, instanceID) {
		t.Fatal("the self-approved waive left the step open")
	}

	// The request, as stored and as read again: decided by the account that
	// asked.
	var requestedBy, decidedBy, stored, reason string
	if err := h.db.Raw(`SELECT requested_by_id::text, decided_by_id::text, status, decision_reason FROM deviation_requests WHERE id = ?`, requestID).
		Row().Scan(&requestedBy, &decidedBy, &stored, &reason); err != nil {
		t.Fatalf("read the request: %v", err)
	}
	if stored != "applied" || decidedBy != requestedBy || decidedBy != h.accountID(t, "boss").String() || reason != why {
		t.Fatalf("the request is stored %s, asked by %s and decided by %s for %q; want applied, by boss's account both times", stored, requestedBy, decidedBy, reason)
	}
	if status, read, raw := h.readRequest(t, boss, requestID); status != http.StatusOK || !read.Request.SelfApproved || read.Request.Status != "applied" {
		t.Fatalf("the request read again: %d (%s), want it applied and self-approved", status, raw)
	}
	if status, listed, raw := h.queue(t, boss, "status=applied"); status != http.StatusOK || len(listed.Requests) != 1 || listed.Requests[0]["self_approved"] != true {
		t.Fatalf("the request in the queue of applied requests: %d (%s), want it listed as self-approved", status, raw)
	}

	// The ledger row.
	rows, err := h.svc.ListInstanceDeviations(h.tenantContext(), instanceID)
	if err != nil || len(rows) != 1 {
		t.Fatalf("the ledger: %+v, %v; want the one row", rows, err)
	}
	row := rows[0]
	if row.Status != entities.DeviationApplied || row.Actor != "boss" || row.ApprovedBy != "boss" || row.ApprovedByID != row.ActorID ||
		row.ActorID != h.accountID(t, "boss") || row.Details["self_approved"] != true || fmt.Sprint(row.Details["other_administrators"]) != "0" {
		t.Fatalf("the ledger row: %+v, want it applied, asked for and approved by one account, marked self_approved, "+
			"and saying how many other administrators the organization had: none", row)
	}

	// The trail: an entry that says so in so many words, with the reason, in
	// place of the entry a second administrator's approval leaves; and the
	// waived step's own entry, marked.
	said := h.entriesOf(t, instanceID, serviceimpl.EventDeviationSelfApproved)
	wantSentence := "No second administrator approved this. boss approved their own request to waive “Approve”, " +
		"which this installation allows only while nobody else administers the organization. Reason: " + why
	if len(said) != 1 || said[0].Narrative != wantSentence {
		t.Fatalf("the trail's entry of the self-approval: %+v\nwant one that reads\n  %s", said, wantSentence)
	}
	if said[0].Data["self_approved"] != true || said[0].Data["approved_by"] != "boss" || said[0].Data["requested_by"] != "boss" ||
		said[0].Data["request_id"] != requestID || said[0].Data["deviation_id"] != row.ID.String() ||
		fmt.Sprint(said[0].Data["other_administrators"]) != "0" {
		t.Fatalf("the self-approval's entry carries %v, want it to say as well that no other administrator was found", said[0].Data)
	}
	if second := h.entriesOf(t, instanceID, serviceimpl.EventDeviationApproved); len(second) != 0 {
		t.Fatalf("the trail says a second administrator approved: %+v", second)
	}
	skipped := h.entriesOf(t, instanceID, serviceimpl.EventNodeSkipped)
	if len(skipped) != 1 || skipped[0].Data["self_approved"] != true || skipped[0].Data["approved_by"] != "boss" ||
		skipped[0].Data["request_id"] != requestID {
		t.Fatalf("the waived step's entry: %+v, want it marked self_approved and naming the request", skipped)
	}
	for _, entry := range append(skipped, said...) {
		for _, untrue := range []string{"second administrator approved it", "A second administrator,", "approved by a second"} {
			if strings.Contains(entry.Narrative, untrue) {
				t.Errorf("the %s entry reads as if somebody else had approved: %s", entry.Type, entry.Narrative)
			}
		}
	}

	// The server's log: one line of its own, naming the setting, the request
	// and the account — by id as well as by name — and nothing that was asked
	// for or said. It is not the line a refused attempt leaves.
	lines := logs.said(selfApprovalLogged)
	if len(lines) != 1 || lines[0]["level"] != "warn" || lines[0]["setting"] != serviceimpl.EnvSoleAdministratorOrganizations ||
		lines[0]["request"] != requestID || lines[0]["actor"] != "boss" || lines[0]["actor_id"] != h.accountID(t, "boss").String() {
		t.Fatalf("the log holds %v, want one warning naming the setting, request %s and boss by name and account id", lines, requestID)
	}
	if line, _ := json.Marshal(lines[0]); strings.Contains(string(line), why) || strings.Contains(string(line), routeReason) {
		t.Fatalf("the log line carries what was asked for or why: %s", line)
	}
	if refused := logs.said(refusedSelfApprovalLogged); len(refused) != 0 {
		t.Fatalf("an admitted self-approval was logged as a refused one: %v", refused)
	}

	// Decided, it is decided: nobody decides it again, its requester included.
	closed := h.everyRow(t)
	want = invalid(fmt.Sprintf("boss approved this on %s, and it was applied.", approved.Request.DecidedAt.UTC().Format(decidedOn)))
	for _, verb := range []string{"approve", "reject"} {
		if status, _, raw := h.decide(t, boss, requestID, verb, why); status != http.StatusBadRequest || !sameJSON(t, raw, want) {
			t.Fatalf("boss's %s of a request already approved: %d (%s), want 400 %s", verb, status, raw, want)
		}
	}
	h.requireUnchanged(t, closed, "deciding a self-approved request again")
}

// candidate is an account that may or may not be "another administrator" of
// the organization under test.
type candidate struct {
	name string
	spec enrolled
	// deleted says the account is deleted once it has signed in.
	deleted bool
	// platform says the account is named in METIS_PLATFORM_ADMINS: the test
	// names it there before the server is put together.
	platform bool
	// administers says whether the account administers the organization:
	// whether it could approve the request, and so whether it counts.
	administers bool
	// refused is what the approve route answers the account's own approval,
	// sent for the organization under test, when it does not administer it:
	// 401 for an account that is gone or does not belong, 403 for a member
	// without the role.
	refused int
}

// "Nobody else administers the organization" and "nobody else could approve
// this request" are one question. An account that can pass the approval's own
// checks for this organization counts as another administrator, and takes the
// choice away; an account that cannot does not, and its approval is refused.
// Each kind of account is asked both ways, through the approve route: the
// requester's own approval is admitted exactly when the candidate's is not.
//
// Each case has an organization of its own in one installation, every one of
// them named, so every case also has, beside its candidate, the administrators of every other
// case's organization — accounts that hold the role in every organization
// they belong to, and do not belong to this one.
func TestWhoCountsAsAnotherAdministratorIsWhoCouldApprove(t *testing.T) {
	admin, user := []string{entities.RoleAdmin}, []string{entities.RoleUser}
	named, namedOutsider := uuid.Must(uuid.NewV7()), uuid.Must(uuid.NewV7())
	cases := []candidate{
		{name: "an administrator here, by a role held on the account",
			spec: enrolled{global: admin, here: true}, administers: true},
		{name: "an administrator here, by a role held in this organization alone",
			spec: enrolled{here: true, rolesHere: admin}, administers: true},
		{name: "an administrator here whose role is written in lower case",
			spec: enrolled{global: []string{"admin"}, here: true}, administers: true},
		{name: "an administrator here and of another organization",
			spec: enrolled{global: admin, here: true, elsewhere: true}, administers: true},
		{name: "an administrator here by a role held here, who is a plain member elsewhere",
			spec: enrolled{here: true, rolesHere: admin, elsewhere: true}, administers: true},
		{name: "a member who holds no administrator role",
			spec: enrolled{global: user, here: true}, refused: http.StatusForbidden},
		{name: "an operator and designer here",
			spec: enrolled{here: true, rolesHere: []string{entities.RoleOperator, entities.RoleDesigner}}, refused: http.StatusForbidden},
		{name: "an administrator of another organization only, by a role held on the account",
			spec: enrolled{global: admin, elsewhere: true}, refused: http.StatusUnauthorized},
		{name: "a member here who administers another organization, by a role held there",
			spec: enrolled{here: true, elsewhere: true, rolesElsewhere: admin}, refused: http.StatusForbidden},
		{name: "an account whose role is ADMINISTRATOR, which is no role",
			spec: enrolled{global: []string{"ADMINISTRATOR"}, here: true}, refused: http.StatusForbidden},
		{name: "an administrator here whose account was deleted, role held on the account",
			spec: enrolled{global: admin, here: true}, deleted: true, refused: http.StatusUnauthorized},
		{name: "an administrator here whose account was deleted, role held here",
			spec: enrolled{here: true, rolesHere: admin}, deleted: true, refused: http.StatusUnauthorized},
		{name: "a platform administrator who is a plain member here",
			spec: enrolled{id: named, global: user, here: true}, platform: true, refused: http.StatusForbidden},
		{name: "a platform administrator who does not belong here",
			spec: enrolled{id: namedOutsider, global: admin, elsewhere: true}, platform: true, refused: http.StatusUnauthorized},
	}
	// The list is read when the server is put together.
	var listed []string
	for _, c := range cases {
		if c.platform {
			listed = append(listed, c.spec.id.String())
		}
	}
	if len(listed) != 2 {
		t.Fatalf("%d platform administrators are named, want the two cases", len(listed))
	}
	t.Setenv(platformadmins.Env, strings.Join(listed, ","))
	// Every case's organization is made, then named, then the server is
	// started again: the list of them is read when it starts.
	base := newDeviationRouteHarness(t)
	organizations, ids := make([]*deviationHarness, len(cases)), make([]string, len(cases))
	for i := range cases {
		organizations[i] = base.inAnotherOrganization(t, fmt.Sprintf("Organization %d", i))
		ids[i] = organizations[i].orgID.String()
	}
	t.Setenv(serviceimpl.EnvSoleAdministratorOrganizations, strings.Join(ids, ","))
	installation := base.restarted(t)

	for i, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			h := installation.at(organizations[i])
			boss := h.signIn(t, fmt.Sprintf("boss-%d", i), entities.RoleAdmin)
			id, token := h.enrol(t, fmt.Sprintf("candidate-%d", i), c.spec)
			if c.deleted {
				h.remove(t, id)
			}
			instanceID := h.oneStep(t)
			requestID := h.askToWaive(t, boss, instanceID)

			if c.administers {
				// The requester first: refused, because the candidate could.
				want := refusal("forbidden", anotherMustApprove)
				if status, _, raw := h.decideHere(t, boss, requestID, "approve", decisionReason); status != http.StatusForbidden || !sameJSON(t, raw, want) {
					t.Fatalf("the requester's own approval: %d (%s), want 403 %s — the candidate administers this organization", status, raw, want)
				}
				status, approved, raw := h.decideHere(t, token, requestID, "approve", "")
				if status != http.StatusOK || !approved.Applied || approved.Request.SelfApproved {
					t.Fatalf("the candidate's approval: %d (%s), want it applied — they were counted as another administrator", status, raw)
				}
				return
			}
			// The candidate first: refused, whichever organization the
			// request is sent for, and nothing changes.
			before := h.everyRow(t)
			if status, _, raw := h.decideHere(t, token, requestID, "approve", decisionReason); status != c.refused {
				t.Fatalf("the candidate's approval, sent for this organization: %d (%s), want %d", status, raw, c.refused)
			}
			if status, _, raw := h.decide(t, token, requestID, "approve", decisionReason); status == http.StatusOK ||
				(status != http.StatusUnauthorized && status != http.StatusForbidden && status != http.StatusNotFound) {
				t.Fatalf("the candidate's approval, sent for the organization they sign in to: %d (%s), want it refused", status, raw)
			}
			h.requireUnchanged(t, before, "the candidate's refused approval")
			status, approved, raw := h.decideHere(t, boss, requestID, "approve", decisionReason)
			if status != http.StatusOK || !approved.Applied || !approved.Request.SelfApproved {
				t.Fatalf("the requester's own approval: %d (%s), want it applied and self-approved — the candidate could not approve", status, raw)
			}
		})
	}
}

// An account an identity provider signs in belongs where its latest sign-in
// put it: the organizations that sign-in's claim named, written as its
// memberships before the request goes on. So it is counted, and can approve,
// in exactly those — an administrator here while the provider names this
// organization for it, and nobody here once a sign-in no longer does.
//
// The tests' server signs local accounts in, not a provider's, so whether
// such an account's own approval would be let through is asked of the
// principal its sign-in answers: the organizations a request of its can be
// for, and the role it holds there — what the tenant resolver and the role
// gate read.
func TestAnAccountAnIdentityProviderPlacesIsCountedWhereItsLatestSignInPutIt(t *testing.T) {
	h := withOrganizationNamed(t)
	elsewhere := h.inAnotherOrganization(t, "Elsewhere")
	instanceID := h.oneStep(t)
	boss := h.signIn(t, "boss", entities.RoleAdmin)
	requestID := h.askToWaive(t, boss, instanceID)

	signedIn := func(organizations ...uuid.UUID) entities.User {
		t.Helper()
		claims := entities.IdentityClaims{Issuer: "https://idp.example", Subject: "sub-dita", Username: "dita",
			OrganizationClaim: "orgs", HasOrganizationClaim: true}
		for _, organization := range organizations {
			claims.Organizations = append(claims.Organizations, organization.String())
		}
		account, err := h.svc.SignInThroughIdentityProvider(context.Background(), claims)
		if err != nil {
			t.Fatalf("sign dita in through the provider: %v", err)
		}
		return account
	}
	couldApproveHere := func(account entities.User) bool {
		belongs := false
		for _, organization := range account.Organizations {
			belongs = belongs || (organization != nil && organization.ID == h.orgID)
		}
		return belongs && account.HoldsRoleIn(h.orgID, entities.RoleAdmin)
	}

	// Placed here, and made an administrator here by an administrator.
	dita := signedIn(h.orgID, elsewhere.orgID)
	there := entities.WithTenantContext(entities.WithSystemContext(context.Background()), entities.TenantContext{TenantID: h.orgID.String()})
	if err := h.svc.SetOrganizationRoles(there, dita.ID, []string{entities.RoleAdmin}); err != nil {
		t.Fatalf("make dita an administrator here: %v", err)
	}
	if dita = signedIn(h.orgID, elsewhere.orgID); !couldApproveHere(dita) {
		t.Fatalf("dita, placed here and made an administrator, could not approve here: %+v", dita)
	}
	want := refusal("forbidden", anotherMustApprove)
	if status, _, raw := h.decide(t, boss, requestID, "approve", decisionReason); status != http.StatusForbidden || !sameJSON(t, raw, want) {
		t.Fatalf("boss approving his own request while dita administers: %d (%s), want 403 %s", status, raw, want)
	}

	// The provider stops naming this organization: dita's next sign-in takes
	// her out of it, and the role she held here goes with the membership.
	if dita = signedIn(elsewhere.orgID); couldApproveHere(dita) {
		t.Fatalf("dita, no longer placed here, could still approve here: %+v", dita)
	}
	if status, approved, raw := h.decide(t, boss, requestID, "approve", decisionReason); status != http.StatusOK || !approved.Request.SelfApproved {
		t.Fatalf("boss approving his own request once dita is gone: %d (%s), want it applied and self-approved", status, raw)
	}
}

// Who administers the organization is asked when the approval is made, of
// the accounts as they then are — not when the request was made.
func TestTheAdministratorsAreCountedWhenTheApprovalIsMade(t *testing.T) {
	t.Run("an administrator appointed while it waited takes the choice away", func(t *testing.T) {
		h := withOrganizationNamed(t)
		instanceID := h.oneStep(t)
		boss := h.signIn(t, "boss", entities.RoleAdmin)
		clerk, _ := h.enrol(t, "clerk", enrolled{global: []string{entities.RoleUser}, here: true})
		requestID := h.askToWaive(t, boss, instanceID)

		there := entities.WithTenantContext(entities.WithSystemContext(context.Background()), entities.TenantContext{TenantID: h.orgID.String()})
		if err := h.svc.SetOrganizationRoles(there, clerk, []string{entities.RoleAdmin}); err != nil {
			t.Fatalf("make the clerk an administrator: %v", err)
		}
		want := refusal("forbidden", anotherMustApprove)
		if status, _, raw := h.decide(t, boss, requestID, "approve", decisionReason); status != http.StatusForbidden || !sameJSON(t, raw, want) {
			t.Fatalf("self-approval with a second administrator now present: %d (%s), want 403 %s", status, raw, want)
		}
		if h.requestStatus(t, requestID) != "pending_approval" || !h.stepIsOpen(t, instanceID) {
			t.Fatal("the refused self-approval changed something")
		}
	})

	t.Run("the only other administrator gone, the requester may approve", func(t *testing.T) {
		h := withOrganizationNamed(t)
		instanceID := h.oneStep(t)
		boss := h.signIn(t, "boss", entities.RoleAdmin)
		former, _ := h.enrol(t, "former", enrolled{here: true, rolesHere: []string{entities.RoleAdmin}})
		requestID := h.askToWaive(t, boss, instanceID)
		if status, _, raw := h.decide(t, boss, requestID, "approve", decisionReason); status != http.StatusForbidden {
			t.Fatalf("self-approval while the other administrator is there: %d (%s), want 403", status, raw)
		}
		there := entities.WithTenantContext(entities.WithSystemContext(context.Background()), entities.TenantContext{TenantID: h.orgID.String()})
		if err := h.svc.SetOrganizationRoles(there, former, []string{entities.RoleOperator}); err != nil {
			t.Fatalf("take the administrator role from the other administrator: %v", err)
		}
		if status, approved, raw := h.decide(t, boss, requestID, "approve", decisionReason); status != http.StatusOK || !approved.Request.SelfApproved {
			t.Fatalf("self-approval once nobody else administers: %d (%s), want it applied and self-approved", status, raw)
		}
	})

	// The count is made by the approval, after it holds the request — not
	// before it waits for it. An approval that waits behind somebody else
	// holding the request, while a second administrator is appointed, finds
	// that administrator when its turn comes.
	t.Run("an administrator appointed while the approval waited for the request is counted", func(t *testing.T) {
		h := withOrganizationNamed(t)
		instanceID := h.oneStep(t)
		boss := h.signIn(t, "boss", entities.RoleAdmin)
		clerk, _ := h.enrol(t, "clerk", enrolled{global: []string{entities.RoleUser}, here: true})
		requestID := h.askToWaive(t, boss, instanceID)

		holder := h.holdOpen(t, `SELECT 1 FROM deviation_requests WHERE id = ? FOR UPDATE`, requestID)
		var got posted
		finished := make(chan error, 1)
		go func() {
			var err error
			got, err = h.post(boss, requestPath(requestID)+"/approve", `{"reason":"`+decisionReason+`"}`)
			finished <- err
		}()
		holder.untilBehind(finished)
		there := entities.WithTenantContext(entities.WithSystemContext(context.Background()), entities.TenantContext{TenantID: h.orgID.String()})
		if err := h.svc.SetOrganizationRoles(there, clerk, []string{entities.RoleAdmin}); err != nil {
			t.Fatalf("make the clerk an administrator: %v", err)
		}
		holder.undo()
		if err := answerOf(t, finished, "the approval that waited"); err != nil {
			t.Fatalf("the approval that waited: %v", err)
		}
		want := refusal("forbidden", anotherMustApprove)
		if got.status != http.StatusForbidden || !sameJSON(t, got.raw, want) {
			t.Fatalf("the approval that waited while a second administrator was appointed: %d (%s), want 403 %s", got.status, got.raw, want)
		}
		if h.requestStatus(t, requestID) != "pending_approval" || !h.stepIsOpen(t, instanceID) {
			t.Fatal("the refused self-approval changed something")
		}
	})

	// What is left of the race, and which way it fails. A grant that has been
	// made and not yet committed is nobody's role yet: the account it is for
	// cannot approve, and the count does not see it. So an approval made at
	// that moment is admitted, and the second administrator appears just
	// after it. The other order — the grant committed, then the approval —
	// is the first subtest, and is refused.
	t.Run("a grant not yet committed is not yet an administrator", func(t *testing.T) {
		h := withOrganizationNamed(t)
		instanceID := h.oneStep(t)
		boss := h.signIn(t, "boss", entities.RoleAdmin)
		clerk, clerkToken := h.enrol(t, "clerk", enrolled{global: []string{entities.RoleUser}, here: true})
		requestID := h.askToWaive(t, boss, instanceID)

		granting := h.holdOpen(t, `UPDATE user_organizations SET roles = '["ADMIN"]' WHERE user_id = ? AND organization_id = ?`, clerk, h.orgID)
		if status, _, raw := h.decide(t, clerkToken, requestID, "approve", ""); status != http.StatusForbidden {
			t.Fatalf("the clerk approving on a grant not yet committed: %d (%s), want 403", status, raw)
		}
		status, approved, raw := h.decide(t, boss, requestID, "approve", decisionReason)
		if status != http.StatusOK || !approved.Request.SelfApproved {
			t.Fatalf("self-approval while the grant is not committed: %d (%s), want it applied and self-approved", status, raw)
		}
		if err := granting.commit(); err != nil {
			t.Fatalf("commit the grant: %v", err)
		}
	})
}

// The list is the server's, read when it starts. Nothing a request carries
// adds an organization to it, and changing the environment under a running
// server changes nothing.
func TestNothingButTheListReadAtStartLetsARequesterApprove(t *testing.T) {
	t.Run("not named at start: no request, and no later change of the environment, names it", func(t *testing.T) {
		h := soleOrganizations(t, func(uuid.UUID) string { return uuid.Must(uuid.NewV7()).String() })
		instanceID := h.oneStep(t)
		boss := h.signIn(t, "boss", entities.RoleAdmin)
		requestID := h.askToWaive(t, boss, instanceID)
		before := h.everyRow(t)

		organization := h.orgID.String()
		t.Setenv(serviceimpl.EnvSoleAdministratorOrganizations, organization)
		approve := requestPath(requestID) + "/approve"
		reason := `{"reason":"` + decisionReason + `"}`
		want := refusal("forbidden", soleWaits)
		for name, send := range map[string]func() (int, string, bool){
			"the environment changed under the server": func() (int, string, bool) {
				return h.call(t, http.MethodPost, boss, approve, reason)
			},
			"headers named after the setting": func() (int, string, bool) {
				return h.call(t, http.MethodPost, boss, approve, reason,
					"X-Metis-Sole-Administrator-Organizations", organization, "X-Sole-Administrator-Organization", organization,
					"X-Self-Approved", "true", tenant.OrganizationHeader, organization)
			},
			"the setting in the address": func() (int, string, bool) {
				return h.call(t, http.MethodPost, boss, approve+"?sole_administrator_organizations="+organization+"&self_approved=true&"+
					serviceimpl.EnvSoleAdministratorOrganizations+"="+organization, reason)
			},
		} {
			if status, raw, _ := send(); status != http.StatusForbidden || !sameJSON(t, raw, want) {
				t.Fatalf("%s: %d (%s), want 403 %s", name, status, raw, want)
			}
		}
		// A body that says more than a reason is not a decision at all.
		for _, body := range []string{
			`{"reason":"x","self_approved":true}`,
			`{"reason":"x","sole_administrator_organizations":["` + organization + `"]}`,
			`{"reason":"x","` + serviceimpl.EnvSoleAdministratorOrganizations + `":"` + organization + `"}`,
			`{"reason":"x","other_administrators":0}`,
			`{"reason":"x","organization_id":"` + organization + `"}`,
			`{"reason":"x","decided_by":"deputy"}`,
		} {
			if status, raw, _ := h.call(t, http.MethodPost, boss, approve, body); status != http.StatusBadRequest {
				t.Fatalf("a decision that says %s: %d (%s), want 400", body, status, raw)
			}
		}
		h.requireUnchanged(t, before, "a request that tried to name its own organization")
	})

	t.Run("named at start: taking it off the list takes a restart", func(t *testing.T) {
		h := withOrganizationNamed(t)
		instanceID := h.oneStep(t)
		boss := h.signIn(t, "boss", entities.RoleAdmin)
		requestID := h.askToWaive(t, boss, instanceID)
		t.Setenv(serviceimpl.EnvSoleAdministratorOrganizations, "")
		if status, approved, raw := h.decide(t, boss, requestID, "approve", decisionReason); status != http.StatusOK || !approved.Request.SelfApproved {
			t.Fatalf("self-approval on a server started with the organization named: %d (%s), want it applied", status, raw)
		}
		// Started again with the list as it now is, the next request waits.
		again := h.restarted(t)
		next := again.oneStep(t)
		waiting := again.askToWaive(t, again.login(t, "boss"), next)
		want := refusal("forbidden", soleWaits)
		if status, _, raw := again.decide(t, again.login(t, "boss"), waiting, "approve", decisionReason); status != http.StatusForbidden || !sameJSON(t, raw, want) {
			t.Fatalf("self-approval after a restart with the organization off the list: %d (%s), want 403 %s", status, raw, want)
		}
	})
}

// Only a waive is approved here. A migration's request has no approval yet —
// it arrives with the migration's own second approver — and naming the
// organization does not make one: its requester, alone in a named
// organization, is refused as anybody is.
func TestNamingAnOrganizationApprovesNoMigration(t *testing.T) {
	h := withOrganizationNamed(t)
	boss := h.signIn(t, "boss", entities.RoleAdmin)
	request := h.sampleMigration(t, "mf1-sole-administrator")
	request.RequestedBy, request.RequestedByID = "boss", h.accountID(t, "boss")
	request = h.mustCreateRequest(t, request)
	before := h.everyRow(t)

	want := invalid("approving a migration arrives with the migration's second approver")
	if status, _, raw := h.decide(t, boss, request.ID.String(), "approve", decisionReason); status != http.StatusBadRequest || !sameJSON(t, raw, want) {
		t.Fatalf("a sole administrator approving their own migration request: %d (%s), want 400 %s", status, raw, want)
	}
	h.requireUnchanged(t, before, "the refused approval of a migration")
}

// Withdrawal needs no setting and no second administrator: whoever asked may
// always end their own request, alone in the organization or not, and a
// withdrawal is nobody's approval — in an organization that is named as in
// one that is not.
func TestARequesterAloneInTheOrganizationMayAlwaysWithdraw(t *testing.T) {
	for name, list := range map[string]func(uuid.UUID) string{
		"not named": func(uuid.UUID) string { return "" },
		"named":     uuid.UUID.String,
	} {
		t.Run(name, func(t *testing.T) {
			h := soleOrganizations(t, list)
			logs := captureLogs(t)
			instanceID := h.oneStep(t)
			boss := h.signIn(t, "boss", entities.RoleAdmin)
			requestID := h.askToWaive(t, boss, instanceID)

			status, withdrawn, body := h.decide(t, boss, requestID, "reject", "asked for the wrong step")
			if status != http.StatusOK || withdrawn.Request.Status != "rejected" || withdrawn.Request.DecidedBy != "boss" ||
				withdrawn.Request.SelfApproved || withdrawn.Applied {
				t.Fatalf("a sole administrator withdrawing: %d (%s), want it rejected by them and self-approved by nobody", status, body)
			}
			if !h.stepIsOpen(t, instanceID) {
				t.Fatal("a withdrawal closed the step")
			}
			if said := h.entriesOf(t, instanceID, serviceimpl.EventDeviationSelfApproved); len(said) != 0 {
				t.Fatalf("a withdrawal is on the trail as a self-approval: %+v", said)
			}
			if rejected := h.entriesOf(t, instanceID, serviceimpl.EventDeviationRejected); len(rejected) != 1 || rejected[0].Data["withdrawn"] != true {
				t.Fatalf("the trail's entry of the withdrawal: %+v", rejected)
			}
			if lines := logs.said(selfApprovalLogged); len(lines) != 0 {
				t.Fatalf("a withdrawal was logged as a self-approval, made or refused: %v", lines)
			}
			// And the same waive can be asked for again.
			if again := h.askToWaive(t, boss, instanceID); again == requestID {
				t.Fatal("asking again after a withdrawal answered the withdrawn request")
			}
		})
	}
}
