package instancemigration

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/gsoultan/metis/server/domains/entities"
	servicecontracts "github.com/gsoultan/metis/server/domains/services/contracts"
	"github.com/gsoultan/metis/server/domains/services/impl"
	"github.com/gsoultan/metis/server/repositories"
	repocontracts "github.com/gsoultan/metis/server/repositories/contracts"
)

func (f *fixture) ledger(t *testing.T, instanceID uuid.UUID) []entities.Deviation {
	t.Helper()
	rows, err := f.svc.ListInstanceDeviations(f.ctx, instanceID)
	if err != nil {
		t.Fatalf("read the ledger: %v", err)
	}
	return rows
}

func (f *fixture) entryOf(t *testing.T, instanceID uuid.UUID, eventType string) entities.AuditEntry {
	t.Helper()
	entries, err := f.svc.GetAuditLogs(f.ctx, instanceID)
	if err != nil {
		t.Fatalf("read the trail: %v", err)
	}
	for _, entry := range entries {
		if entry.Type == eventType {
			return entry
		}
	}
	t.Fatalf("the trail has no %s entry", eventType)
	return entities.AuditEntry{}
}

func skipOps(reason string) []servicecontracts.MigrationOption {
	return []servicecontracts.MigrationOption{
		servicecontracts.WithNodeActions(map[string]servicecontracts.NodeAction{
			"opsApprove": {Kind: servicecontracts.NodeActionSkip, Reason: reason},
		}),
		servicecontracts.WithActor("dita"),
	}
}

// "This approval did not happen, and here is who said so and why" is now a
// ledger row as well as a trail entry: written in the skip's transaction, with
// the migration's run id, naming the task it withdrew.
func TestASkipIsLedgeredWithItsRunAndItsEntry(t *testing.T) {
	f := newFixture(t)
	v1, v2 := f.parkedOnOpsApprove(t)
	opsTask := f.openTasks(t)[0]
	if err := f.migrateWithApproval(t, uuidOf(t, v1), uuidOf(t, v2), nil, skipOps("the operations manager role was eliminated")...); err != nil {
		t.Fatalf("apply: %v", err)
	}
	instance := f.onlyInstance(t)
	rows := f.ledger(t, instance.ID)
	if len(rows) != 1 {
		t.Fatalf("the ledger holds %d rows, want one for the skip", len(rows))
	}
	row := rows[0]
	if row.Kind != entities.DeviationWaive || row.Origin != entities.DeviationOriginMigration || row.Scope != entities.DeviationScopeTask ||
		row.Actor != "dita" || row.Reason != "the operations manager role was eliminated" ||
		row.Node == nil || row.Node.ID != "opsApprove" || row.Node.Name != "Operations approve" ||
		row.Task == nil || row.Task.ID != opsTask.ID || row.Definition == nil || row.Definition.ID.String() != v1 {
		t.Fatalf("the skip's row: %+v", row)
	}
	if row.Details["withdrawn"] != float64(1) {
		t.Errorf("details %v, want withdrawn 1", row.Details)
	}
	entry := f.entryOf(t, instance.ID, impl.EventNodeSkipped)
	if entry.Data["run_id"] != row.RunID.String() || entry.Data["deviation_id"] != row.ID.String() || row.AuditEntryID != entry.ID {
		t.Fatalf("row %+v and entry %+v do not name each other and the run", row, entry.Data)
	}
}

func TestACancelIsLedgeredAndItsEntryIsWrittenWithTheChange(t *testing.T) {
	f := newFixture(t)
	v1, v2 := f.parkedOnOpsApprove(t)
	opts := []servicecontracts.MigrationOption{
		servicecontracts.WithNodeActions(map[string]servicecontracts.NodeAction{
			"opsApprove": {Kind: servicecontracts.NodeActionCancel, Reason: "re-quote"},
		}),
		servicecontracts.WithActor("dita"),
	}
	if err := f.svc.MigrateInstances(f.ctx, uuidOf(t, v1), uuidOf(t, v2), nil, opts...); err != nil {
		t.Fatalf("apply: %v", err)
	}
	instance := f.onlyInstance(t)
	rows := f.ledger(t, instance.ID)
	if len(rows) != 1 || rows[0].Kind != entities.DeviationCancel || rows[0].Scope != entities.DeviationScopeInstance {
		t.Fatalf("the cancel's ledger: %+v", rows)
	}
	if rows[0].Before["instance"].(map[string]any)["status"] != "active" || rows[0].After["instance"].(map[string]any)["status"] != "cancelled" {
		t.Errorf("before %v after %v", rows[0].Before, rows[0].After)
	}
	if f.entryOf(t, instance.ID, impl.EventInstanceCancelled).Data["deviation_id"] != rows[0].ID.String() {
		t.Error("the cancellation entry does not name its ledger row")
	}
}

