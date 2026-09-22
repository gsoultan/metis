package instancemigration

import (
	"testing"

	"github.com/gsoultan/metis/server/domains/entities"
	servicecontracts "github.com/gsoultan/metis/server/domains/services/contracts"
	"github.com/gsoultan/metis/server/domains/services/impl"
)

// Running a migration twice.
//
// A migration writes instance by instance, each in its own transaction, so a
// failure part-way leaves some moved and some not. Rather than a run table to
// resume from, the source version *is* the record: an instance that moved is no
// longer on it. Re-running the same migration therefore picks up exactly what
// is left — provided a second pass cannot undo or duplicate what the first did,
// which is what these cover.

// TestRunningTheSameMigrationTwiceChangesNothingTheSecondTime.
func TestRunningTheSameMigrationTwiceChangesNothingTheSecondTime(t *testing.T) {
	f := newFixture(t)
	v1 := f.deploy(t, "approve")
	if _, err := f.svc.StartProcess(f.ctx, f.project, "expense-approval", nil); err != nil {
		t.Fatalf("start an instance: %v", err)
	}
	v2 := f.deploy(t, "review")

	mapping := map[string]string{"approve": "review"}
	if err := f.svc.MigrateInstances(f.ctx, v1, v2, mapping); err != nil {
		t.Fatalf("first run: %v", err)
	}
	// Nothing is left on the source version, so the second run has nothing to
	// do and must say so rather than fail.
	if err := f.svc.MigrateInstances(f.ctx, v1, v2, mapping); err != nil {
		t.Fatalf("second run: %v", err)
	}

	if tasks := f.openTasks(t); len(tasks) != 1 || tasks[0].NodeID() != "review" {
		t.Fatalf("a second run disturbed the work; open tasks: %+v", tasks)
	}
	entries, err := f.svc.GetAuditLogs(f.ctx, f.onlyInstance(t).ID)
	if err != nil {
		t.Fatalf("read the trail: %v", err)
	}
	migrated := 0
	for _, entry := range entries {
		if entry.Type == impl.EventInstanceMigrated {
			migrated++
		}
	}
	if migrated != 1 {
		t.Fatalf("the trail records %d migrations of one instance that moved once", migrated)
	}
}

// TestAResumedRunPicksUpOnlyWhatIsLeft. The half that did not move is exactly
// the half still on the source version.
func TestAResumedRunPicksUpOnlyWhatIsLeft(t *testing.T) {
	f := newFixture(t)
	v1 := f.deploy(t, "approve")
	first, err := f.svc.StartProcess(f.ctx, f.project, "expense-approval", nil)
	if err != nil {
		t.Fatalf("start the first instance: %v", err)
	}
	second, err := f.svc.StartProcess(f.ctx, f.project, "expense-approval", nil)
	if err != nil {
		t.Fatalf("start the second instance: %v", err)
	}
	v2 := f.deploy(t, "review")
	mapping := map[string]string{"approve": "review"}

	// A partial run, stood in for by migrating one instance by name.
	if err := f.svc.MigrateInstances(f.ctx, v1, v2, mapping,
		servicecontracts.WithInstances(first)); err != nil {
		t.Fatalf("partial run: %v", err)
	}

	// The plan for the whole migration now covers only what is left.
	plan, err := f.svc.PlanInstanceMigration(f.ctx, v1, v2, mapping)
	if err != nil {
		t.Fatalf("plan: %v", err)
	}
	if plan.Instances != 1 {
		t.Fatalf("the resumed plan covers %d instances; one still has not moved", plan.Instances)
	}

	if err := f.svc.MigrateInstances(f.ctx, v1, v2, mapping); err != nil {
		t.Fatalf("resumed run: %v", err)
	}
	instances, err := f.svc.ListInstances(f.ctx, f.project)
	if err != nil {
		t.Fatalf("list instances: %v", err)
	}
	for _, instance := range instances {
		if instance.Definition == nil || instance.Definition.ID != v2 {
			t.Fatalf("instance %s did not end up on version 2", instance.ID)
		}
	}
	_ = second
}

