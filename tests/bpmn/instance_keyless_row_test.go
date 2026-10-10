package bpmn_test

import (
	"testing"

	"github.com/gsoultan/metis/server/domains/entities"
)

// What stops an act being made twice on one visit is the replay an apply asks
// for under the instance's lock, and the replay finds the visit's row by its
// visit key and its status. The unique index on the live key is the
// database's backstop, and it does not see every row: a server of the release
// before the live key wrote its rows without one, and during a rolling
// upgrade such a server is still writing.
//
// So an applied row whose live key is NULL — written here as that server
// leaves it — is still the record of its visit: the same request made again
// is answered with it, nothing is done a second time, and no second row is
// written beside it. Were the replay ever to read the live key instead, this
// fails.
func TestAnAppliedRowWithNoLiveKeyIsStillTheRecordOfItsVisit(t *testing.T) {
	for _, kind := range []entities.DeviationKind{entities.DeviationWaive, entities.DeviationCancel, entities.DeviationHold} {
		t.Run(string(kind), func(t *testing.T) {
			h := newEngineHarness(t, "Keyless Row Project "+string(kind))
			events := &eventLog{}
			h.dispatcher.Register(events)
			w := newWaiver(h)
			id := w.start(t, opsApproval(h.projID, "ops-keyless-"+string(kind)), nil)
			cmd := w.previewed(t, deviationCommand(kind, id, "opsApprove", nil))
			first, err := w.svc.DeviateInstance(w.ctx, cmd)
			if err != nil || !first.Applied || first.Replayed || first.Deviation == nil {
				t.Fatalf("the %s: %+v, %v; want it applied", kind, first, err)
			}

			// The row as a server of the previous release wrote it.
			keyless := h.db.Exec(`UPDATE instance_deviations SET live_visit_key = NULL WHERE id = ? AND live_visit_key IS NOT NULL`, first.Deviation.ID)
			if keyless.Error != nil || keyless.RowsAffected != 1 {
				t.Fatalf("take the live key off the row: %d row(s), %v", keyless.RowsAffected, keyless.Error)
			}
			before, raised := everyRow(t, h), events.count()

			again, err := w.svc.DeviateInstance(w.ctx, cmd)
			if err != nil || !again.Applied || !again.Replayed || again.Deviation == nil || again.Deviation.ID != first.Deviation.ID {
				t.Fatalf("the same %s sent again: %+v, %v; want a replay of the row that was written", kind, again, err)
			}
			if changed := tablesThatDiffer(before, everyRow(t, h)); len(changed) != 0 {
				t.Fatalf("the replay of a row with no live key changed %v", changed)
			}
			if now := events.count(); now != raised {
				t.Fatalf("the replay raised %d event(s)", now-raised)
			}
			if rows := w.ledger(t, id); len(rows) != 1 {
				t.Fatalf("the ledger holds %d rows for one %s, want the one", len(rows), kind)
			}
		})
	}
}