func TestACancelIsRecordedOnceAcrossReruns(t *testing.T) {
	f := newFixture(t)
	v1, v2 := f.parkedOnOpsApprove(t)
	opts := []servicecontracts.MigrationOption{
		servicecontracts.WithNodeActions(map[string]servicecontracts.NodeAction{
			"opsApprove": {Kind: servicecontracts.NodeActionCancel, Reason: "re-quote"},
		}),
		servicecontracts.WithActor("dita"),
	}
	for run := 1; run <= 2; run++ {
		if err := f.svc.MigrateInstances(f.ctx, uuidOf(t, v1), uuidOf(t, v2), nil, opts...); err != nil {
			t.Fatalf("run %d: %v", run, err)
		}
	}
	if rows := f.ledger(t, f.onlyInstance(t).ID); len(rows) != 1 {
		t.Fatalf("two runs left %d cancel rows, want one", len(rows))
	}
}

func TestAHoldIsRecordedOnceHoweverOftenTheMigrationRuns(t *testing.T) {
	f := newFixture(t)
	v1, v2 := f.parkedOnOpsApprove(t)
	opts := []servicecontracts.MigrationOption{
		servicecontracts.WithNodeActions(map[string]servicecontracts.NodeAction{
			"opsApprove": {Kind: servicecontracts.NodeActionHold, Reason: "ask the account manager"},
		}),
		servicecontracts.WithActor("dita"),
	}
	for run := 1; run <= 3; run++ {
		if err := f.svc.MigrateInstances(f.ctx, uuidOf(t, v1), uuidOf(t, v2), nil, opts...); err != nil {
			t.Fatalf("run %d: %v", run, err)
		}
	}
	instance := f.onlyInstance(t)
	rows := f.ledger(t, instance.ID)
	if len(rows) != 1 || rows[0].Kind != entities.DeviationHold {
		t.Fatalf("three runs left the ledger %+v, want one hold", rows)
	}
	incidents, err := f.svc.ListIncidents(f.ctx, instance.ID)
	if err != nil || len(incidents) != 1 {
		t.Fatalf("incidents: %d (err %v)", len(incidents), err)
	}
	if rows[0].After["incident"].(map[string]any)["id"] != incidents[0].ID.String() {
		t.Errorf("the hold's row names incident %v, want %s", rows[0].After, incidents[0].ID)
	}
}

// Dropping a control step an instance had not passed is the acknowledgement
// someone signed: a control_waived row per such instance, pointing at the
// instance_migrated entry that tells it.
func TestAnAcknowledgedControlLossIsLedgeredPerInstance(t *testing.T) {
	f := newFixture(t)
	v1, err := f.svc.CreateDefinition(f.ctx, controlled(f.project, "opsApprove", true))
	if err != nil {
		t.Fatalf("deploy v1: %v", err)
	}
	if _, err := f.svc.StartProcess(f.ctx, f.project, "controlled-approval", nil); err != nil {
		t.Fatalf("start: %v", err)
	}
	v2, err := f.svc.CreateDefinition(f.ctx, controlled(f.project, "salesApprove", false))
	if err != nil {
		t.Fatalf("deploy v2: %v", err)
	}
	if err := f.migrateWithApproval(t, v1, v2, map[string]string{"opsApprove": "salesApprove"},
		servicecontracts.WithAcknowledgedHolds("opsApprove"), servicecontracts.WithActor("dita")); err != nil {
		t.Fatalf("apply: %v", err)
	}
	instance := f.onlyInstance(t)
	rows := f.ledger(t, instance.ID)
	if len(rows) != 1 || rows[0].Kind != entities.DeviationControlWaived || rows[0].Scope != entities.DeviationScopeInstance ||
		rows[0].Node == nil || rows[0].Node.ID != "opsApprove" || rows[0].Reason != "" || rows[0].Actor != "dita" {
		t.Fatalf("the control loss's ledger: %+v", rows)
	}
	if rows[0].Details["note"] != "second signature required over $50k" {
		t.Errorf("details %v, want the compliance note", rows[0].Details)
	}
	if entry := f.entryOf(t, instance.ID, impl.EventInstanceMigrated); entry.ID != rows[0].AuditEntryID {
		t.Errorf("the row names entry %s; the migration entry is %s", rows[0].AuditEntryID, entry.ID)
	}
}