// TestACancelledInstanceIsNotCancelledAgain. A cancelled instance stays on the
// version it ran, so a second pass finds it — and must leave it alone.
func TestACancelledInstanceIsNotCancelledAgain(t *testing.T) {
	f := newFixture(t)
	v1, v2 := f.parkedOnOpsApprove(t)

	opts := []servicecontracts.MigrationOption{
		servicecontracts.WithNodeActions(map[string]servicecontracts.NodeAction{
			"opsApprove": {Kind: servicecontracts.NodeActionCancel, Reason: "re-quote"},
		}),
		servicecontracts.WithActor("dita"),
	}
	if err := f.svc.MigrateInstances(f.ctx, uuidOf(t, v1), uuidOf(t, v2), nil, opts...); err != nil {
		t.Fatalf("first run: %v", err)
	}
	if err := f.svc.MigrateInstances(f.ctx, uuidOf(t, v1), uuidOf(t, v2), nil, opts...); err != nil {
		t.Fatalf("second run: %v", err)
	}

	entries, err := f.svc.GetAuditLogs(f.ctx, f.onlyInstance(t).ID)
	if err != nil {
		t.Fatalf("read the trail: %v", err)
	}
	cancelled := 0
	for _, entry := range entries {
		if entry.Type == impl.EventInstanceCancelled {
			cancelled++
		}
	}
	if cancelled != 1 {
		t.Fatalf("the trail records %d cancellations of one instance that was cancelled once", cancelled)
	}
	if f.onlyInstance(t).Status != entities.ProcessCancelled {
		t.Fatal("the instance is no longer cancelled after a second run")
	}
}

// TestAHeldInstanceDoesNotCollectAnIncidentPerRun. A held instance stays on the
// source version by design, so every later run finds it again; one instance
// somebody has to look at must not become a growing pile of identical rows.
func TestAHeldInstanceDoesNotCollectAnIncidentPerRun(t *testing.T) {
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

	incidents, err := f.svc.ListIncidents(f.ctx, f.onlyInstance(t).ID)
	if err != nil {
		t.Fatalf("list incidents: %v", err)
	}
	if len(incidents) != 1 {
		t.Fatalf("three runs left %d incidents on one held instance", len(incidents))
	}
}

// TestEveryEntryOfOneRunSharesARunID, so the trail reads back as "what did that
// migration do" rather than as unrelated events sharing a timestamp.
func TestEveryEntryOfOneRunSharesARunID(t *testing.T) {
	f := newFixture(t)
	v1, v2 := f.parkedOnOpsApprove(t)

	opts := []servicecontracts.MigrationOption{
		servicecontracts.WithNodeActions(map[string]servicecontracts.NodeAction{
			"opsApprove": {Kind: servicecontracts.NodeActionSkip, Reason: "role eliminated"},
		}),
		servicecontracts.WithActor("dita"),
	}
	if err := f.svc.MigrateInstances(f.ctx, uuidOf(t, v1), uuidOf(t, v2), nil, opts...); err != nil {
		t.Fatalf("apply: %v", err)
	}

	entries, err := f.svc.GetAuditLogs(f.ctx, f.onlyInstance(t).ID)
	if err != nil {
		t.Fatalf("read the trail: %v", err)
	}
	seen := map[string]struct{}{}
	for _, entry := range entries {
		switch entry.Type {
		case impl.EventInstanceMigrated, impl.EventNodeSkipped:
		default:
			continue
		}
		id, ok := entry.Data["run_id"].(string)
		if !ok || id == "" {
			t.Fatalf("a %q entry carries no run id: %+v", entry.Type, entry.Data)
		}
		seen[id] = struct{}{}
	}
	if len(seen) != 1 {
		t.Fatalf("one migration wrote entries under %d run ids", len(seen))
	}
}

// TestAFinishedInstanceIsNotRewrittenByAMigration.
//
// A completed instance is still listed against the version it ran, so a
// migration finds it. Moving it would repoint its definition at a graph it
// never executed — and that record is the only account of what it actually did.
func TestAFinishedInstanceIsNotRewrittenByAMigration(t *testing.T) {
	f := newFixture(t)
	v1 := f.deploy(t, "approve")
	if _, err := f.svc.StartProcess(f.ctx, f.project, "expense-approval", nil); err != nil {
		t.Fatalf("start an instance: %v", err)
	}
	// Run it to the end, so it is finished before the migration happens.
	f.completeTaskOn(t, "approve", "ada")
	if status := f.onlyInstance(t).Status; status != entities.ProcessCompleted {
		t.Fatalf("the instance is %q, not finished; the test needs it finished", status)
	}

	v2 := f.deploy(t, "review")
	if err := f.svc.MigrateInstances(f.ctx, v1, v2, map[string]string{"approve": "review"}); err != nil {
		t.Fatalf("apply: %v", err)
	}

	instance := f.onlyInstance(t)
	if instance.Definition == nil || instance.Definition.ID != v1 {
		t.Fatal("a finished instance was repointed at a version it never ran")
	}
	entries, err := f.svc.GetAuditLogs(f.ctx, instance.ID)
	if err != nil {
		t.Fatalf("read the trail: %v", err)
	}
	for _, entry := range entries {
		if entry.Type == impl.EventInstanceMigrated {
			t.Fatal("a finished instance has a migration on its trail")
		}
	}
}
