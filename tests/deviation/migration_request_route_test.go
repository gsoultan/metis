package deviation_test

import (
	"encoding/json"
	"fmt"
	"net/http"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/gsoultan/metis/server/domains/entities"
	serviceimpl "github.com/gsoultan/metis/server/domains/services/impl"
)

// A migration that skips a step waits for a second administrator as a waive
// does, and is decided over the same routes. These tests ask for one and
// decide it as the accounts of an organization, through the routes.

const (
	// skipReason is why the tests' migration skips its step.
	skipReason = "the operations manager role was eliminated"
	// routeMigrationKey is the process the tests migrate.
	routeMigrationKey = "route-migration"
)

// twoStep is start → opsApprove → sign → end, or — without its approval —
// start → sign → end: the version that dropped the step.
func twoStep(projectID uuid.UUID, withApproval bool) *entities.ProcessDefinition {
	nodes := []*entities.Node{
		{ID: "start", Type: entities.StartEvent, Outgoing: []string{"f1"}},
		{ID: "sign", Name: "Sign", Type: entities.UserTask, Assignee: "sasha", Incoming: []string{"f2"}, Outgoing: []string{"f3"}},
		{ID: "end", Type: entities.EndEvent, Incoming: []string{"f3"}},
	}
	flows := []*entities.SequenceFlow{{ID: "f3", SourceRef: "sign", TargetRef: "end"}}
	if withApproval {
		nodes = append(nodes, &entities.Node{ID: "opsApprove", Name: "Operations approve", Type: entities.UserTask, Assignee: "ollie",
			Incoming: []string{"f1"}, Outgoing: []string{"f2"}})
		flows = append(flows,
			&entities.SequenceFlow{ID: "f1", SourceRef: "start", TargetRef: "opsApprove"},
			&entities.SequenceFlow{ID: "f2", SourceRef: "opsApprove", TargetRef: "sign"})
	} else {
		nodes[1].Incoming = []string{"f1"}
		flows = append(flows, &entities.SequenceFlow{ID: "f1", SourceRef: "start", TargetRef: "sign"})
	}
	return &entities.ProcessDefinition{Project: &entities.Project{ID: projectID}, Key: routeMigrationKey, Name: "Route migration", Nodes: nodes, Flows: flows}
}

// migration is two versions of one process in the harness's project, the
// second staged so that the first stays the one a new instance starts on.
type migration struct {
	v1, v2 uuid.UUID
}

// twoVersionsToMigrate deploys the process with its approval and stages the
// version without it.
func (h *deviationHarness) twoVersionsToMigrate(t *testing.T) migration {
	t.Helper()
	v1, err := h.svc.CreateDefinition(h.tenantContext(), twoStep(h.projID, true))
	if err != nil {
		t.Fatalf("deploy v1: %v", err)
	}
	v2, err := h.svc.DeployDefinition(h.tenantContext(), twoStep(h.projID, false), false)
	if err != nil {
		t.Fatalf("stage v2: %v", err)
	}
	return migration{v1: v1, v2: v2}
}

// waitingAtTheApproval starts an instance on the first version: it waits at
// the operations approval.
func (h *deviationHarness) waitingAtTheApproval(t *testing.T) uuid.UUID {
	t.Helper()
	id, err := h.svc.StartProcess(h.tenantContext(), h.projID, routeMigrationKey, nil)
	if err != nil {
		t.Fatalf("start an instance: %v", err)
	}
	return id
}

// migrateReply is the migrate route's answer as a client reads it.
type migrateReply struct {
	Applied         *bool          `json:"applied"`
	PassedOver      *[]any         `json:"passed_over"`
	Plan            map[string]any `json:"plan"`
	PendingApproval *struct {
		RequestID   string    `json:"request_id"`
		Status      string    `json:"status"`
		RequestedBy string    `json:"requested_by"`
		ExpiresAt   time.Time `json:"expires_at"`
		Because     []string  `json:"because"`
	} `json:"pending_approval"`
}

// migrate sends the migration that skips the approval over the migrate
// route, as a dry run or an apply, and answers the status and the reply.
func (h *deviationHarness) migrate(t *testing.T, token string, m migration, dryRun bool) (int, migrateReply, string) {
	t.Helper()
	status, raw := h.do(t, http.MethodPost, token, "/api/v1/definitions/versions/migrate", map[string]any{
		"source_definition_id": m.v1.String(), "target_definition_id": m.v2.String(), "dry_run": dryRun,
		"node_actions": map[string]any{"opsApprove": map[string]any{"kind": "skip", "reason": skipReason}},
	})
	var reply migrateReply
	if status >= 200 && status < 300 {
		if err := json.Unmarshal([]byte(raw), &reply); err != nil {
			t.Fatalf("decode the migrate reply: %v (%s)", err, raw)
		}
	}
	return status, reply, raw
}

