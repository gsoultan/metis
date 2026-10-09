package deviation_test

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/gsoultan/metis/server/domains/entities"
	serviceimpl "github.com/gsoultan/metis/server/domains/services/impl"
	"github.com/gsoultan/metis/server/interceptors/tenant"
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

// Off by default, and off unless the organization is named. Unset, empty,
// naming another organization, written as a switch it is not, or set under
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
			// The organization is not named, so nothing says its name is stale.
			if stale := logs.said(staleNameLogged); len(stale) != 0 {
				t.Fatalf("an organization that is not named was logged as named and out of date: %v", stale)
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

// Both sides at once, with one person: an account that is the only
// administrator of two organizations, one of them named. The same account,
// with the same reason, is let through in the named one and told to wait in
// the other.
func TestOneAdministratorOfTwoOrganizationsApprovesTheirOwnRequestOnlyInTheNamedOne(t *testing.T) {
	base := newDeviationRouteHarness(t)
	second := base.inAnotherOrganization(t, "Not Named")
	t.Setenv(serviceimpl.EnvSoleAdministratorOrganizations, base.orgID.String())
	here := base.restarted(t)
	there := here.at(second)

	account := entities.User{ID: uuid.Must(uuid.NewV7()), Username: "boss", Roles: []string{entities.RoleAdmin},
		Organizations: []*entities.Organization{{ID: here.orgID}, {ID: there.orgID}}}
	if err := here.svc.CreateUser(entities.WithSystemContext(context.Background()), account, harnessPassword); err != nil {
		t.Fatalf("create the administrator of both: %v", err)
	}
	boss := here.login(t, "boss")
	// ask has boss ask for a waive in one of the two organizations, chosen
	// by the header an account of several organizations chooses one with.
	ask := func(in *deviationHarness) (uuid.UUID, string) {
		t.Helper()
		instanceID := in.oneStep(t)
		organization := []string{tenant.OrganizationHeader, in.orgID.String()}
		status, raw := in.send(t, boss, deviationsPath(instanceID), `{"kind":"waive","node_id":"step","reason":"`+routeReason+`"}`, organization...)
		var planned deviateReply
		if err := json.Unmarshal([]byte(raw), &planned); status != http.StatusOK || err != nil || planned.Plan.VisitKey == "" {
			t.Fatalf("the preview: %d (%s)", status, raw)
		}
		status, raw = in.send(t, boss, deviationsPath(instanceID),
			`{"kind":"waive","node_id":"step","reason":"`+routeReason+`","visit_key":"`+planned.Plan.VisitKey+`","dry_run":false}`, organization...)
		var asked deviateReply
		if err := json.Unmarshal([]byte(raw), &asked); status != http.StatusAccepted || err != nil || asked.PendingApproval == nil {
			t.Fatalf("the apply: %d (%s), want 202 and the request it waits on", status, raw)
		}
		return instanceID, asked.PendingApproval.RequestID
	}
	_, named := ask(here)
	waitingAt, unnamed := ask(there)

	want := refusal("forbidden", soleWaits)
	if status, _, raw := there.decideHere(t, boss, unnamed, "approve", decisionReason); status != http.StatusForbidden || !sameJSON(t, raw, want) {
		t.Fatalf("in the organization that is not named: %d (%s), want 403 %s", status, raw, want)
	}
	if status, approved, raw := here.decideHere(t, boss, named, "approve", decisionReason); status != http.StatusOK || !approved.Request.SelfApproved {
		t.Fatalf("in the named organization: %d (%s), want it applied and self-approved", status, raw)
	}
	// Approved in one, the other is as it was.
	if status, _, raw := there.decideHere(t, boss, unnamed, "approve", decisionReason); status != http.StatusForbidden || !sameJSON(t, raw, want) {
		t.Fatalf("in the organization that is not named, after approving in the named one: %d (%s), want 403 %s", status, raw, want)
	}
	if there.requestStatus(t, unnamed) != "pending_approval" || !there.stepIsOpen(t, waitingAt) {
		t.Fatal("the request in the organization that is not named did not stay waiting")
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
	// The list names an organization that has two administrators: it is out
	// of date, and each refusal that found it so says it, to whoever reads
	// the log — naming the setting, the organization and the request.
	const wantStale = "This setting names this organization as having one administrator, and it has another: an administrator's " +
		"approval of their own request was refused. Take the organization off the list."
	stale := logs.said(staleNameLogged)
	if len(stale) != 2 || stale[0]["level"] != "warn" || stale[0]["message"] != wantStale ||
		stale[0]["setting"] != serviceimpl.EnvSoleAdministratorOrganizations || stale[0]["organization"] != h.orgID.String() ||
		stale[0]["request"] != requestID {
		t.Fatalf("the log holds %v\nwant a warning for each refusal, naming the setting, organization %s and request %s, that reads\n  %s",
			stale, h.orgID, requestID, wantStale)
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
		details["self_approved"] != true || fmt.Sprint(details["other_administrators"]) != "0" || details["organization_id"] != h.orgID.String() {
		t.Fatalf("the row of a self-approval: %v, want it applied, asked for and approved by boss, marked self_approved, "+
			"and saying which organization it was and that no other administrator of it was found", approved.Deviation)
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
		row.ActorID != h.accountID(t, "boss") || row.Details["self_approved"] != true || fmt.Sprint(row.Details["other_administrators"]) != "0" ||
		row.Details["organization_id"] != h.orgID.String() {
		t.Fatalf("the ledger row: %+v, want it applied, asked for and approved by one account, marked self_approved, "+
			"and saying which organization it was and how many other administrators it had: none", row)
	}

	// The trail: an entry that says so in so many words, with the reason, in
	// place of the entry a second administrator's approval leaves; and the
	// waived step's own entry, marked.
	said := h.entriesOf(t, instanceID, serviceimpl.EventDeviationSelfApproved)
	wantSentence := "No second administrator approved this. boss approved their own request to waive “Approve”, " +
		"which is allowed in this organization only while nobody else administers it. Reason: " + why
	if len(said) != 1 || said[0].Narrative != wantSentence {
		t.Fatalf("the trail's entry of the self-approval: %+v\nwant one that reads\n  %s", said, wantSentence)
	}
	if said[0].Data["self_approved"] != true || said[0].Data["approved_by"] != "boss" || said[0].Data["requested_by"] != "boss" ||
		said[0].Data["request_id"] != requestID || said[0].Data["deviation_id"] != row.ID.String() ||
		fmt.Sprint(said[0].Data["other_administrators"]) != "0" || said[0].Data["organization_id"] != h.orgID.String() {
		t.Fatalf("the self-approval's entry carries %v, want it to say as well which organization it was and that no other "+
			"administrator of it was found", said[0].Data)
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

	// The server's log: one line of its own, naming the setting, the
	// organization, the request and the account — by id as well as by name —
	// and nothing that was asked for or said. It is not the line a refused
	// attempt leaves, and nothing calls the list out of date.
	lines := logs.said(selfApprovalLogged)
	if len(lines) != 1 || lines[0]["level"] != "warn" || lines[0]["setting"] != serviceimpl.EnvSoleAdministratorOrganizations ||
		lines[0]["organization"] != h.orgID.String() || lines[0]["request"] != requestID || lines[0]["actor"] != "boss" ||
		lines[0]["actor_id"] != h.accountID(t, "boss").String() {
		t.Fatalf("the log holds %v, want one warning naming the setting, organization %s, request %s and boss by name and account id",
			lines, h.orgID, requestID)
	}
	if stale := logs.said(staleNameLogged); len(stale) != 0 {
		t.Fatalf("an organization with one administrator was logged as named and out of date: %v", stale)
	}
	if line, _ := json.Marshal(lines[0]); strings.Contains(string(line), why) || strings.Contains(string(line), routeReason) {
		t.Fatalf("the log line carries what was asked for or why: %s", line)
	}
	if refused := logs.said(refusedSelfApprovalLogged); len(refused) != 0 {
		t.Fatalf("an admitted self-approval was logged as a refused one: %v", refused)
	}

	// Decided, it is decided: nobody decides it again, its requester
	// included — and whoever tries is told whose approval it was.
	closed := h.everyRow(t)
	want = invalid(fmt.Sprintf("boss approved this — their own request, with no second administrator — on %s, and it was applied.",
		approved.Request.DecidedAt.UTC().Format(decidedOn)))
	for _, verb := range []string{"approve", "reject"} {
		if status, _, raw := h.decide(t, boss, requestID, verb, why); status != http.StatusBadRequest || !sameJSON(t, raw, want) {
			t.Fatalf("boss's %s of a request already approved: %d (%s), want 400 %s", verb, status, raw, want)
		}
	}
	h.requireUnchanged(t, closed, "deciding a self-approved request again")
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
