package instancemigration

import (
	"context"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/gsoultan/metis/server/domains/entities"
	servicecontracts "github.com/gsoultan/metis/server/domains/services/contracts"
	"github.com/gsoultan/metis/server/domains/services/impl"
	"github.com/gsoultan/metis/server/endpoints/definition"
	"github.com/gsoultan/metis/server/repositories"
	repocontracts "github.com/gsoultan/metis/server/repositories/contracts"
	"github.com/gsoultan/metis/server/repositories/models"
)

// An instance that arrives somewhere between the listing and its lock.
//
// moved_on_test.go is the instance that LEFT the step a decision names. This
// is the other direction: the instance that ARRIVED, after the migration
// listed it, on a step the new version does not have, or on one the migration
// decides. The planner's "must land somewhere" check had run on the listing,
// and the rewrite never asked again of the row its lock returned: the instance
// was re-pointed at the new version holding a token and an open task on a step
// that version lacks. Its holder could still complete the task; the token came
// off, nothing followed it, and the instance stayed active for ever with no
// token and no task, and nothing in the product left to move it with.
//
// The window is opened on purpose, as in moved_on_test.go: the completion that
// moves the instance runs the moment the apply has listed it, or the moment
// before the rewrite takes its lock. No goroutines, no timing.

// lockHooked runs something once, just before the next lock of an instance is
// taken: the last moment at which the instance can still move under a
// migration that has already looked at it.
type lockHooked struct {
	repocontracts.ProcessRepository
	armed bool
	fired bool
	fire  func()
}

func (p *lockHooked) GetForUpdate(ctx context.Context, id uuid.UUID) (models.ProcessInstanceModel, error) {
	if p.armed {
		// Disarmed first: the completion it runs takes the same lock.
		p.armed = false
		p.fired = true
		p.fire()
	}
	return p.ProcessRepository.GetForUpdate(ctx, id)
}

// newLockRacedFixture is newRacedFixture whose instance locks can be hooked
// as well as its listings.
func newLockRacedFixture(t *testing.T) (*fixture, *hookedProcess, *lockHooked) {
	t.Helper()
	listing, locks := &hookedProcess{}, &lockHooked{}
	f := newFixtureOver(t, func(repo repositories.Repository) repositories.Repository {
		locks.ProcessRepository = repo.Process()
		listing.ProcessRepository = locks
		return &hookedRepository{Repository: repo, process: listing}
	})
	return f, listing, locks
}

// beforeTheRewriteLocks runs fire when the next apply, having listed its
// instances and read them again to decide their work, is about to lock the
// first one. Only for a run that takes no lock before the rewrite's: one whose
// instance is on no step the migration decides.
func beforeTheRewriteLocks(listing *hookedProcess, locks *lockHooked, fire func()) {
	locks.fire = fire
	listing.atApplysListing(func() { locks.armed = true })
}

// reached fails a test whose hook never ran: it would be asserting about a
// migration nothing raced.
func reached(t *testing.T, listing *hookedProcess) {
	t.Helper()
	if listing.calls < listing.on {
		t.Fatalf("the hook never reached apply's listing (%d calls); the test is not exercising the window", listing.calls)
	}
}

// assertNothingIsStranded is the invariant of this file: a migration never
// leaves an instance running with nothing that could move it on. Every active
// instance has a token and an open task, each on a step the version it runs
// has, and every open task has a token under it.
func (f *fixture) assertNothingIsStranded(t *testing.T) {
	t.Helper()
	instances, err := f.svc.ListInstances(f.ctx, f.project)
	if err != nil {
		t.Fatalf("list instances: %v", err)
	}
	f.assertEveryTokenHasAStep(t)
	open := f.openTasks(t)
	for _, instance := range instances {
		if instance.Status != entities.ProcessActive {
			continue
		}
		version, err := f.svc.GetDefinition(f.ctx, instance.Definition.ID)
		if err != nil {
			t.Fatalf("read the version instance %s runs: %v", instance.ID, err)
		}
		if len(instance.Tokens) == 0 {
			t.Errorf("STRANDED: instance %s is active with no token", instance.ID)
		}
		on := map[string]bool{}
		for _, token := range instance.Tokens {
			if token.Node != nil {
				on[token.Node.ID] = true
			}
		}
		tasks := 0
		for _, task := range open {
			if task.Instance == nil || task.Instance.ID != instance.ID {
				continue
			}
			tasks++
			if version.FindNode(task.NodeID()) == nil {
				t.Errorf("STRANDED: instance %s has an open task on %q, a step version %d does not have",
					instance.ID, task.NodeID(), version.Version)
			}
			if !on[task.NodeID()] {
				t.Errorf("STRANDED: instance %s has an open task on %q and no token there", instance.ID, task.NodeID())
			}
		}
		if tasks == 0 {
			t.Errorf("STRANDED: instance %s is active with no open task", instance.ID)
		}
	}
}

