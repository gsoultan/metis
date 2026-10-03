package deviation_test

import (
	"net/http"
	"testing"

	"github.com/google/uuid"
	"github.com/gsoultan/metis/server/domains/entities"
)

// actorOfRow is what the read route says about who made a row, and the account
// id the row itself keeps, read straight from the table.
type actorOfRow struct {
	kind          string
	actor         any
	actorIsServer any
	stored        uuid.UUID
}

func (h *deviationHarness) actorsOf(t *testing.T, token string, instanceID uuid.UUID) []actorOfRow {
	t.Helper()
	status, reply, body := h.readDeviations(t, token, instanceID.String())
	if status != http.StatusOK {
		t.Fatalf("read: %d (%s)", status, body)
	}
	out := make([]actorOfRow, 0, len(reply.Deviations))
	for _, row := range reply.Deviations {
		var stored *uuid.UUID
		if err := h.db.Raw(`SELECT actor_id FROM instance_deviations WHERE id = ?`, row["id"]).Row().Scan(&stored); err != nil {
			t.Fatalf("read the row's account id: %v", err)
		}
		found := actorOfRow{kind: row["kind"].(string), actor: row["actor"], actorIsServer: row["actor_is_server"]}
		if stored != nil {
			found.stored = *stored
		}
		out = append(out, found)
	}
	return out
}

func (h *deviationHarness) accountID(t *testing.T, username string) uuid.UUID {
	t.Helper()
	var id uuid.UUID
	if err := h.db.Raw(`SELECT id FROM users WHERE username = ?`, username).Row().Scan(&id); err != nil {
		t.Fatalf("read the account %q: %v", username, err)
	}
	return id
}

// TestAnAccountWhoseNameHasASpaceAtItsEndIsRecordedAsItselfAndNotAsTheServer.
//
// Root cause: the ledger trimmed the actor and then compared the trimmed name
// with the signed-in account's own. An account called "System " started a
// step, the row said "System" and kept no account id, and the route read "no
// account id" as the server: a person's act, told as the server's. An account
// "dita " was recorded as "dita", who may be somebody else. The name is
// recorded exactly as the account has it, with the account's id, by every
// writer: an activation, a migration's decision, a hand-over.
func TestAnAccountWhoseNameHasASpaceAtItsEndIsRecordedAsItselfAndNotAsTheServer(t *testing.T) {
	h := newDeviationHarness(t)
	reader := h.signIn(t, "member", entities.RoleUser)

	// An activation, by an operator called "System ".
	system := h.signIn(t, "System ", entities.RoleOperator)
	research := h.startResearch(t)
	status, reply := h.do(t, http.MethodPost, system, "/api/v1/processes/adhoc/activate", map[string]any{
		"instance_id": research.String(), "sub_process_node_id": "research", "task_node_id": "call-customer",
	})
	if status != http.StatusOK {
		t.Fatalf("an activation by the account %q: %d (%s)", "System ", status, reply)
	}

	// A migration's hold and a hand-over, by an administrator called "dita ".
	dita := h.signIn(t, "dita ", entities.RoleAdmin)
	h.signIn(t, "bob", entities.RoleUser)
	v1, v2, held := h.twoVersionsOfOneStep(t)
	status, reply = h.do(t, http.MethodPost, dita, "/api/v1/definitions/versions/migrate", map[string]any{
		"source_definition_id": v1.String(), "target_definition_id": v2.String(), "dry_run": false,
		"node_actions": map[string]any{"step": map[string]any{"kind": "hold", "reason": "the approver has left"}},
	})
	if status != http.StatusOK {
		t.Fatalf("a migration by the account %q: %d (%s)", "dita ", status, reply)
	}
	status, reply = h.do(t, http.MethodPost, dita, "/api/v1/tasks/"+h.openTaskOf(t, held).String()+"/assign",
		map[string]any{"user_id": "bob", "reason": "alice is on leave"})
	if status != http.StatusOK {
		t.Fatalf("a hand-over by the account %q: %d (%s)", "dita ", status, reply)
	}

	want := []struct {
		instance uuid.UUID
		kind     string
		actor    string
	}{
		{research, string(entities.DeviationAdHocActivation), "System "},
		{held, string(entities.DeviationHold), "dita "},
		{held, string(entities.DeviationReassign), "dita "},
	}
	for _, w := range want {
		var found *actorOfRow
		for _, row := range h.actorsOf(t, reader, w.instance) {
			if row.kind == w.kind {
				found = &row
			}
		}
		if found == nil {
			t.Errorf("no %s row was written by the account %q", w.kind, w.actor)
			continue
		}
		if found.actor != w.actor {
			t.Errorf("the %s of the account %q is recorded as by %q", w.kind, w.actor, found.actor)
		}
		if found.actorIsServer != false {
			t.Errorf("the %s of the account %q reads actor_is_server = %v; a signed-in account made it", w.kind, w.actor, found.actorIsServer)
		}
		if account := h.accountID(t, w.actor); found.stored != account {
			t.Errorf("the %s row of the account %q keeps the account id %s, want its own %s", w.kind, w.actor, found.stored, account)
		}
	}
}

// openTaskOf is the one open task of an instance.
func (h *deviationHarness) openTaskOf(t *testing.T, instanceID uuid.UUID) uuid.UUID {
	t.Helper()
	tasks, err := h.svc.ListTasks(h.tenantContext(), h.projID)
	if err != nil {
		t.Fatalf("list tasks: %v", err)
	}
	for _, task := range tasks {
		if task.Instance != nil && task.Instance.ID == instanceID &&
			(task.Status == entities.TaskUnclaimed || task.Status == entities.TaskClaimed) {
			return task.ID
		}
	}
	t.Fatalf("instance %s has no open task", instanceID)
	return uuid.Nil
}