// TestOnlyTheInstanceThatHadNotPassedTheControlLosesIt.
//
// Root cause: which controls an instance lost was worked out from its completed
// steps after the migration had re-pointed them at the new graph. With the
// control step mapped onto another, an instance that had given the approval no
// longer listed it, and was told — and would now be ledgered — as having lost
// it. It is worked out once, from the locked row's own list, before the
// rewrite.
func TestOnlyTheInstanceThatHadNotPassedTheControlLosesIt(t *testing.T) {
	f := newFixture(t)
	v1, err := f.svc.CreateDefinition(f.ctx, controlledTwoStep(f.project))
	if err != nil {
		t.Fatalf("deploy v1: %v", err)
	}
	passed, err := f.svc.StartProcess(f.ctx, f.project, "controlled-two-step", nil)
	if err != nil {
		t.Fatalf("start: %v", err)
	}
	f.completeTaskOn(t, "opsApprove", "ada")
	pending, err := f.svc.StartProcess(f.ctx, f.project, "controlled-two-step", nil)
	if err != nil {
		t.Fatalf("start: %v", err)
	}
	v2, err := f.svc.CreateDefinition(f.ctx, controlledTwoStepWithout(f.project))
	if err != nil {
		t.Fatalf("deploy v2: %v", err)
	}
	if err := f.migrateWithApproval(t, v1, v2, map[string]string{"opsApprove": "salesApprove"},
		servicecontracts.WithAcknowledgedHolds("opsApprove"), servicecontracts.WithActor("dita")); err != nil {
		t.Fatalf("apply: %v", err)
	}
	if rows := f.ledger(t, passed); len(rows) != 0 {
		t.Errorf("the instance that had given the approval has %d ledger row(s): %+v", len(rows), rows)
	}
	rows := f.ledger(t, pending)
	if len(rows) != 1 || rows[0].Kind != entities.DeviationControlWaived || rows[0].Node == nil || rows[0].Node.ID != "opsApprove" {
		t.Errorf("the instance that had not given it: %+v, want one control_waived row for the approval", rows)
	}
	if told := f.entryOf(t, passed, impl.EventInstanceMigrated).Narrative; strings.Contains(told, "had not yet passed") {
		t.Errorf("the instance that gave the approval is told as having lost it: %s", told)
	}
	if told := f.entryOf(t, pending, impl.EventInstanceMigrated).Narrative; !strings.Contains(told, "had not yet passed") {
		t.Errorf("the instance that had not given the approval is not told as having lost it: %s", told)
	}
}

func TestAMappingOnlyMigrationLedgersNothing(t *testing.T) {
	f := newFixture(t)
	v1 := f.deploy(t, "approve")
	if _, err := f.svc.StartProcess(f.ctx, f.project, "expense-approval", nil); err != nil {
		t.Fatalf("start: %v", err)
	}
	v2 := f.deploy(t, "review")
	if err := f.svc.MigrateInstances(f.ctx, v1, v2, map[string]string{"approve": "review"}, servicecontracts.WithActor("dita")); err != nil {
		t.Fatalf("apply: %v", err)
	}
	if rows := f.ledger(t, f.onlyInstance(t).ID); len(rows) != 0 {
		t.Fatalf("a migration that only moved work wrote %d ledger rows", len(rows))
	}
}

type refusingDeviations struct{}

func (refusingDeviations) Create(context.Context, entities.Deviation) (entities.Deviation, error) {
	return entities.Deviation{}, errors.New("the ledger table is not there")
}
func (refusingDeviations) ListByInstance(context.Context, uuid.UUID) ([]entities.Deviation, error) {
	return nil, nil
}

func (refusingDeviations) FindLiveByVisit(context.Context, uuid.UUID, string) (entities.Deviation, bool, error) {
	return entities.Deviation{}, false, nil
}

type unledgeredRepository struct{ repositories.Repository }

func (unledgeredRepository) Deviation() repocontracts.DeviationRepository {
	return refusingDeviations{}
}

// newUnledgeredFixture is newFixture over a repository whose ledger refuses
// every write.
func newUnledgeredFixture(t *testing.T) *fixture {
	t.Helper()
	return newFixtureOver(t, func(repo repositories.Repository) repositories.Repository {
		return unledgeredRepository{repo}
	})
}

// A decision that cannot be entered in the ledger is not made: the skipped
// approval is still open, the held instance raises no incident, the cancelled
// one is still running, the trail tells none of it — and the migration says so.
func TestADecisionThatCannotBeLedgeredIsNotMade(t *testing.T) {
	for _, kind := range []servicecontracts.NodeActionKind{servicecontracts.NodeActionSkip, servicecontracts.NodeActionCancel, servicecontracts.NodeActionHold} {
		t.Run(string(kind), func(t *testing.T) {
			f := newUnledgeredFixture(t)
			v1, v2 := f.parkedOnOpsApprove(t)
			err := f.migrateDecided(t, kind, uuidOf(t, v1), uuidOf(t, v2), nil,
				servicecontracts.WithNodeActions(map[string]servicecontracts.NodeAction{"opsApprove": {Kind: kind, Reason: "policy"}}),
				servicecontracts.WithActor("dita"))
			if err == nil {
				t.Fatalf("a %s that could not be ledgered reported success", kind)
			}
			instance := f.onlyInstance(t)
			if instance.Status != entities.ProcessActive || instance.Definition == nil || instance.Definition.ID.String() != v1 {
				t.Fatalf("the instance is %s on %v; nothing should have changed", instance.Status, instance.Definition)
			}
			if open := f.openTasks(t); len(open) != 1 || open[0].NodeID() != "opsApprove" {
				t.Fatalf("the operations approval is not open any more: %+v", open)
			}
			if incidents, _ := f.svc.ListIncidents(f.ctx, instance.ID); len(incidents) != 0 {
				t.Fatalf("a hold that was not made raised %d incident(s)", len(incidents))
			}
			// And the trail does not tell a decision that was not made.
			f.assertNoMigrationEntries(t, instance.ID)
		})
	}
}