// runsToItsEnd has each holder complete what is open, step after step, and
// fails unless the instance then finishes: the proof that no step was reached
// that nothing follows.
func (f *fixture) runsToItsEnd(t *testing.T) {
	t.Helper()
	for range 10 {
		open := f.openTasks(t)
		if len(open) == 0 {
			break
		}
		if err := f.svc.CompleteTask(f.ctx, open[0].ID, username(open[0].Assignee), nil); err != nil {
			t.Fatalf("complete %q: %v", open[0].NodeID(), err)
		}
		f.assertNothingIsStranded(t)
	}
	if instance := f.onlyInstance(t); instance.Status != entities.ProcessCompleted {
		t.Fatalf("with nothing left open the instance is %s, holding %v; it can never finish", instance.Status, tokenNodes(instance))
	}
}

// assertWaitingAt fails unless the only instance is running the version given
// with one token and one open task, both on the step given.
func (f *fixture) assertWaitingAt(t *testing.T, version uuid.UUID, nodeID string) entities.ProcessInstance {
	t.Helper()
	instance := f.onlyInstance(t)
	if instance.Status != entities.ProcessActive {
		t.Errorf("the instance is %s; it should be running", instance.Status)
	}
	if instance.Definition == nil || instance.Definition.ID != version {
		t.Errorf("the instance is not on the version expected")
	}
	if len(instance.Tokens) != 1 || instance.Tokens[0].Node == nil || instance.Tokens[0].Node.ID != nodeID {
		t.Errorf("the instance holds %v, want one token on %s", tokenNodes(instance), nodeID)
	}
	if open := f.openTasks(t); len(open) != 1 || open[0].NodeID() != nodeID {
		t.Errorf("%d task(s) are open (%+v), want the one on %s", len(open), open, nodeID)
	}
	return instance
}

// assertPassedOver fails unless the run left exactly the one instance alone,
// with a reason that names the step as people know it and no id at all.
func assertPassedOver(t *testing.T, result entities.MigrationResult, instance entities.ProcessInstance, stepName, stepID string) {
	t.Helper()
	if len(result.PassedOver) != 1 || result.PassedOver[0].Instance == nil || result.PassedOver[0].Instance.ID != instance.ID {
		t.Fatalf("the run passed over %+v, want the one instance (%s)", result.PassedOver, instance.ID)
	}
	assertNamesTheStep(t, result.PassedOver[0].Reason, stepName, stepID)
}

func assertNamesTheStep(t *testing.T, reason, stepName, stepID string) {
	t.Helper()
	t.Logf("passed over, and told: %s", reason)
	if !strings.Contains(reason, stepName) || strings.Contains(reason, stepID) || anyID.MatchString(reason) {
		t.Errorf("the reason should name %q as people know it, and carry no id: %q", stepName, reason)
	}
}

// TestAnInstanceThatReachesAStepTheNewVersionLacksIsNotMoved.
//
// Root cause: whether an instance's work can land on the new version was
// answered from the migration's listing and never asked again of the row the
// rewrite's lock returned.
//
// A migration that only moves work, no decisions in it. The quotation waits at
// the supervisor's review, which both versions have, when it is planned and
// listed; the supervisor completes the review before the rewrite locks it, and
// it now waits at the operations approval, which the new version does not have.
// Asked through the endpoint, so that what a client is told is what is read.
func TestAnInstanceThatReachesAStepTheNewVersionLacksIsNotMoved(t *testing.T) {
	f, listing := newRacedFixture(t)
	v1, v2 := f.waitingAtSupervisorReview(t)
	listing.atTheEndpointsApplyListing(func() { f.completeTaskOn(t, "supervisorReview", "sam") })

	dryRun := false
	reply, body := f.migrateOverTheEndpoint(t, definition.MigrateInstancesRequest{
		SourceDefinitionID: v1.String(), TargetDefinitionID: v2.String(), DryRun: &dryRun,
	})
	reached(t, listing)

	// Not rewritten: on the version it was running, with its token and its
	// task where its holder left them, and nothing said to have been done.
	instance := f.assertWaitingAt(t, v1, "opsApprove")
	f.assertNothingIsStranded(t)
	if rows := f.ledger(t, instance.ID); len(rows) != 0 {
		t.Errorf("an instance that was left alone has %d ledger row(s): %+v", len(rows), rows)
	}
	f.assertNoMigrationEntries(t, instance.ID)

	// And the reply says so.
	if *reply.Applied {
		t.Errorf("applied is true although nothing was written to any instance: %s", body)
	}
	if len(*reply.PassedOver) != 1 || (*reply.PassedOver)[0].InstanceID != instance.ID.String() {
		t.Fatalf("passed_over is %+v, want the one instance (%s)", *reply.PassedOver, instance.ID)
	}
	assertNamesTheStep(t, (*reply.PassedOver)[0].Reason, "Operations approve", "opsApprove")

	// Planning again is how it is dealt with: the plan now finds it where it
	// stands and says what the migration needs.
	plan, err := f.svc.PlanInstanceMigration(f.ctx, v1, v2, nil)
	if err != nil {
		t.Fatalf("plan again: %v", err)
	}
	if plan.Applicable() || !strings.Contains(strings.Join(plan.Refusals, "; "), "opsApprove") {
		t.Errorf("planned again, the migration should be refused for the work on the operations approval: %v", plan.Refusals)
	}

	// Its holder completes the step, and the instance goes on as the version
	// it runs says: to the sales approval, and from there to its end.
	f.completeTaskOn(t, "opsApprove", "ollie")
	f.assertWaitingAt(t, v1, "salesApprove")
	f.runsToItsEnd(t)
}

