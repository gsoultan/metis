package instancemigration

import (
	"context"
	"errors"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/gsoultan/metis/server/domains/entities"
	servicecontracts "github.com/gsoultan/metis/server/domains/services/contracts"
	"github.com/gsoultan/metis/server/domains/services/impl"
	"github.com/gsoultan/metis/server/repositories"
	repocontracts "github.com/gsoultan/metis/server/repositories/contracts"
	"github.com/gsoultan/metis/server/repositories/models"
)

// An instance is moved to another version with the trail entry that says so,
// or it is not moved.
//
// Root cause: the instance_migrated entry was written after the rewrite's
// unit of work had committed, on the caller's context, and a write that
// failed was only logged. An instance could then stand on the new version —
// its work re-pointed, under a second administrator's approval or not — with
// nothing on its trail to say how. For an approved redirect, and for an
// approved loosening of a separation-of-duties rule, that entry is the only
// record on the instance: neither writes a ledger row.

// migratedEntryFailingOnce is the real trail, except that one write of an
// instance_migrated entry — the failOn-th — is refused.
type migratedEntryFailingOnce struct {
	repocontracts.AuditRepository
	writes *atomic.Int32
	failOn int32
}

func (a migratedEntryFailingOnce) Create(ctx context.Context, entry models.AuditModel) error {
	if entry.Type == impl.EventInstanceMigrated && a.writes.Add(1) == a.failOn {
		return errors.New("the trail lost its connection")
	}
	return a.AuditRepository.Create(ctx, entry)
}

type migratedEntryFailingOnceRepository struct {
	repositories.Repository
	writes *atomic.Int32
	failOn int32
}

func (r migratedEntryFailingOnceRepository) Audit() repocontracts.AuditRepository {
	return migratedEntryFailingOnce{AuditRepository: r.Repository.Audit(), writes: r.writes, failOn: r.failOn}
}

// The trail fails for the second of three instances of a migration one
// administrator applies. The first is moved, with its entry. The second is
// not moved: its rewrite is undone with the entry, as it is when its ledger
// cannot be written, and the run stops there naming it. The third is not
// reached. Nothing stands on the new version without an entry.
func TestAnInstanceIsNotMovedWhenItsMigrationEntryCannotBeWritten(t *testing.T) {
	writes := &atomic.Int32{}
	f := newFixtureOver(t, func(repo repositories.Repository) repositories.Repository {
		return migratedEntryFailingOnceRepository{Repository: repo, writes: writes, failOn: 2}
	})
	v1 := f.deploy(t, "approve")
	for range 3 {
		if _, err := f.svc.StartProcess(f.ctx, f.project, "expense-approval", nil); err != nil {
			t.Fatalf("start an instance: %v", err)
		}
	}
	v2 := f.deploy(t, "review")
	rename := map[string]string{"approve": "review"}

	result, err := f.svc.ApplyInstanceMigration(f.ctx, v1, v2, rename, servicecontracts.WithActor("dita"))
	if err == nil {
		t.Fatalf("a migration whose second instance's trail entry could not be written reported success: %+v", result)
	}
	if said := err.Error(); !strings.Contains(said, "1 of 3 instances had already been dealt with") || !strings.Contains(said, "the trail lost its connection") {
		t.Errorf("the failure says %q, want how far the run got and why it stopped", said)
	}
	if result.Changed != 1 || len(result.PassedOver) != 0 {
		t.Errorf("the run says it changed %d and passed over %d, want the one it moved before it stopped", result.Changed, len(result.PassedOver))
	}
	moved, left := f.stillOn(t, v2.String()), f.stillOn(t, v1.String())
	if len(moved) != 1 || len(left) != 2 {
		t.Fatalf("%d instance(s) moved and %d stayed, want one and two: nothing is moved without its entry", len(moved), len(left))
	}
	if entry := f.entryOf(t, moved[0].ID, impl.EventInstanceMigrated); !strings.Contains(entry.Narrative, "approve→review") {
		t.Errorf("the moved instance's entry reads %q", entry.Narrative)
	}
	named := 0
	for _, instance := range left {
		f.assertNoMigrationEntries(t, instance.ID)
		if strings.Contains(err.Error(), instance.ID.String()) {
			named++
		}
	}
	if named != 1 || strings.Contains(err.Error(), moved[0].ID.String()) {
		t.Errorf("the failure should name the one instance it stopped at, and only it: %v", err)
	}
	for _, task := range f.openTasks(t) {
		onNew := task.Instance != nil && task.Instance.ID == moved[0].ID
		if (onNew && task.NodeID() != "review") || (!onNew && task.NodeID() != "approve") {
			t.Errorf("a task of instance %v is open at %q: only the moved instance's work is re-pointed", task.Instance, task.NodeID())
		}
	}

	// Run again, the trail writing: the two that remain are moved, each with
	// its entry.
	if _, err := f.svc.ApplyInstanceMigration(f.ctx, v1, v2, rename, servicecontracts.WithActor("dita")); err != nil {
		t.Fatalf("the same migration run again: %v", err)
	}
	for _, instance := range f.stillOn(t, v2.String()) {
		f.entryOf(t, instance.ID, impl.EventInstanceMigrated)
	}
	if on := f.stillOn(t, v2.String()); len(on) != 3 {
		t.Fatalf("%d instance(s) are on the new version after the second run, want all three", len(on))
	}
}

// The same under an approval, where the entry is the only record of what was
// approved: a redirect past a control writes no ledger row. The run fails,
// the instance stays where it was on the version it was running, and the
// request reads interrupted, having acted on nothing.
func TestAnApprovedRedirectIsNotMadeWhenItsEntryCannotBeWritten(t *testing.T) {
	f := newFixtureOver(t, func(repo repositories.Repository) repositories.Repository {
		return trailRefusingRepository{Repository: repo, refused: []string{impl.EventInstanceMigrated}}
	})
	v1, v2 := f.startedOn(t, prepared(f.project, true, true), prepared(f.project, true, true))
	instance := f.assertWaitingAt(t, v1, "prepare")
	past := map[string]string{"prepare": "sign"}
	opts := []servicecontracts.MigrationOption{servicecontracts.WithInstances(instance.ID), servicecontracts.WithActor("dita")}

	pending, err := f.svc.RequestMigrationApproval(adminAs(f.ctx, "dita"), v1, v2, past, opts...)
	if err != nil {
		t.Fatalf("ask: %v", err)
	}
	out, err := f.svc.ApproveDeviationRequest(adminAs(f.ctx, "omar"), pending.RequestID, "")
	if err == nil || out.Applied || out.Request.Status != entities.DeviationRequestInterrupted {
		t.Fatalf("an approved redirect whose entry could not be written: %+v %v, want the run to fail and the request to read interrupted", out.Request, err)
	}
	if out.MigrationResult == nil || out.MigrationResult.Changed != 0 {
		t.Fatalf("the run says %+v, want it to have changed nothing", out.MigrationResult)
	}
	f.assertWaitingAt(t, v1, "prepare")
	f.assertNoMigrationEntries(t, instance.ID)
	if rows := f.ledger(t, instance.ID); len(rows) != 0 {
		t.Fatalf("the redirect that was not made left %d ledger row(s)", len(rows))
	}
}
