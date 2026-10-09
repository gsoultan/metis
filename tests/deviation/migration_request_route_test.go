package deviation_test

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"testing"

	"github.com/google/uuid"

	pkgauth "github.com/gsoultan/metis/internal/pkg/auth"
	"github.com/gsoultan/metis/server/domains/entities"
	servicecontracts "github.com/gsoultan/metis/server/domains/services/contracts"
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

// skippingTheApproval is the migration's one decision.
func skippingTheApproval() []servicecontracts.MigrationOption {
	return []servicecontracts.MigrationOption{servicecontracts.WithNodeActions(map[string]servicecontracts.NodeAction{
		"opsApprove": {Kind: servicecontracts.NodeActionSkip, Reason: skipReason}})}
}

// askToMigrate asks, as the administrator called name, for the migration
// that skips the approval, and answers the request's id.
func (h *deviationHarness) askToMigrate(t *testing.T, name string, m migration) string {
	t.Helper()
	asking := context.WithValue(h.tenantContext(), pkgauth.UserContextKey,
		entities.User{ID: h.accountID(t, name), Username: name, Roles: []string{entities.RoleAdmin}})
	pending, err := h.svc.RequestMigrationApproval(asking, m.v1, m.v2, nil, skippingTheApproval()...)
	if err != nil {
		t.Fatalf("%s asks for the migration: %v", name, err)
	}
	return pending.RequestID.String()
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
	sentence := " No second administrator approved it: boss approved their own request (request " + requestID + ")."
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
		requestID := h.askToMigrate(t, "boss", m)
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
		requestID := h.askToMigrate(t, "boss", m)
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
		requestID := h.askToMigrate(t, "boss", m)
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
		if status, approved, raw := h.decide(t, boss, h.askToMigrate(t, "boss", m), "approve", why); status != http.StatusOK || !approved.Request.SelfApproved {
			t.Fatalf("alone: %d (%s), want the self-approval taken", status, raw)
		}
		if h.versionOf(t, first) != m.v2 {
			t.Fatal("the self-approved migration left the instance on the old version")
		}

		h.signIn(t, "deputy", entities.RoleAdmin)
		second := h.waitingAtTheApproval(t)
		requestID := h.askToMigrate(t, "boss", m)
		want := refusal("forbidden", anotherMustApprove)
		if status, _, raw := h.decide(t, boss, requestID, "approve", why); status != http.StatusForbidden || !sameJSON(t, raw, want) {
			t.Fatalf("no longer alone: %d (%s), want 403 %s", status, raw, want)
		}
		if h.versionOf(t, second) != m.v1 || h.requestStatus(t, requestID) != "pending_approval" {
			t.Fatal("the refused approval moved the second instance or its request")
		}
	})
}