// arrivesAtOpsApproveAfterTheListing applies a migration that decides the
// operations approval to a quotation that was at the step before it when the
// apply listed it, and reached the approval before the apply got to it.
func arrivesAtOpsApproveAfterTheListing(t *testing.T, kind servicecontracts.NodeActionKind) (f *fixture, v1, v2 uuid.UUID, result entities.MigrationResult) {
	t.Helper()
	f, listing := newRacedFixture(t)
	v1, v2 = f.waitingAtSupervisorReview(t)
	listing.atApplysListing(func() { f.completeTaskOn(t, "supervisorReview", "sam") })
	result, err := f.svc.ApplyInstanceMigration(f.ctx, v1, v2, nil, decideOps(kind, "the role was eliminated")...)
	if err != nil {
		t.Fatalf("apply: %v", err)
	}
	reached(t, listing)
	return f, v1, v2, result
}

// assertDecided fails unless the instance's ledger holds the one row of the
// decision about the operations approval and its trail the entry that tells
// it, each naming the other.
func (f *fixture) assertDecided(t *testing.T, instance entities.ProcessInstance, kind entities.DeviationKind, entryType string) {
	t.Helper()
	rows := f.ledger(t, instance.ID)
	if len(rows) != 1 || rows[0].Kind != kind || rows[0].Actor != "dita" || rows[0].Reason != "the role was eliminated" ||
		rows[0].Node == nil || rows[0].Node.ID != "opsApprove" || rows[0].Node.Name != "Operations approve" {
		t.Fatalf("the ledger should hold one %s of the operations approval, by dita, with her reason: %+v", kind, rows)
	}
	entry := f.entryOf(t, instance.ID, entryType)
	if entry.ID != rows[0].AuditEntryID || entry.Data["deviation_id"] != rows[0].ID.String() {
		t.Errorf("the %s entry and the ledger row do not name each other", entryType)
	}
}

// TestAnInstanceThatReachesASkippedStepAfterTheListingIsSkippedAndThenMoved.
//
// Root cause: which decision an instance gets was chosen from the listing's
// copy of it. One that reached the step afterwards was not skipped, and was
// then moved with its token on the step the skip was there to clear.
func TestAnInstanceThatReachesASkippedStepAfterTheListingIsSkippedAndThenMoved(t *testing.T) {
	f, _, v2, result := arrivesAtOpsApproveAfterTheListing(t, servicecontracts.NodeActionSkip)

	instance := f.assertWaitingAt(t, v2, "salesApprove")
	f.assertNothingIsStranded(t)
	f.assertDecided(t, instance, entities.DeviationWaive, impl.EventNodeSkipped)
	if withdrawn := f.ledger(t, instance.ID)[0].Details["withdrawn"]; withdrawn != float64(1) {
		t.Errorf("the skip's row says %v task(s) were withdrawn, want the one operations approval", withdrawn)
	}
	f.entryOf(t, instance.ID, impl.EventInstanceMigrated)
	if result.Changed != 1 || len(result.PassedOver) != 0 {
		t.Errorf("the run says it acted on %d and passed over %d, want one and none", result.Changed, len(result.PassedOver))
	}
	f.runsToItsEnd(t)
}

// A cancel ends the instances waiting at the step. One that got there after
// the listing is one of them.
func TestAnInstanceThatReachesAStepBeingCancelledAtAfterTheListingIsCancelled(t *testing.T) {
	f, v1, _, result := arrivesAtOpsApproveAfterTheListing(t, servicecontracts.NodeActionCancel)

	instance := f.onlyInstance(t)
	if instance.Status != entities.ProcessCancelled || len(instance.Tokens) != 0 || instance.Definition == nil || instance.Definition.ID != v1 {
		t.Fatalf("the instance is %s on %v holding %v; it should have been ended where it stood, on the version it ran",
			instance.Status, instance.Definition, tokenNodes(instance))
	}
	if open := f.openTasks(t); len(open) != 0 {
		t.Errorf("a cancelled instance has %d open task(s): %+v", len(open), open)
	}
	f.assertDecided(t, instance, entities.DeviationCancel, impl.EventInstanceCancelled)
	if result.Changed != 1 || len(result.PassedOver) != 0 {
		t.Errorf("the run says it acted on %d and passed over %d, want one and none", result.Changed, len(result.PassedOver))
	}
}