// askToMigrate applies, over the migrate route and as token's administrator,
// the migration that skips the approval, and answers the id of the request
// the route says now waits.
//
// The apply has to have been sent for approval: nothing applied, nobody
// passed over, a request waiting and named — or the test fails here. A skip
// the route applied on one administrator's call would otherwise pass every
// assertion a test goes on to make of "the approved migration". The reply's
// status is the migrate route's to pin (it becomes 202): this takes either.
func (h *deviationHarness) askToMigrate(t *testing.T, token string, m migration) string {
	t.Helper()
	status, reply, raw := h.migrate(t, token, m, false)
	if (status != http.StatusOK && status != http.StatusAccepted) || reply.PendingApproval == nil || reply.Applied == nil || *reply.Applied ||
		reply.PassedOver == nil || len(*reply.PassedOver) != 0 || reply.PendingApproval.Status != "pending_approval" {
		t.Fatalf("an apply that skips a step: %d (%s), want it sent for approval with nothing applied", status, raw)
	}
	if _, err := uuid.Parse(reply.PendingApproval.RequestID); err != nil {
		t.Fatalf("the route named the request %q: %v", reply.PendingApproval.RequestID, err)
	}
	return reply.PendingApproval.RequestID
}

// versionOf is the version an instance is running.
func (h *deviationHarness) versionOf(t *testing.T, instanceID uuid.UUID) uuid.UUID {
	t.Helper()
	var version uuid.UUID
	if err := h.db.Raw(`SELECT definition_id FROM process_instances WHERE id = ?`, instanceID).Row().Scan(&version); err != nil {
		t.Fatalf("read the version of instance %s: %v", instanceID, err)
	}
	return version
}

// requireSelfApprovedRun fails unless everything the run wrote on an instance
// says that boss asked for it, that boss approved it and nobody else did, in
// which organization, and on which request.
func (h *deviationHarness) requireSelfApprovedRun(t *testing.T, instanceID uuid.UUID, requestID string) {
	t.Helper()
	rows, err := h.svc.ListInstanceDeviations(h.tenantContext(), instanceID)
	if err != nil || len(rows) != 1 {
		t.Fatalf("the ledger: %+v, %v; want the one row of the skip", rows, err)
	}
	row := rows[0]
	if row.Status != entities.DeviationApplied || row.Actor != "boss" || row.ApprovedBy != "boss" || row.ApprovedByID != row.ActorID ||
		row.ActorID != h.accountID(t, "boss") || row.RequestID.String() != requestID || row.DecidedAt == nil ||
		row.Details["self_approved"] != true || fmt.Sprint(row.Details["other_administrators"]) != "0" ||
		row.Details["organization_id"] != h.orgID.String() {
		t.Fatalf("the ledger row: %+v, want it applied, asked for and approved by one account, marked self_approved, "+
			"and saying which organization it was and that it had no other administrator", row)
	}
	sentence := " No second administrator approved this migration: boss approved their own request (request " + requestID + ")."
	for _, eventType := range []string{serviceimpl.EventNodeSkipped, serviceimpl.EventInstanceMigrated} {
		entries := h.entriesOf(t, instanceID, eventType)
		if len(entries) != 1 || !strings.HasSuffix(entries[0].Narrative, sentence) {
			t.Fatalf("the %s entry: %+v\nwant one that ends\n  %s", eventType, entries, sentence)
		}
		data := entries[0].Data
		if data["self_approved"] != true || data["approved_by"] != "boss" || data["request_id"] != requestID ||
			fmt.Sprint(data["other_administrators"]) != "0" || data["organization_id"] != h.orgID.String() || data["actor"] != "boss" {
			t.Fatalf("the %s entry carries %v, want the request, who approved, that nobody else did and in which organization", eventType, data)
		}
	}
	// A migration's request is for a version: no instance's trail has an
	// entry of the approval by itself. The entries of what the run did say it.
	for _, eventType := range []string{serviceimpl.EventDeviationSelfApproved, serviceimpl.EventDeviationApproved, serviceimpl.EventDeviationRequested} {
		if entries := h.entriesOf(t, instanceID, eventType); len(entries) != 0 {
			t.Fatalf("the trail has a %s entry for a migration's request: %+v", eventType, entries)
		}
	}
}

