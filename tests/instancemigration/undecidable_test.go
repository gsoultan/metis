package instancemigration

import (
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/gsoultan/metis/server/domains/entities"
	servicecontracts "github.com/gsoultan/metis/server/domains/services/contracts"
	"github.com/gsoultan/metis/server/endpoints/definition"
)

// A decision that can never be taken.
//
// A skip, a cancel or a hold is taken on the instances that hold a token on
// the step it names. No instance ever holds a token on a boundary event: the
// token is on the step the event is attached to, and the event keeps only its
// waiting message, or its timer. A decision naming the event was accepted by
// the plan, taken on nobody, and recorded nowhere — and because the plan lets
// work on a decided step go without anywhere to land, it also excused the
// event's waiting message from having to land. The instance was moved, and
// went on waiting for a message on a step its version does not have.

// withdrawable is start → approve → end, where a message that the customer
// withdrew interrupts the approval and leads to closing the file. Without the
// event and what follows it when listening is false: the new version stopped
// listening.
func withdrawable(projectID uuid.UUID, listening bool) *entities.ProcessDefinition {
	def := &entities.ProcessDefinition{
		Project: &entities.Project{ID: projectID},
		Key:     "withdrawable-request",
		Name:    "Withdrawable request",
		Nodes: []*entities.Node{
			{ID: "start", Type: entities.StartEvent, Outgoing: []string{"w1"}},
			{ID: "approve", Name: "Approve the request", Type: entities.UserTask, Assignee: "ada", Incoming: []string{"w1"}, Outgoing: []string{"w2"}},
			{ID: "end", Type: entities.EndEvent, Incoming: []string{"w2"}},
		},
		Flows: []*entities.SequenceFlow{
			{ID: "w1", SourceRef: "start", TargetRef: "approve"},
			{ID: "w2", SourceRef: "approve", TargetRef: "end"},
		},
	}
	if !listening {
		return def
	}
	def.Nodes = append(def.Nodes,
		&entities.Node{
			ID: "customerWithdrew", Name: "The customer withdrew", Type: entities.BoundaryEvent, AttachedToRef: "approve",
			Properties: map[string]any{"message_name": "customer-withdrew"}, Outgoing: []string{"w3"},
		},
		&entities.Node{ID: "closeFile", Name: "Close the file", Type: entities.UserTask, Assignee: "cyd", Incoming: []string{"w3"}, Outgoing: []string{"w4"}},
		&entities.Node{ID: "closed", Type: entities.EndEvent, Incoming: []string{"w4"}})
	def.Flows = append(def.Flows,
		&entities.SequenceFlow{ID: "w3", SourceRef: "customerWithdrew", TargetRef: "closeFile"},
		&entities.SequenceFlow{ID: "w4", SourceRef: "closeFile", TargetRef: "closed"})
	return def
}

// TestADecisionOnABoundaryEventIsRefusedInThePlan.
//
// Root cause: the planner asked whether a decided node exists, never whether
// an instance can be waiting at it, and excused the work on any decided node
// from having to land.
//
// An approval is open, with a boundary event listening for the customer
// withdrawing. The new version has no such event. Before, with a decision
// naming the event: the plan was applicable, the apply said it had acted on
// one instance, nothing was skipped, ended or held, and the instance was on
// version 2 still waiting for the message on a step version 2 lacks. The
// message was then accepted without error and did nothing.
func TestADecisionOnABoundaryEventIsRefusedInThePlan(t *testing.T) {
	for _, kind := range everyNodeAction {
		t.Run(string(kind), func(t *testing.T) {
			f := newFixture(t)
			v1, v2 := f.startedOn(t, withdrawable(f.project, true), withdrawable(f.project, false))
			request := func(dryRun bool) definition.MigrateInstancesRequest {
				return definition.MigrateInstancesRequest{
					SourceDefinitionID: v1.String(), TargetDefinitionID: v2.String(), DryRun: &dryRun,
					NodeActions: map[string]servicecontracts.NodeAction{
						"customerWithdrew": {Kind: kind, Reason: "the new version does not listen for it"},
					},
				}
			}

			// The dry run shows the refusal, naming the step and the step to
			// decide instead as people know them.
			_, body := f.migrateOverTheEndpoint(t, request(true))
			plan := planOf(t, body)
			refusals := strings.Join(plan.Refusals, "; ")
			t.Logf("refused, and told: %s", refusals)
			if plan.Applicable() {
				t.Fatalf("a %s on a boundary event, which no instance ever waits at, was reported as applicable", kind)
			}
			for _, want := range []string{string(kind), "The customer withdrew", "boundary event", "Approve the request"} {
				if !strings.Contains(refusals, want) {
					t.Errorf("the refusal does not say %q: %s", want, refusals)
				}
			}

			// And the apply refuses the same thing, having written nothing.
			reply, err := definition.MakeMigrateInstancesEndpoint(f.svc)(f.ctx, request(false))
			if err != nil {
				t.Fatalf("migrate: %v", err)
			}
			answered := reply.(definition.MigrateInstancesResponse)
			if answered.Err == nil || answered.Applied || !strings.Contains(answered.Err.Error(), "boundary event") {
				t.Fatalf("the apply should have been refused for the decision on the boundary event: applied=%v err=%v", answered.Applied, answered.Err)
			}
			instance := f.assertWaitingAt(t, v1, "approve")
			if rows := f.ledger(t, instance.ID); len(rows) != 0 {
				t.Errorf("a refused migration left %d ledger row(s): %+v", len(rows), rows)
			}
			f.assertNoMigrationEntries(t, instance.ID)
			if waiting := f.waitingEventRows(t, instance.ID); len(waiting) != 1 || !strings.HasPrefix(waiting[0], "node=customerWithdrew ") {
				t.Errorf("the instance should still be listening on the boundary event: %v", waiting)
			}
			f.assertNothingIsStranded(t)

			// So the message still does what the version it runs says.
			if err := f.svc.SendMessage(f.ctx, f.project, "customer-withdrew", "", nil); err != nil {
				t.Fatalf("send the message: %v", err)
			}
			f.assertWaitingAt(t, v1, "closeFile")
			f.runsToItsEnd(t)
		})
	}
}