// A hold keeps the instances waiting at the step where they are, for a person
// to decide. One that got there after the listing is held too, not moved.
func TestAnInstanceThatReachesAStepBeingHeldAtAfterTheListingIsHeld(t *testing.T) {
	f, v1, _, result := arrivesAtOpsApproveAfterTheListing(t, servicecontracts.NodeActionHold)

	instance := f.assertWaitingAt(t, v1, "opsApprove")
	f.assertNothingIsStranded(t)
	f.assertDecided(t, instance, entities.DeviationHold, impl.EventInstanceHeld)
	if open := f.openIncidentsOn(t, instance.ID, "opsApprove"); open != 1 {
		t.Errorf("%d incident(s) are open at the operations approval, want the one that holds the instance", open)
	}
	if result.Changed != 1 || len(result.PassedOver) != 0 {
		t.Errorf("the run says it acted on %d and passed over %d, want one and none", result.Changed, len(result.PassedOver))
	}
}

// TestTheStrandingReproducedInReviewNoLongerHappens is the reproduction from
// the review of the deviation ledger, both ways it was run: a migration that
// skips the operations approval, and one that decides nothing. Supervisor
// review is completed between the apply's listing and the instance's lock.
//
// What was observed then, in each: apply returned nil; the instance was on
// version 2 with a token and an open task on the operations approval, which
// version 2 does not have; and once its holder completed that task the
// instance stayed active with no token and no task.
func TestTheStrandingReproducedInReviewNoLongerHappens(t *testing.T) {
	runs := map[string][]servicecontracts.MigrationOption{
		"a skip of the operations approval": skipOps("the role was eliminated"),
		"a migration that only moves":       {servicecontracts.WithActor("dita")},
	}
	for name, opts := range runs {
		t.Run(name, func(t *testing.T) {
			f, listing := newRacedFixture(t)
			v1, v2 := f.waitingAtSupervisorReview(t)
			listing.atApplysListing(func() { f.completeTaskOn(t, "supervisorReview", "sam") })
			if err := f.svc.MigrateInstances(f.ctx, v1, v2, nil, opts...); err != nil {
				t.Fatalf("apply: %v", err)
			}
			reached(t, listing)

			instance := f.onlyInstance(t)
			onV2 := instance.Definition != nil && instance.Definition.ID == v2
			for _, token := range instance.Tokens {
				if onV2 && token.Node != nil && token.Node.ID == "opsApprove" {
					t.Errorf("the instance is on version 2 with a token on the operations approval, which version 2 does not have")
				}
			}
			for _, task := range f.openTasks(t) {
				if onV2 && task.NodeID() == "opsApprove" {
					t.Errorf("the instance is on version 2 with an open task on the operations approval")
				}
			}
			f.assertNothingIsStranded(t)
			// The holder's completion, which used to be the end of the
			// instance's progress and is now a step of it.
			f.runsToItsEnd(t)
		})
	}
}

// TestAnInstanceThatReachesADecidedStepJustBeforeItsLockIsLeftForTheNextRun.
//
// The apply reads the instance again before it decides, which narrows the
// window from the length of the run to a few statements. It does not close
// it: the decision and the rewrite are separate transactions, and an instance
// can still reach the step between that read and the rewrite's lock. The
// rewrite asks under its lock, and leaves alone an instance holding work the
// migration decides rather than moves; the next run finds it at the step and
// decides it.
func TestAnInstanceThatReachesADecidedStepJustBeforeItsLockIsLeftForTheNextRun(t *testing.T) {
	for _, kind := range everyNodeAction {
		t.Run(string(kind), func(t *testing.T) {
			f, listing, locks := newLockRacedFixture(t)
			v1, v2 := f.waitingAtSupervisorReview(t)
			beforeTheRewriteLocks(listing, locks, func() { f.completeTaskOn(t, "supervisorReview", "sam") })

			result, err := f.svc.ApplyInstanceMigration(f.ctx, v1, v2, nil, decideOps(kind, "the role was eliminated")...)
			if err != nil {
				t.Fatalf("apply: %v", err)
			}
			if !locks.fired {
				t.Fatal("the hook never reached the rewrite's lock; the test is not exercising the window")
			}

			instance := f.assertWaitingAt(t, v1, "opsApprove")
			f.assertNothingIsStranded(t)
			if rows := f.ledger(t, instance.ID); len(rows) != 0 {
				t.Errorf("an instance that was left alone has %d ledger row(s): %+v", len(rows), rows)
			}
			f.assertNoMigrationEntries(t, instance.ID)
			if result.Changed != 0 {
				t.Errorf("the run says it acted on %d instance(s); it wrote nothing", result.Changed)
			}
			assertPassedOver(t, result, instance, "Operations approve", "opsApprove")

			// The same migration again finds it at the step, and decides it.
			again, err := f.svc.ApplyInstanceMigration(f.ctx, v1, v2, nil, decideOps(kind, "the role was eliminated")...)
			if err != nil {
				t.Fatalf("the second run: %v", err)
			}
			if again.Changed != 1 || len(again.PassedOver) != 0 {
				t.Errorf("the second run says it acted on %d and passed over %d, want one and none", again.Changed, len(again.PassedOver))
			}
			if rows := f.ledger(t, instance.ID); len(rows) != 1 {
				t.Errorf("after the second run the ledger holds %d row(s), want the one decision", len(rows))
			}
			f.assertNothingIsStranded(t)
		})
	}
}

