package impl

import (
	"testing"

	"github.com/google/uuid"
	"github.com/gsoultan/metis/server/domains/entities"
)

// The trail's sentence for an activation names who started which step inside
// which sub-process, by name where there is one, and the reason when one was
// given. The entry's data carries the reason only then.
func TestAnActivationsEntryTellsWhoStartedWhatAndWhy(t *testing.T) {
	t.Parallel()
	research := &entities.Node{ID: "research", Name: "Research the claim"}
	cases := []struct {
		name             string
		sub, step        *entities.Node
		actor, reason    string
		narrative        string
		wantReasonInData bool
	}{
		{"named steps, no reason", research, &entities.Node{ID: "call", Name: "Call the customer"}, "olga", "",
			"olga started “Call the customer” inside “Research the claim”", false},
		{"with a reason", research, &entities.Node{ID: "call", Name: "Call the customer"}, "olga", "the customer asked",
			"olga started “Call the customer” inside “Research the claim”: the customer asked", true},
		{"steps nobody named", &entities.Node{ID: "research"}, &entities.Node{ID: "call"}, "System", "",
			"System started “call” inside “research”", false},
	}
	for _, c := range cases {
		row := entities.Deviation{
			ID: uuid.Must(uuid.NewV7()), RunID: uuid.Must(uuid.NewV7()),
			Instance: &entities.ProcessInstance{ID: uuid.Must(uuid.NewV7())},
			Actor:    c.actor, Reason: c.reason,
		}
		entry := activationEntry(row, c.sub, c.step)
		if entry.Type != EventStepActivated || entry.Narrative != c.narrative || entry.Node != c.step || entry.Instance != row.Instance {
			t.Errorf("%s: the entry is %+v, want %q", c.name, entry, c.narrative)
		}
		if entry.Data[auditActor] != c.actor || entry.Data[auditSubProcess] != c.sub.ID ||
			entry.Data["run_id"] != row.RunID.String() || entry.Data[auditDeviationID] != row.ID.String() {
			t.Errorf("%s: the entry's data is %+v", c.name, entry.Data)
		}
		if _, has := entry.Data[auditReason]; has != c.wantReasonInData {
			t.Errorf("%s: the entry carries a reason: %v, want %v", c.name, has, c.wantReasonInData)
		}
	}
}
