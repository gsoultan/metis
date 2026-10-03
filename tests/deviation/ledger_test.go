package deviation_test

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"
	"github.com/gsoultan/metis/internal/pkg/apierr"
	pkgauth "github.com/gsoultan/metis/internal/pkg/auth"
	"github.com/gsoultan/metis/server/domains/entities"
	serviceimpl "github.com/gsoultan/metis/server/domains/services/impl"
)

// The ledger names the account as well as the username: an administrator can
// rename accounts in their organization, so a name alone cannot tell later
// whether two rows were the same person.
func TestTheLedgerKeepsTheAccountOfTheActorWhoIsSignedIn(t *testing.T) {
	h := newDeviationHarness(t)
	instanceID := h.startOneStep(t, entities.Node{Name: "Approve", Type: entities.UserTask, Assignee: "alice"})
	ledger := serviceimpl.NewDeviationLedger(h.repo)
	account := uuid.Must(uuid.NewV7())
	ctx := context.WithValue(h.tenantContext(), pkgauth.UserContextKey,
		entities.User{ID: account, Username: "boss", Roles: []string{entities.RoleAdmin}})

	d := h.sample(instanceID)
	d.ID = uuid.Nil
	var recorded entities.Deviation
	if err := h.repo.UnitOfWork().Do(ctx, func(tx context.Context) error {
		var err error
		recorded, err = ledger.Record(tx, d)
		return err
	}); err != nil {
		t.Fatalf("record: %v", err)
	}
	rows, err := ledger.ListInstanceDeviations(h.tenantContext(), instanceID)
	if err != nil || len(rows) != 1 {
		t.Fatalf("read %d rows (err %v)", len(rows), err)
	}
	if rows[0].ID != recorded.ID || rows[0].ActorID != account {
		t.Fatalf("the row is %s for account %s, want %s for %s", rows[0].ID, rows[0].ActorID, recorded.ID, account)
	}
}

// A malformed row refused by Record writes nothing, and its error is a plain
// refusal rather than a database error.
func TestTheLedgerRefusesAReasonlessOverride(t *testing.T) {
	h := newDeviationHarness(t)
	instanceID := h.startOneStep(t, entities.Node{Name: "Approve", Type: entities.UserTask, Assignee: "alice"})
	ledger := serviceimpl.NewDeviationLedger(h.repo)
	d := h.sample(instanceID)
	d.Reason = ""
	err := h.repo.UnitOfWork().Do(h.tenantContext(), func(tx context.Context) error {
		_, err := ledger.Record(tx, d)
		return err
	})
	if !errors.Is(err, apierr.ErrInvalidArgument) {
		t.Fatalf("a reassignment with no reason: got %v, want it refused as invalid", err)
	}
	if rows, _ := ledger.ListInstanceDeviations(h.tenantContext(), instanceID); len(rows) != 0 {
		t.Fatalf("a refused row was written: %d", len(rows))
	}
}