// TestAnInstanceThatReachesAHeldStepBothVersionsHaveIsNotMovedUndecided.
//
// The step need not be one the new version lacks. A hold says "a person looks
// at every instance waiting here before it goes anywhere"; when both versions
// have the step, an instance that reached it just before its lock would land
// perfectly well, and landing is not the question. Moved, it would be on the
// new version with nobody having looked, and no later run of the migration
// would find it. It is left where it is, and the next run holds it.
func TestAnInstanceThatReachesAHeldStepBothVersionsHaveIsNotMovedUndecided(t *testing.T) {
	f, listing, locks := newLockRacedFixture(t)
	v1, err := f.svc.CreateDefinition(f.ctx, quotationV1(f))
	if err != nil {
		t.Fatalf("deploy v1: %v", err)
	}
	if _, err := f.svc.StartProcess(f.ctx, f.project, "quotation", nil); err != nil {
		t.Fatalf("start a quotation: %v", err)
	}
	// The same steps again: a version that still has the operations approval.
	v2, err := f.svc.CreateDefinition(f.ctx, quotationV1(f))
	if err != nil {
		t.Fatalf("deploy v2: %v", err)
	}
	beforeTheRewriteLocks(listing, locks, func() { f.completeTaskOn(t, "supervisorReview", "sam") })
	hold := decideOps(servicecontracts.NodeActionHold, "ask the account manager")

	result, err := f.svc.ApplyInstanceMigration(f.ctx, v1, v2, nil, hold...)
	if err != nil {
		t.Fatalf("apply: %v", err)
	}
	if !locks.fired {
		t.Fatal("the hook never reached the rewrite's lock; the test is not exercising the window")
	}
	instance := f.assertWaitingAt(t, v1, "opsApprove")
	f.assertNoMigrationEntries(t, instance.ID)
	if result.Changed != 0 {
		t.Errorf("the run says it acted on %d instance(s); it wrote nothing", result.Changed)
	}
	assertPassedOver(t, result, instance, "Operations approve", "opsApprove")

	if _, err := f.svc.ApplyInstanceMigration(f.ctx, v1, v2, nil, hold...); err != nil {
		t.Fatalf("the second run: %v", err)
	}
	instance = f.assertWaitingAt(t, v1, "opsApprove")
	if rows := f.ledger(t, instance.ID); len(rows) != 1 || rows[0].Kind != entities.DeviationHold {
		t.Errorf("the second run should have held the instance: %+v", rows)
	}
	if open := f.openIncidentsOn(t, instance.ID, "opsApprove"); open != 1 {
		t.Errorf("%d incident(s) are open at the operations approval, want the one that holds the instance", open)
	}
}

// forked is start → split → (Branch A ‖ Branch B → Extra check) → join → end,
// and without the extra check when withExtra is false: an instance with two
// tokens, one of which can move onto a step the other version lacks.
func forked(projectID uuid.UUID, withExtra bool) *entities.ProcessDefinition {
	def := &entities.ProcessDefinition{
		Project: &entities.Project{ID: projectID},
		Key:     "forked-approval",
		Name:    "Forked approval",
		Nodes: []*entities.Node{
			{ID: "start", Type: entities.StartEvent, Outgoing: []string{"f1"}},
			{ID: "split", Type: entities.ParallelGateway, Incoming: []string{"f1"}, Outgoing: []string{"f2", "f3"}},
			{ID: "branchA", Name: "Branch A", Type: entities.UserTask, Assignee: "ada", Incoming: []string{"f2"}, Outgoing: []string{"f4"}},
			{ID: "branchB", Name: "Branch B", Type: entities.UserTask, Assignee: "bo", Incoming: []string{"f3"}, Outgoing: []string{"f5"}},
			{ID: "join", Type: entities.ParallelGateway, Incoming: []string{"f4", "f6"}, Outgoing: []string{"f7"}},
			{ID: "end", Type: entities.EndEvent, Incoming: []string{"f7"}},
		},
		Flows: []*entities.SequenceFlow{
			{ID: "f1", SourceRef: "start", TargetRef: "split"},
			{ID: "f2", SourceRef: "split", TargetRef: "branchA"},
			{ID: "f3", SourceRef: "split", TargetRef: "branchB"},
			{ID: "f4", SourceRef: "branchA", TargetRef: "join"},
			{ID: "f7", SourceRef: "join", TargetRef: "end"},
		},
	}
	if !withExtra {
		def.Nodes[4].Incoming = []string{"f4", "f5"}
		def.Flows = append(def.Flows, &entities.SequenceFlow{ID: "f5", SourceRef: "branchB", TargetRef: "join"})
		return def
	}
	def.Nodes = append(def.Nodes, &entities.Node{
		ID: "extraCheck", Name: "Extra check", Type: entities.UserTask, Assignee: "cyd", Incoming: []string{"f5"}, Outgoing: []string{"f6"},
	})
	def.Flows = append(def.Flows,
		&entities.SequenceFlow{ID: "f5", SourceRef: "branchB", TargetRef: "extraCheck"},
		&entities.SequenceFlow{ID: "f6", SourceRef: "extraCheck", TargetRef: "join"})
	return def
}

