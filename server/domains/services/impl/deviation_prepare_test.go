package impl

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/gsoultan/metis/internal/pkg/apierr"
	pkgauth "github.com/gsoultan/metis/internal/pkg/auth"
	"github.com/gsoultan/metis/server/domains/entities"
)

func wellFormedDeviation() entities.Deviation {
	return entities.Deviation{
		Project:  &entities.Project{ID: uuid.Must(uuid.NewV7())},
		Instance: &entities.ProcessInstance{ID: uuid.Must(uuid.NewV7())},
		Kind:     entities.DeviationRelease,
		Scope:    entities.DeviationScopeTask,
		Origin:   entities.DeviationOriginTask,
		Status:   entities.DeviationApplied,
		Actor:    "boss",
		Reason:   "  alice left the company  ",
	}
}

// Record is the only way into the ledger, so it is where a malformed row is
// stopped: an unknown kind, a missing actor, a reason a required kind lacks.
func TestPrepareDeviation(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name    string
		change  func(*entities.Deviation)
		refused bool // refused at all
		client  bool // refused because of what a person typed (400), else a writer's mistake
	}{
		{"well formed", func(*entities.Deviation) {}, false, false},
		{"unknown kind", func(d *entities.Deviation) { d.Kind = "skip" }, true, false},
		{"unknown scope", func(d *entities.Deviation) { d.Scope = "forever" }, true, false},
		{"unknown origin", func(d *entities.Deviation) { d.Origin = "api" }, true, false},
		{"terminal status on a new row", func(d *entities.Deviation) { d.Status = entities.DeviationRejected }, true, false},
		{"no actor", func(d *entities.Deviation) { d.Actor = "  " }, true, false},
		{"no reason where one is required", func(d *entities.Deviation) { d.Reason = " " }, true, true},
		{"reason too long", func(d *entities.Deviation) { d.Reason = strings.Repeat("x", entities.MaxDeviationReasonLength+1) }, true, true},
		{"no reason on an activation", func(d *entities.Deviation) {
			d.Kind, d.Origin, d.Reason = entities.DeviationAdHocActivation, entities.DeviationOriginAdHoc, ""
		}, false, false},
		{"a reason on a control loss", func(d *entities.Deviation) {
			d.Kind, d.Origin, d.Scope, d.Reason = entities.DeviationControlWaived, entities.DeviationOriginMigration, entities.DeviationScopeInstance, "why"
		}, true, false},
		{"no project", func(d *entities.Deviation) { d.Project = nil }, true, false},
		{"no instance", func(d *entities.Deviation) { d.Instance = nil }, true, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			d := wellFormedDeviation()
			c.change(&d)
			got, err := prepareDeviation(context.Background(), d)
			if c.refused {
				if err == nil {
					t.Fatal("a malformed row was accepted")
				}
				if isClient := errors.Is(err, apierr.ErrInvalidArgument); isClient != c.client {
					t.Fatalf("got %v: invalid-argument %v, want %v (what a person typed is a 400, a writer's mistake a server error)", err, isClient, c.client)
				}
				return
			}
			if err != nil {
				t.Fatalf("refused: %v", err)
			}
			if got.ID == uuid.Nil || got.ID.Version() != 7 {
				t.Errorf("id %s, want a UUID v7 assigned", got.ID)
			}
			if got.RunID == uuid.Nil {
				t.Error("no run id assigned")
			}
			if got.Reason != strings.TrimSpace(d.Reason) {
				t.Errorf("reason %q, want it trimmed", got.Reason)
			}
		})
	}
}

// An id the caller chose is kept, so the caller can put it in the audit entry
// it writes beside the row; and the account id is the signed-in account's when
// that account is the actor, and nobody's otherwise.
func TestPrepareDeviationKeepsTheCallersIdAndTakesTheActorsAccount(t *testing.T) {
	t.Parallel()
	account := uuid.Must(uuid.NewV7())
	ctx := context.WithValue(context.Background(), pkgauth.UserContextKey, entities.User{ID: account, Username: "boss"})

	d := wellFormedDeviation()
	d.ID = uuid.Must(uuid.NewV7())
	got, err := prepareDeviation(ctx, d)
	if err != nil {
		t.Fatalf("refused: %v", err)
	}
	if got.ID != d.ID {
		t.Errorf("id %s, want the caller's %s", got.ID, d.ID)
	}
	if got.ActorID != account {
		t.Errorf("actor id %s, want the signed-in account %s", got.ActorID, account)
	}

	d.Actor = "System"
	if got, _ := prepareDeviation(ctx, d); got.ActorID != uuid.Nil {
		t.Errorf("an actor who is not the signed-in account was given its id %s", got.ActorID)
	}
}

// With no ledger there is no deviation: a wiring without one refuses the write,
// and so refuses the change it would have recorded.
func TestALedgerWithNoRepositoryRefusesEveryWrite(t *testing.T) {
	t.Parallel()
	ledger := NewDeviationLedger(nil)
	if _, err := ledger.Record(context.Background(), wellFormedDeviation()); err == nil {
		t.Fatal("a ledger with no repository accepted a write")
	}
	if _, err := ledger.ListInstanceDeviations(context.Background(), uuid.Must(uuid.NewV7())); err == nil {
		t.Fatal("a ledger with no repository answered a read as though it had nothing")
	}
}
