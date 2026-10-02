package impl

import (
	"context"
	"testing"

	"github.com/gsoultan/metis/server/domains/entities"
	"github.com/gsoultan/metis/server/repositories/models"
)

func handOverEntry(auditType string, data map[string]any) entities.AuditEntry {
	return entities.AuditEntry{Type: auditType, Node: &entities.Node{Name: "Approve the refund"}, Data: data}
}

// Everything else the writer records keeps the sentence it had, whatever its
// data happens to carry: only a hand-over is told from its data.
func TestRecordEventLeavesEveryOtherEntryAsItWas(t *testing.T) {
	t.Parallel()
	data := map[string]any{"actor": "alice", "target": "bob", "previous_holder": "carol", "reason": "because"}
	for _, auditType := range []string{
		EventTaskClaimed, EventTaskCompleted, EventTaskCreated, EventTaskEscalated,
		EventNodeSkipped, EventInstanceMigrated, EventParkedWorkWithdrawn,
	} {
		var persisted models.AuditModel
		writer := &auditWriter{repo: &stubAuditRepo{onCreate: func(m models.AuditModel) error {
			persisted = m
			return nil
		}}}
		if err := writer.RecordEvent(context.Background(), handOverEntry(auditType, data)); err != nil {
			t.Fatalf("RecordEvent(%s): %v", auditType, err)
		}
		if want := narrativeFor(auditType, "Approve the refund", "alice"); persisted.Narrative != want {
			t.Errorf("%s reads %q, want what it always read: %q", auditType, persisted.Narrative, want)
		}
	}
}

func TestHandOverNarrative(t *testing.T) {
	t.Parallel()
	changed := func(fields ...string) map[string]any {
		changes := map[string]any{}
		for _, field := range fields {
			changes[field] = map[string]any{"before": "a", "after": "b"}
		}
		return changes
	}
	cases := []struct {
		name  string
		entry entities.AuditEntry
		want  string
	}{
		{"assigned to somebody when nobody held it",
			handOverEntry(EventTaskAssigned, map[string]any{"actor": "boss", "target": "bob"}),
			`boss assigned task "Approve the refund" to bob`},
		{"reassigned with a reason",
			handOverEntry(EventTaskAssigned, map[string]any{"actor": "boss", "target": "bob", "previous_holder": "alice", "reason": "alice is on leave until Monday"}),
			`boss reassigned task "Approve the refund" from alice to bob: alice is on leave until Monday`},
		{"reassigned to somebody it was not offered to",
			handOverEntry(EventTaskAssigned, map[string]any{"actor": "boss", "target": "bob", "previous_holder": "alice", "candidate_override": true, "reason": "nobody in finance is in"}),
			`boss reassigned task "Approve the refund" from alice to bob, who is not one of the people it is offered to: nobody in finance is in`},
		{"delegated by its holder",
			handOverEntry(EventTaskDelegated, map[string]any{"actor": "alice", "target": "mallory", "previous_holder": "alice", "owner": "alice"}),
			`alice delegated task "Approve the refund" to mallory`},
		{"delegated on the holder's behalf",
			handOverEntry(EventTaskDelegated, map[string]any{"actor": "boss", "target": "bob", "previous_holder": "alice", "owner": "alice", "reason": "alice asked by phone"}),
			`boss delegated task "Approve the refund" from alice to bob: alice asked by phone`},
		{"handed back by the delegate",
			handOverEntry(EventTaskResolved, map[string]any{"actor": "mallory", "target": "alice", "previous_holder": "mallory"}),
			`mallory handed task "Approve the refund" back to alice`},
		{"handed back by an administrator",
			handOverEntry(EventTaskResolved, map[string]any{"actor": "boss", "target": "alice", "previous_holder": "mallory", "reason": "mallory is away"}),
			`boss handed task "Approve the refund" back from mallory to alice: mallory is away`},
		{"released by its holder",
			handOverEntry(EventTaskUnclaimed, map[string]any{"actor": "alice", "previous_holder": "alice"}),
			`alice released task "Approve the refund" back to the queue`},
		{"released by an administrator",
			handOverEntry(EventTaskUnclaimed, map[string]any{"actor": "boss", "previous_holder": "alice", "reason": "alice left"}),
			`boss released task "Approve the refund" from alice back to the queue: alice left`},
		{"one field edited",
			handOverEntry(EventTaskEdited, map[string]any{"actor": "alice", "changes": changed("due_date")}),
			`alice changed the due date of task "Approve the refund"`},
		{"two fields edited",
			handOverEntry(EventTaskEdited, map[string]any{"actor": "alice", "changes": changed("priority", "name")}),
			`alice changed the name and the priority of task "Approve the refund"`},
		{"three fields edited, with a reason",
			handOverEntry(EventTaskEdited, map[string]any{"actor": "boss", "holder": "alice", "changes": changed("name", "priority", "due_date"), "reason": "the customer called"}),
			`boss changed the name, the priority and the due date of task "Approve the refund": the customer called`},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			got, ok := handOverNarrative(c.entry)
			if !ok || got != c.want {
				t.Fatalf("got  %q (%v)\nwant %q", got, ok, c.want)
			}
		})
	}
}

// Entries written before this — an assignment that names only its target as
// "actor", a release that names nobody — have nothing to say who did it, and
// keep the sentence they always had.
func TestAnEntryThatDoesNotSayWhoActedKeepsItsOldSentence(t *testing.T) {
	t.Parallel()
	for name, entry := range map[string]entities.AuditEntry{
		"an old assignment":   handOverEntry(EventTaskAssigned, map[string]any{"actor": "bob"}),
		"an old release":      handOverEntry(EventTaskUnclaimed, map[string]any{"actor": ""}),
		"a claim":             handOverEntry(EventTaskClaimed, map[string]any{"actor": "alice", "target": "alice"}),
		"an edit of nothing":  handOverEntry(EventTaskEdited, map[string]any{"actor": "alice"}),
		"an entry of no data": handOverEntry(EventTaskDelegated, nil),
	} {
		if told, ok := handOverNarrative(entry); ok {
			t.Errorf("%s was given the sentence %q", name, told)
		}
	}
}

func TestRecordEventTellsAHandOverFromItsData(t *testing.T) {
	t.Parallel()
	var persisted models.AuditModel
	writer := &auditWriter{repo: &stubAuditRepo{onCreate: func(m models.AuditModel) error {
		persisted = m
		return nil
	}}}
	if err := writer.RecordEvent(context.Background(), handOverEntry(EventTaskAssigned,
		map[string]any{"actor": "boss", "target": "bob", "previous_holder": "alice", "reason": "on leave"})); err != nil {
		t.Fatalf("RecordEvent: %v", err)
	}
	if want := `boss reassigned task "Approve the refund" from alice to bob: on leave`; persisted.Narrative != want {
		t.Fatalf("stored %q, want %q", persisted.Narrative, want)
	}
	if persisted.Data["reason"] != "on leave" {
		t.Fatalf("the reason was not stored with the entry: %v", persisted.Data)
	}
}