// forkedAndListed starts a forked approval on the version with the extra check
// and deploys the one without, and has Branch B completed — so that its token
// reaches the extra check — the moment the next apply has listed the instance.
func forkedAndListed(t *testing.T) (f *fixture, v1, v2 uuid.UUID) {
	t.Helper()
	f, listing := newRacedFixture(t)
	v1, err := f.svc.CreateDefinition(f.ctx, forked(f.project, true))
	if err != nil {
		t.Fatalf("deploy v1: %v", err)
	}
	if _, err := f.svc.StartProcess(f.ctx, f.project, "forked-approval", nil); err != nil {
		t.Fatalf("start: %v", err)
	}
	v2, err = f.svc.CreateDefinition(f.ctx, forked(f.project, false))
	if err != nil {
		t.Fatalf("deploy v2: %v", err)
	}
	listing.atApplysListing(func() { f.completeTaskOn(t, "branchB", "bo") })
	return f, v1, v2
}

// Two tokens, and only one of them moved. The instance is one row: it is not
// moved in part. A migration that only moves work leaves the whole of it on
// the version it runs, the branch that did not move included.
func TestAnInstanceWithOneOfTwoTokensOnAStepTheNewVersionLacksIsNotMoved(t *testing.T) {
	f, v1, v2 := forkedAndListed(t)
	result, err := f.svc.ApplyInstanceMigration(f.ctx, v1, v2, nil, servicecontracts.WithActor("dita"))
	if err != nil {
		t.Fatalf("apply: %v", err)
	}

	instance := f.onlyInstance(t)
	if instance.Definition == nil || instance.Definition.ID != v1 {
		t.Errorf("the instance was moved although one of its branches waits at a step the new version lacks")
	}
	if on := tokenNodes(instance); len(on) != 2 || on[0] != "branchA" || on[1] != "extraCheck" {
		t.Errorf("the instance holds %v, want its two branches where their holders left them", on)
	}
	f.assertNothingIsStranded(t)
	f.assertNoMigrationEntries(t, instance.ID)
	if result.Changed != 0 {
		t.Errorf("the run says it acted on %d instance(s); it wrote nothing", result.Changed)
	}
	assertPassedOver(t, result, instance, "Extra check", "extraCheck")
	f.runsToItsEnd(t)
}

// The same with a skip of the step the token reached: that branch is advanced
// past it, and the instance is then moved whole.
func TestAnInstanceWithOneOfTwoTokensOnASkippedStepIsSkippedThereAndMoved(t *testing.T) {
	f, v1, v2 := forkedAndListed(t)
	result, err := f.svc.ApplyInstanceMigration(f.ctx, v1, v2, nil,
		servicecontracts.WithNodeActions(map[string]servicecontracts.NodeAction{
			"extraCheck": {Kind: servicecontracts.NodeActionSkip, Reason: "the check was dropped"},
		}), servicecontracts.WithActor("dita"))
	if err != nil {
		t.Fatalf("apply: %v", err)
	}

	instance := f.onlyInstance(t)
	if instance.Status != entities.ProcessActive || instance.Definition == nil || instance.Definition.ID != v2 {
		t.Fatalf("the instance is %s on %v; it should be running the new version", instance.Status, instance.Definition)
	}
	for _, token := range instance.Tokens {
		if token.Node != nil && token.Node.ID == "extraCheck" {
			t.Errorf("the instance still holds a token on the extra check, which the new version does not have")
		}
	}
	if rows := f.ledger(t, instance.ID); len(rows) != 1 || rows[0].Kind != entities.DeviationWaive || rows[0].Node == nil || rows[0].Node.ID != "extraCheck" {
		t.Errorf("the ledger should hold the one skip of the extra check: %+v", rows)
	}
	if result.Changed != 1 || len(result.PassedOver) != 0 {
		t.Errorf("the run says it acted on %d and passed over %d, want one and none", result.Changed, len(result.PassedOver))
	}
	f.assertNothingIsStranded(t)
	f.runsToItsEnd(t)
}