// Without the decision the plan was always right about this: the event's
// waiting message has nowhere to land, and the migration is refused for it.
// The refusal above must not have been bought by losing this one.
func TestAWaitingMessageOnABoundaryEventTheNewVersionLacksStillStopsTheMigration(t *testing.T) {
	f := newFixture(t)
	v1, v2 := f.startedOn(t, withdrawable(f.project, true), withdrawable(f.project, false))
	plan, err := f.svc.PlanInstanceMigration(f.ctx, v1, v2, nil)
	if err != nil {
		t.Fatalf("plan: %v", err)
	}
	if plan.Applicable() || !strings.Contains(strings.Join(plan.Refusals, "; "), "nowhere to put the work parked on customerWithdrew") {
		t.Fatalf("the migration should be refused for the waiting message: %v", plan.Refusals)
	}
	if err := f.svc.MigrateInstances(f.ctx, v1, v2, nil); err == nil {
		t.Fatal("the apply accepted what the plan refused")
	}
	f.assertWaitingAt(t, v1, "approve")
	f.assertNothingIsStranded(t)
}

// TestWorkLeftOnADecidedStepIsNotCarriedToAVersionWithoutTheStep.
//
// The plan lets work on a step the migration decides go without anywhere to
// land, because that work is to be skipped, cancelled or held. By the time
// the instance is rewritten the decisions have been taken, and a token still
// on such a step already keeps the instance where it is. A waiting event or
// an open task still there, with no token under it, did not: it was carried
// to the new version naming a step that version does not have.
//
// The engine does not leave either behind (leaving a step lets go of what
// waits on it and withdraws its tasks), so the state is made by hand here, for
// a quotation that is waiting at the step before the operations approval.
// What is asserted is the rule, should anything ever leave one.
func TestWorkLeftOnADecidedStepIsNotCarriedToAVersionWithoutTheStep(t *testing.T) {
	left := map[string]string{
		"a waiting event": `
			INSERT INTO event_subscriptions (id, created_at, updated_at, project_id, instance_id, node_id, type, event_name, correlation_key)
			SELECT gen_random_uuid(), now(), now(), project_id, id, 'opsApprove', 'message', 'operations-replied', ''
			  FROM process_instances WHERE id = ?`,
		"an open task": `
			INSERT INTO tasks (id, created_at, updated_at, project_id, instance_id, node_id, name, type, status, assignee,
			                   candidate_users, candidate_groups, priority, variables)
			SELECT gen_random_uuid(), now(), now(), project_id, instance_id, 'opsApprove', 'Operations approve', type, 'claimed', 'ollie',
			       candidate_users, candidate_groups, priority, variables
			  FROM tasks WHERE instance_id = ? AND node_id = 'supervisorReview'`,
	}
	for what, leave := range left {
		t.Run(what, func(t *testing.T) {
			f := newFixture(t)
			v1, v2 := f.waitingAtSupervisorReview(t)
			instance := f.onlyInstance(t)
			if err := f.db.WithContext(f.ctx).Exec(leave, instance.ID).Error; err != nil {
				t.Fatalf("leave %s on the operations approval: %v", what, err)
			}

			result, err := f.svc.ApplyInstanceMigration(f.ctx, v1, v2, nil, skipOps("the role was eliminated")...)
			if err != nil {
				t.Fatalf("apply: %v", err)
			}

			// Not rewritten: on the version it runs, where it was waiting.
			after := f.onlyInstance(t)
			if after.Definition == nil || after.Definition.ID != v1 {
				t.Errorf("the instance was moved, with %s on a step the new version does not have", what)
			}
			if on := tokenNodes(after); len(on) != 1 || !strings.HasPrefix(on[0], "supervisorReview") {
				t.Errorf("the instance holds %v, want its one token on the supervisor's review", on)
			}
			f.assertEveryTokenHasAStep(t)
			// The open task left by hand is itself a task with no token under
			// it: the premise of that case, not something the migration did.
			if what == "a waiting event" {
				f.assertNothingIsStranded(t)
			}
			f.assertNoMigrationEntries(t, instance.ID)
			if rows := f.ledger(t, instance.ID); len(rows) != 0 {
				t.Errorf("an instance that was left alone has %d ledger row(s): %+v", len(rows), rows)
			}
			if result.Changed != 0 {
				t.Errorf("the run says it acted on %d instance(s); it wrote nothing", result.Changed)
			}
			assertPassedOver(t, result, instance, "Operations approve", "opsApprove")
		})
	}
}