// Rulings §4, of a migration: the requester's own approval follows the rule
// a waive's does, through the same check. Alone in an organization that is
// not named, they wait. Alone in one that is named, they may approve, saying
// why, and every record of the run says nobody else did. With somebody else
// to ask, they are refused, named or not.
func TestASoleAdministratorsOwnMigrationFollowsTheRuleAWaivesDoes(t *testing.T) {
	const why = "the only other administrator left last week; the director agreed in writing"

	t.Run("alone, in an organization that is not named", func(t *testing.T) {
		h := newDeviationRouteHarness(t)
		logs := captureLogs(t)
		boss := h.signIn(t, "boss", entities.RoleAdmin)
		m := h.twoVersionsToMigrate(t)
		instanceID := h.waitingAtTheApproval(t)
		requestID := h.askToMigrate(t, boss, m)
		before := h.everyRow(t)

		want := refusal("forbidden", soleWaits)
		if status, _, raw := h.decide(t, boss, requestID, "approve", why); status != http.StatusForbidden || !sameJSON(t, raw, want) {
			t.Fatalf("a sole administrator approving their own migration: %d (%s), want 403 %s", status, raw, want)
		}
		h.requireUnchanged(t, before, "the refused approval of one's own migration")
		if h.versionOf(t, instanceID) != m.v1 || h.requestStatus(t, requestID) != "pending_approval" {
			t.Fatal("the refused approval moved the instance or the request")
		}
		if lines := logs.said(refusedSelfApprovalLogged); len(lines) != 1 || lines[0]["request"] != requestID || lines[0]["actor"] != "boss" {
			t.Fatalf("the refused attempt was logged as %v, want one line naming the request and the account", lines)
		}
	})

	t.Run("alone, in an organization that is named", func(t *testing.T) {
		h := withOrganizationNamed(t)
		logs := captureLogs(t)
		boss := h.signIn(t, "boss", entities.RoleAdmin)
		m := h.twoVersionsToMigrate(t)
		instanceID := h.waitingAtTheApproval(t)
		requestID := h.askToMigrate(t, boss, m)
		before := h.everyRow(t)

		// A reason is required: with nobody else to approve it, it is the record.
		want := invalid(sayWhy)
		for _, reason := range []string{"", "   "} {
			if status, _, raw := h.decideHere(t, boss, requestID, "approve", reason); status != http.StatusBadRequest || !sameJSON(t, raw, want) {
				t.Fatalf("a self-approval with the reason %q: %d (%s), want 400 %s", reason, status, raw, want)
			}
		}
		h.requireUnchanged(t, before, "a self-approval of a migration that did not say why")
		if said := logs.said(selfApprovalLogged); len(said) != 0 {
			t.Fatalf("self-approvals that were not made were logged: %v", said)
		}

		status, approved, raw := h.decide(t, boss, requestID, "approve", "  "+why+"  ")
		if status != http.StatusOK || !approved.Applied || approved.Request.Status != "applied" || approved.Request.RequestedBy != "boss" ||
			approved.Request.DecidedBy != "boss" || approved.Request.DecisionReason != why || !approved.Request.SelfApproved {
			t.Fatalf("a sole administrator's approval of their own migration: %d (%s), want it applied, by boss, for the reason given, and self-approved", status, raw)
		}
		if h.versionOf(t, instanceID) != m.v2 {
			t.Fatal("the approved migration left the instance on the old version")
		}
		h.requireSelfApprovedRun(t, instanceID, requestID)
		// The request keeps what the exception rested on beside what the run
		// did: the report did not write over it.
		outcome := approved.Request.Outcome
		if outcome["self_approved"] != true || fmt.Sprint(outcome["other_administrators"]) != "0" || outcome["organization_id"] != h.orgID.String() ||
			fmt.Sprint(outcome["changed"]) != "1" || fmt.Sprint(outcome["passed_over"]) != "0" {
			t.Fatalf("the request's outcome %v, want what the run did and what the self-approval rested on", outcome)
		}
		lines := logs.said(selfApprovalLogged)
		if len(lines) != 1 || lines[0]["request"] != requestID || lines[0]["actor"] != "boss" || lines[0]["organization"] != h.orgID.String() ||
			lines[0]["setting"] != serviceimpl.EnvSoleAdministratorOrganizations {
			t.Fatalf("the self-approval was logged as %v, want one line naming the setting, the organization, the request and the account", lines)
		}
		if strings.Contains(fmt.Sprint(lines), why) {
			t.Fatalf("the log line of the self-approval carries the reason given: %v", lines)
		}
	})

	t.Run("named, and somebody else administers it", func(t *testing.T) {
		h := withOrganizationNamed(t)
		logs := captureLogs(t)
		boss, deputy := h.signIn(t, "boss", entities.RoleAdmin), h.signIn(t, "deputy", entities.RoleAdmin)
		m := h.twoVersionsToMigrate(t)
		instanceID := h.waitingAtTheApproval(t)
		requestID := h.askToMigrate(t, boss, m)
		before := h.everyRow(t)

		want := refusal("forbidden", anotherMustApprove)
		if status, _, raw := h.decide(t, boss, requestID, "approve", why); status != http.StatusForbidden || !sameJSON(t, raw, want) {
			t.Fatalf("the requester approving beside another administrator: %d (%s), want 403 %s", status, raw, want)
		}
		h.requireUnchanged(t, before, "the refused approval of one's own migration")
		if stale := logs.said(staleNameLogged); len(stale) != 1 || stale[0]["organization"] != h.orgID.String() || stale[0]["request"] != requestID {
			t.Fatalf("the out-of-date list was logged as %v, want one line naming the organization and the request", stale)
		}

		status, approved, raw := h.decide(t, deputy, requestID, "approve", "")
		if status != http.StatusOK || !approved.Applied || approved.Request.DecidedBy != "deputy" || approved.Request.SelfApproved {
			t.Fatalf("the other administrator approving: %d (%s), want it applied and nobody's self-approval", status, raw)
		}
		rows, err := h.svc.ListInstanceDeviations(h.tenantContext(), instanceID)
		if err != nil || len(rows) != 1 || rows[0].Actor != "boss" || rows[0].ApprovedBy != "deputy" || rows[0].ActorID != h.accountID(t, "boss") ||
			rows[0].ApprovedByID != h.accountID(t, "deputy") {
			t.Fatalf("the ledger: %+v, %v; want one row asked for by boss and approved by deputy", rows, err)
		}
		for _, key := range []string{"self_approved", "other_administrators", "organization_id"} {
			if _, said := rows[0].Details[key]; said {
				t.Fatalf("a row a second administrator approved carries %q: %v", key, rows[0].Details)
			}
			if _, said := approved.Request.Outcome[key]; said {
				t.Fatalf("a request a second administrator approved carries %q in its outcome: %v", key, approved.Request.Outcome)
			}
		}
		if said := logs.said(selfApprovalLogged); len(said) != 0 {
			t.Fatalf("a second administrator's approval was logged as a self-approval: %v", said)
		}
	})

	// Both sides of the rule in one organization: alone, the requester's own
	// approval is taken; once somebody else administers it, the same account
	// asking for the same migration is refused.
	t.Run("alone and then not", func(t *testing.T) {
		h := withOrganizationNamed(t)
		boss := h.signIn(t, "boss", entities.RoleAdmin)
		m := h.twoVersionsToMigrate(t)
		first := h.waitingAtTheApproval(t)
		if status, approved, raw := h.decide(t, boss, h.askToMigrate(t, boss, m), "approve", why); status != http.StatusOK || !approved.Request.SelfApproved {
			t.Fatalf("alone: %d (%s), want the self-approval taken", status, raw)
		}
		if h.versionOf(t, first) != m.v2 {
			t.Fatal("the self-approved migration left the instance on the old version")
		}

		h.signIn(t, "deputy", entities.RoleAdmin)
		second := h.waitingAtTheApproval(t)
		requestID := h.askToMigrate(t, boss, m)
		want := refusal("forbidden", anotherMustApprove)
		if status, _, raw := h.decide(t, boss, requestID, "approve", why); status != http.StatusForbidden || !sameJSON(t, raw, want) {
			t.Fatalf("no longer alone: %d (%s), want 403 %s", status, raw, want)
		}
		if h.versionOf(t, second) != m.v1 || h.requestStatus(t, requestID) != "pending_approval" {
			t.Fatal("the refused approval moved the second instance or its request")
		}
	})
}