// twoApprovals is review → first approval → second approval → sign, and
// review → sign when withApprovals is false: two removed steps in a row.
func twoApprovals(projectID uuid.UUID, withApprovals bool) *entities.ProcessDefinition {
	def := &entities.ProcessDefinition{
		Project: &entities.Project{ID: projectID},
		Key:     "two-approvals",
		Name:    "Two approvals",
		Nodes: []*entities.Node{
			{ID: "start", Type: entities.StartEvent, Outgoing: []string{"a1"}},
			{ID: "review", Name: "Review", Type: entities.UserTask, Assignee: "sam", Incoming: []string{"a1"}, Outgoing: []string{"a2"}},
			{ID: "sign", Name: "Sign", Type: entities.UserTask, Assignee: "sasha", Incoming: []string{"a2"}, Outgoing: []string{"a5"}},
			{ID: "end", Type: entities.EndEvent, Incoming: []string{"a5"}},
		},
		Flows: []*entities.SequenceFlow{
			{ID: "a1", SourceRef: "start", TargetRef: "review"},
			{ID: "a5", SourceRef: "sign", TargetRef: "end"},
		},
	}
	if !withApprovals {
		def.Flows = append(def.Flows, &entities.SequenceFlow{ID: "a2", SourceRef: "review", TargetRef: "sign"})
		return def
	}
	def.Nodes[2].Incoming = []string{"a4"}
	def.Nodes = append(def.Nodes,
		&entities.Node{ID: "firstApprove", Name: "First approval", Type: entities.UserTask, Assignee: "fay", Incoming: []string{"a2"}, Outgoing: []string{"a3"}},
		&entities.Node{ID: "secondApprove", Name: "Second approval", Type: entities.UserTask, Assignee: "sol", Incoming: []string{"a3"}, Outgoing: []string{"a4"}})
	def.Flows = append(def.Flows,
		&entities.SequenceFlow{ID: "a2", SourceRef: "review", TargetRef: "firstApprove"},
		&entities.SequenceFlow{ID: "a3", SourceRef: "firstApprove", TargetRef: "secondApprove"},
		&entities.SequenceFlow{ID: "a4", SourceRef: "secondApprove", TargetRef: "sign"})
	return def
}

// TestASkipThatLandsOnAStepTheNewVersionLacksLeavesTheInstanceOnItsVersion.
//
// Nothing races here: the instance is where the listing found it. The skip
// itself is what moves it, onto the step after the one skipped, and the plan
// was made for where it stood before. When the new version has no such step
// either, the rewrite used to move the instance there all the same. The skip
// stands and is recorded; the instance stays on the version it runs, and the
// run says so.
func TestASkipThatLandsOnAStepTheNewVersionLacksLeavesTheInstanceOnItsVersion(t *testing.T) {
	f := newFixture(t)
	v1, err := f.svc.CreateDefinition(f.ctx, twoApprovals(f.project, true))
	if err != nil {
		t.Fatalf("deploy v1: %v", err)
	}
	if _, err := f.svc.StartProcess(f.ctx, f.project, "two-approvals", nil); err != nil {
		t.Fatalf("start: %v", err)
	}
	f.completeTaskOn(t, "review", "sam")
	v2, err := f.svc.CreateDefinition(f.ctx, twoApprovals(f.project, false))
	if err != nil {
		t.Fatalf("deploy v2: %v", err)
	}
	skip := func(steps ...string) []servicecontracts.MigrationOption {
		actions := map[string]servicecontracts.NodeAction{}
		for _, step := range steps {
			actions[step] = servicecontracts.NodeAction{Kind: servicecontracts.NodeActionSkip, Reason: "the approvals were dropped"}
		}
		return []servicecontracts.MigrationOption{servicecontracts.WithNodeActions(actions), servicecontracts.WithActor("dita")}
	}

	result, err := f.svc.ApplyInstanceMigration(f.ctx, v1, v2, nil, skip("firstApprove")...)
	if err != nil {
		t.Fatalf("apply: %v", err)
	}
	instance := f.assertWaitingAt(t, v1, "secondApprove")
	f.assertNothingIsStranded(t)
	if rows := f.ledger(t, instance.ID); len(rows) != 1 || rows[0].Kind != entities.DeviationWaive || rows[0].Node == nil || rows[0].Node.ID != "firstApprove" {
		t.Errorf("the skip that was made should be in the ledger, and nothing else: %+v", rows)
	}
	for _, told := range f.trailTypes(t, instance.ID) {
		if told == impl.EventInstanceMigrated {
			t.Error("the trail says the instance was moved; it was not")
		}
	}
	if result.Changed != 1 {
		t.Errorf("the run says it acted on %d instance(s); it skipped a step of one", result.Changed)
	}
	assertPassedOver(t, result, instance, "Second approval", "secondApprove")

	// Deciding the second step as well is what moves it.
	if _, err := f.svc.ApplyInstanceMigration(f.ctx, v1, v2, nil, skip("firstApprove", "secondApprove")...); err != nil {
		t.Fatalf("the second run: %v", err)
	}
	f.assertWaitingAt(t, v2, "sign")
	f.runsToItsEnd(t)
}

