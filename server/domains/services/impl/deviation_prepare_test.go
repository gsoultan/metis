package impl

import (
	"context"
	"errors"
	"strings"
	"testing"
	"unicode/utf8"

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

// What a person is told is plain English: no kind slug such as task_edit or
// adhoc_activation, which is an internal name.
func TestTheReasonRefusalIsPlainEnglishWithNoKindSlug(t *testing.T) {
	t.Parallel()
	for _, kind := range []entities.DeviationKind{entities.DeviationReassign, entities.DeviationTaskEdit} {
		d := wellFormedDeviation()
		d.Kind, d.Reason = kind, ""
		_, err := prepareDeviation(context.Background(), d)
		if err == nil || !errors.Is(err, apierr.ErrInvalidArgument) {
			t.Fatalf("%s with no reason: got %v, want an invalid-argument refusal", kind, err)
		}
		if strings.Contains(err.Error(), string(kind)) || strings.Contains(err.Error(), "_") {
			t.Errorf("the refusal %q names the internal kind %q", err.Error(), kind)
		}
		if !strings.Contains(err.Error(), "say why") {
			t.Errorf("the refusal %q does not ask for a reason", err.Error())
		}
	}
}

// The ledger keeps 255 characters of a step's name. A definition's author may
// name a step at greater length, and a row that then failed to insert would
// fail the change it records — a hand-over, or a migration its dry run called
// fine. The name is for reading, so the row keeps as much of it as fits, cut
// between characters and never inside one.
func TestPrepareDeviationKeepsAsMuchOfALongStepNameAsFits(t *testing.T) {
	t.Parallel()
	// The 255th character is two bytes long, and so is the one after it: a cut
	// counted in bytes would split one of them.
	long := strings.Repeat("a", deviationNodeNameLength-1) + "éé" + strings.Repeat("b", 40)
	node := &entities.Node{ID: "opsApprove", Name: long}
	d := wellFormedDeviation()
	d.Node = node

	got, err := prepareDeviation(context.Background(), d)
	if err != nil {
		t.Fatalf("refused: %v", err)
	}
	want := strings.Repeat("a", deviationNodeNameLength-1) + "é"
	if got.Node == nil || got.Node.Name != want {
		t.Fatalf("the row keeps %d characters of the name, want the first %d ending on the whole é",
			utf8.RuneCountInString(got.Node.Name), deviationNodeNameLength)
	}
	if !utf8.ValidString(got.Node.Name) {
		t.Error("the name was cut inside a character")
	}
	if got.Node.ID != "opsApprove" {
		t.Errorf("the row names step %q, want its id untouched", got.Node.ID)
	}
	if node.Name != long {
		t.Error("the caller's own node was changed; the entry written beside the row tells the whole name")
	}
}

func TestPrepareDeviationKeepsAStepNameThatFitsWhole(t *testing.T) {
	t.Parallel()
	for _, name := range []string{"", "Operations approve", strings.Repeat("é", deviationNodeNameLength)} {
		node := &entities.Node{ID: "opsApprove", Name: name}
		d := wellFormedDeviation()
		d.Node = node
		got, err := prepareDeviation(context.Background(), d)
		if err != nil {
			t.Fatalf("refused: %v", err)
		}
		if got.Node != node || got.Node.Name != name {
			t.Errorf("a name of %d characters was changed to %q", utf8.RuneCountInString(name), got.Node.Name)
		}
	}
	d := wellFormedDeviation()
	if got, err := prepareDeviation(context.Background(), d); err != nil || got.Node != nil {
		t.Errorf("a row about no step: node %v, err %v", got.Node, err)
	}
}

// What identifies is never cut: a shortened step id or actor would name a
// different step or a different person. One too long for its column is a
// writer's mistake, and is left for the insert to refuse as a server error.
func TestPrepareDeviationDoesNotCutWhatIdentifies(t *testing.T) {
	t.Parallel()
	longID := strings.Repeat("n", deviationNodeNameLength+20)
	d := wellFormedDeviation()
	d.Node = &entities.Node{ID: longID, Name: "Approve"}
	d.Actor = strings.Repeat("u", 300)
	d.IterationID = strings.Repeat("i", 300)
	got, err := prepareDeviation(context.Background(), d)
	if err != nil {
		t.Fatalf("refused: %v", err)
	}
	if got.Node.ID != longID || got.Actor != d.Actor || got.IterationID != d.IterationID {
		t.Error("an identifier was shortened")
	}
}

// TestPrepareDeviationRecordsAnAccountsNameExactlyAndKeepsItsId.
//
// Root cause: the seam trimmed the actor and then compared the trimmed name
// with the signed-in account's own, untrimmed. For an account whose username
// has a space at either end the two differed, so the row kept no account id —
// and a row with no account id is read as the server's. A username is an
// identifier: it is recorded as it is, and "dita " is not "dita".
func TestPrepareDeviationRecordsAnAccountsNameExactlyAndKeepsItsId(t *testing.T) {
	t.Parallel()
	for _, name := range []string{"System ", " System", "dita ", "dita"} {
		account := uuid.Must(uuid.NewV7())
		ctx := context.WithValue(context.Background(), pkgauth.UserContextKey, entities.User{ID: account, Username: name})
		d := wellFormedDeviation()
		d.Actor = name
		got, err := prepareDeviation(ctx, d)
		if err != nil {
			t.Fatalf("an act by the account %q was refused: %v", name, err)
		}
		if got.Actor != name {
			t.Errorf("the account %q is recorded as %q", name, got.Actor)
		}
		if got.ActorID != account {
			t.Errorf("the row of the account %q keeps the account id %s, want its own %s", name, got.ActorID, account)
		}
	}
	// Still refused: an actor that is nothing but spaces names nobody.
	for _, blank := range []string{"", " ", "\t \n"} {
		d := wellFormedDeviation()
		d.Actor = blank
		if _, err := prepareDeviation(context.Background(), d); err == nil || errors.Is(err, apierr.ErrInvalidArgument) {
			t.Errorf("an actor of %q: %v, want it refused as the writer's mistake", blank, err)
		}
	}
}