// A migration that skips a step is asked for and decided over the API, by
// the organization's administrators and nobody else, and the approval's
// reply says what the run did — in names, never account ids, and with every
// list a list.
func TestAMigrationRequestIsAskedForAndDecidedOverTheAPI(t *testing.T) {
	h := newDeviationRouteHarness(t)
	boss, deputy := h.signIn(t, "boss", entities.RoleAdmin), h.signIn(t, "deputy", entities.RoleAdmin)
	member := h.signIn(t, "member", entities.RoleUser)
	outsider := h.signInElsewhere(t, "outsider-boss", entities.RoleAdmin)
	m := h.twoVersionsToMigrate(t)
	instanceID := h.waitingAtTheApproval(t)

	// A dry run says the apply would need somebody else, and asks nobody.
	status, preview, raw := h.migrate(t, boss, m, true)
	if status != http.StatusOK || preview.PendingApproval != nil || preview.Applied == nil || *preview.Applied ||
		preview.Plan["requires_second_approver"] != true {
		t.Fatalf("the dry run: %d (%s), want the plan saying it needs a second administrator and nothing asked", status, raw)
	}
	if reasons, _ := preview.Plan["second_approver_reasons"].([]any); len(reasons) != 1 || !strings.Contains(fmt.Sprint(reasons[0]), "“Operations approve” would be skipped for every listed instance") {
		t.Fatalf("why the dry run says it needs somebody else: %v", preview.Plan["second_approver_reasons"])
	}
	if n := h.requestCount(t); n != 0 {
		t.Fatalf("a dry run left %d request(s)", n)
	}

	// The apply is sent for approval; sent again, it is the same request.
	requestID := h.askToMigrate(t, boss, m)
	if again := h.askToMigrate(t, boss, m); again != requestID || h.requestCount(t) != 1 {
		t.Fatalf("the same apply sent again answered request %s among %d, want the one that waits", again, h.requestCount(t))
	}
	if h.versionOf(t, instanceID) != m.v1 {
		t.Fatal("asking moved the instance")
	}
	// Another administrator applying the same migration is pointed at it.
	want := invalid(fmt.Sprintf("The same migration is already waiting for approval (request %s, asked by boss); approve or reject that one.", requestID))
	if status, _, raw := h.migrate(t, deputy, m, false); status != http.StatusBadRequest || !sameJSON(t, raw, want) {
		t.Fatalf("the same migration applied by another administrator: %d (%s), want 400 %s", status, raw, want)
	}

	// The request, as an administrator of the organization reads it.
	status, read, raw := h.readRequest(t, deputy, requestID)
	if status != http.StatusOK || read.Request.Kind != "migration" || read.Request.Status != "pending_approval" || read.Request.RequestedBy != "boss" ||
		!slices.Equal(read.Request.Instances, []string{instanceID.String()}) || read.Request.InstancesInAll != 1 || len(read.Request.Because) != 1 {
		t.Fatalf("the request read: %d (%s)", status, raw)
	}
	requireFields(t, object(t, raw)["request"], "a waiting migration request",
		append([]string{"source_definition_id", "target_definition_id"}, requestFields...)...)

	// Who may decide it.
	before := h.everyRow(t)
	for name, attempt := range map[string]struct {
		token  string
		status int
		want   string
	}{
		"the requester":                        {boss, http.StatusForbidden, refusal("forbidden", anotherMustApprove)},
		"a member who is no administrator":     {member, http.StatusForbidden, ""},
		"another organization's administrator": {outsider, http.StatusNotFound, refusal("not found", noSuchRequest)},
		"nobody":                               {"", http.StatusUnauthorized, ""},
	} {
		status, _, raw := h.decide(t, attempt.token, requestID, "approve", decisionReason)
		if status != attempt.status || (attempt.want != "" && !sameJSON(t, raw, attempt.want)) {
			t.Fatalf("%s approving the migration: %d (%s), want %d %s", name, status, raw, attempt.status, attempt.want)
		}
		if strings.Contains(raw, skipReason) || strings.Contains(raw, "opsApprove") {
			t.Fatalf("%s was told something of the request: %s", name, raw)
		}
	}
	h.requireUnchanged(t, before, "the refused approvals of a migration")

	// The second administrator approves, and the reply says what the run did.
	status, approved, raw := h.decide(t, deputy, requestID, "approve", "  "+decisionReason+"  ")
	if status != http.StatusOK || !approved.Applied || approved.Request.Status != "applied" || approved.Request.RequestedBy != "boss" ||
		approved.Request.DecidedBy != "deputy" || approved.Request.DecisionReason != decisionReason || approved.Request.SelfApproved ||
		approved.Request.DecidedAt.IsZero() || approved.Deviation != nil {
		t.Fatalf("the second administrator's approval: %d (%s)", status, raw)
	}
	if h.versionOf(t, instanceID) != m.v2 {
		t.Fatal("the approved migration left the instance on the old version")
	}
	written := object(t, raw)
	requireFields(t, written, "the approval of a migration", "request", "applied", "plan", "passed_over", "passed_over_in_all")
	requireFields(t, written["request"], "an applied migration request",
		append([]string{"source_definition_id", "target_definition_id", "decided_by", "decided_at", "decision_reason"}, requestFields...)...)
	if passed, isList := written["passed_over"].([]any); !isList || len(passed) != 0 || !strings.Contains(raw, `"passed_over":[]`) {
		t.Fatalf("passed_over is %v, want a list with nobody in it: %s", written["passed_over"], raw)
	}
	// The plan the run was made from: the migration's own, its lists lists.
	plan, _ := written["plan"].(map[string]any)
	if plan["requires_second_approver"] != true || plan["source_key"] != routeMigrationKey || fmt.Sprint(plan["instances"]) != "1" {
		t.Fatalf("the plan in the reply: %v", written["plan"])
	}
	for _, list := range []string{"second_approver_reasons", "actions", "removed_nodes", "moves"} {
		if _, isList := plan[list].([]any); !isList {
			t.Fatalf("the plan's %s is %T, want a list: %s", list, plan[list], raw)
		}
	}
	if outcome := approved.Request.Outcome; fmt.Sprint(outcome["changed"]) != "1" || fmt.Sprint(outcome["passed_over"]) != "0" || len(outcome) != 2 {
		t.Fatalf("the request's outcome %v, want what the run did and nothing else", outcome)
	}
	if nulls := nullsIn(written, "reply"); len(nulls) != 0 {
		t.Fatalf("the approval holds null at %v: %s", nulls, raw)
	}
	for who, account := range map[string]string{"the requester": h.accountID(t, "boss").String(), "the approver": h.accountID(t, "deputy").String()} {
		if strings.Contains(raw, account) || strings.Contains(raw, "_by_id") {
			t.Errorf("the approval carries the account id of %s: %s", who, raw)
		}
	}

	// Decided, it is not decided again — and the same thing can be asked afresh.
	status, _, raw = h.decide(t, deputy, requestID, "approve", "")
	if status != http.StatusBadRequest || !strings.Contains(raw, "deputy approved this on ") || !strings.Contains(raw, ", and it was applied.") {
		t.Fatalf("approving an applied migration again: %d (%s)", status, raw)
	}
	if status, _, raw := h.decide(t, deputy, requestID, "reject", "too late"); status != http.StatusBadRequest || !strings.Contains(raw, "and it was applied.") {
		t.Fatalf("rejecting an applied migration: %d (%s)", status, raw)
	}

	// One that nobody decided in time is refused as expired, and kept so.
	next := h.waitingAtTheApproval(t)
	overdue := h.askToMigrate(t, boss, m)
	h.letTheDeadlinePass(t, overdue)
	status, _, raw = h.decide(t, deputy, overdue, "approve", "")
	if status != http.StatusBadRequest || !strings.Contains(raw, "This request expired on ") || !strings.Contains(raw, "before anybody approved it, so nothing was applied") {
		t.Fatalf("approving a migration past its deadline: %d (%s)", status, raw)
	}
	if h.requestStatus(t, overdue) != "expired" || h.versionOf(t, next) != m.v1 {
		t.Fatalf("the overdue request is %s and the instance on %s, want expired and unmoved", h.requestStatus(t, overdue), h.versionOf(t, next))
	}
	// Asked again, it is a fresh request, and a rejection ends that one.
	fresh := h.askToMigrate(t, boss, m)
	if fresh == overdue {
		t.Fatal("asking again after an expiry answered the expired request")
	}
	status, rejected, raw := h.decide(t, deputy, fresh, "reject", "not this quarter")
	if status != http.StatusOK || rejected.Request.Status != "rejected" || rejected.Request.DecidedBy != "deputy" || rejected.Applied {
		t.Fatalf("rejecting a migration: %d (%s)", status, raw)
	}
	if h.versionOf(t, next) != m.v1 {
		t.Fatal("a rejected migration moved the instance")
	}
}