// repeating is each reviewer in turn → sign, and sign alone when withReviews
// is false: a step that runs once per item, removed.
func repeating(projectID uuid.UUID, withReviews bool) *entities.ProcessDefinition {
	def := &entities.ProcessDefinition{
		Project: &entities.Project{ID: projectID},
		Key:     "repeating-review",
		Name:    "Repeating review",
		Nodes: []*entities.Node{
			{ID: "start", Type: entities.StartEvent, Outgoing: []string{"r1"}},
			{ID: "sign", Name: "Sign", Type: entities.UserTask, Assignee: "sasha", Incoming: []string{"r2"}, Outgoing: []string{"r3"}},
			{ID: "end", Type: entities.EndEvent, Incoming: []string{"r3"}},
		},
		Flows: []*entities.SequenceFlow{
			{ID: "r3", SourceRef: "sign", TargetRef: "end"},
		},
	}
	if !withReviews {
		def.Nodes[1].Incoming = []string{"r1"}
		def.Flows = append(def.Flows, &entities.SequenceFlow{ID: "r1", SourceRef: "start", TargetRef: "sign"})
		return def
	}
	def.Nodes = append(def.Nodes, &entities.Node{
		ID: "eachReviewer", Name: "Review by each", Type: entities.UserTask, Assignee: "ada",
		MultiInstanceType: "parallel", Collection: "reviewers", ElementVariable: "reviewer",
		Incoming: []string{"r1"}, Outgoing: []string{"r2"},
	})
	def.Flows = append(def.Flows,
		&entities.SequenceFlow{ID: "r1", SourceRef: "start", TargetRef: "eachReviewer"},
		&entities.SequenceFlow{ID: "r2", SourceRef: "eachReviewer", TargetRef: "sign"})
	return def
}

// assertEveryTokenHasAStep fails when an instance holds a token on a step the
// version it runs does not have.
func (f *fixture) assertEveryTokenHasAStep(t *testing.T) {
	t.Helper()
	instances, err := f.svc.ListInstances(f.ctx, f.project)
	if err != nil {
		t.Fatalf("list instances: %v", err)
	}
	for _, instance := range instances {
		version, err := f.svc.GetDefinition(f.ctx, instance.Definition.ID)
		if err != nil {
			t.Fatalf("read the version instance %s runs: %v", instance.ID, err)
		}
		for _, token := range instance.Tokens {
			if token.Node == nil || version.FindNode(token.Node.ID) == nil {
				t.Errorf("STRANDED: instance %s holds a token on %s, a step version %d does not have",
					instance.ID, nodeOf(token.Node), version.Version)
			}
		}
	}
}

// TestASkipThatLeavesPartOfARepeatingStepBehindDoesNotMoveItToAVersionWithoutTheStep.
//
// Nothing races here either. A skip of a step that runs once per item counts
// one of them and leaves the others' tokens on the step (recorded in the
// roadmap, and not this change's to mend). The rewrite then moved the instance,
// those tokens with it, onto a version that has no such step. It is left on
// the version it runs instead, where the same migration, run again, finds the
// rest of the step and skips it; once nothing of the step is left, it moves.
//
// Asserted as what must hold however many runs the step takes, so that a skip
// which one day ends the whole step at once still passes.
func TestASkipThatLeavesPartOfARepeatingStepBehindDoesNotMoveItToAVersionWithoutTheStep(t *testing.T) {
	f := newFixture(t)
	v1, err := f.svc.CreateDefinition(f.ctx, repeating(f.project, true))
	if err != nil {
		t.Fatalf("deploy v1: %v", err)
	}
	if _, err := f.svc.StartProcess(f.ctx, f.project, "repeating-review", map[string]any{"reviewers": []any{"x", "y", "z"}}); err != nil {
		t.Fatalf("start: %v", err)
	}
	v2, err := f.svc.CreateDefinition(f.ctx, repeating(f.project, false))
	if err != nil {
		t.Fatalf("deploy v2: %v", err)
	}
	opts := []servicecontracts.MigrationOption{
		servicecontracts.WithNodeActions(map[string]servicecontracts.NodeAction{
			"eachReviewer": {Kind: servicecontracts.NodeActionSkip, Reason: "the reviews were dropped"},
		}),
		servicecontracts.WithActor("dita"),
	}

	moved := false
	for run := 1; run <= 5 && !moved; run++ {
		result, err := f.svc.ApplyInstanceMigration(f.ctx, v1, v2, nil, opts...)
		if err != nil {
			t.Fatalf("run %d: %v", run, err)
		}
		f.assertEveryTokenHasAStep(t)
		instance := f.onlyInstance(t)
		moved = instance.Definition != nil && instance.Definition.ID == v2
		if !moved {
			// Left behind, and said to be, by the name its step goes by.
			assertPassedOver(t, result, instance, "Review by each", "eachReviewer")
		}
	}
	if !moved {
		t.Fatal("five runs of the same migration did not move the instance; it can never leave the old version")
	}
	f.assertWaitingAt(t, v2, "sign")
	f.runsToItsEnd(t)
}
