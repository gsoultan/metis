package instancemigration

import (
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/gsoultan/metis/server/domains/entities"
	servicecontracts "github.com/gsoultan/metis/server/domains/services/contracts"
	"github.com/gsoultan/metis/server/endpoints/definition"
)

// A decision naming a sub-process.
//
// Entering an embedded sub-process takes the token off the sub-process and
// puts it on the steps inside. A skip, a cancel or a hold naming the
// sub-process therefore found no instance to act on, exactly as one naming a
// boundary event did: accepted by the plan, taken on nobody, recorded nowhere,
// and the instance moved as though nothing had been asked.

// checkedInside is start → a sub-process holding one check → sign → end.
func checkedInside(projectID uuid.UUID) *entities.ProcessDefinition {
	return &entities.ProcessDefinition{
		Project: &entities.Project{ID: projectID},
		Key:     "checked-inside",
		Name:    "Checked inside",
		Nodes: []*entities.Node{
			{ID: "start", Type: entities.StartEvent, Outgoing: []string{"k1"}},
			{
				ID: "checks", Name: "Checks", Type: entities.SubProcess, Incoming: []string{"k1"}, Outgoing: []string{"k2"},
				Nodes: []*entities.Node{
					{ID: "checksStart", Type: entities.StartEvent, ParentID: "checks", Outgoing: []string{"i1"}},
					{ID: "check", Name: "Check the request", Type: entities.UserTask, Assignee: "cyd", ParentID: "checks", Incoming: []string{"i1"}, Outgoing: []string{"i2"}},
					{ID: "checksEnd", Type: entities.EndEvent, ParentID: "checks", Incoming: []string{"i2"}},
				},
				Flows: []*entities.SequenceFlow{
					{ID: "i1", SourceRef: "checksStart", TargetRef: "check"},
					{ID: "i2", SourceRef: "check", TargetRef: "checksEnd"},
				},
			},
			{ID: "sign", Name: "Sign", Type: entities.UserTask, Assignee: "sasha", Incoming: []string{"k2"}, Outgoing: []string{"k3"}},
			{ID: "end", Type: entities.EndEvent, Incoming: []string{"k3"}},
		},
		Flows: []*entities.SequenceFlow{
			{ID: "k1", SourceRef: "start", TargetRef: "checks"},
			{ID: "k2", SourceRef: "checks", TargetRef: "sign"},
			{ID: "k3", SourceRef: "sign", TargetRef: "end"},
		},
	}
}

// TestADecisionOnASubProcessIsRefusedInThePlan.
//
// Root cause: the planner refused a decision on two kinds of node an instance
// never waits at, by name, where the rule is every node the engine does not
// leave a token on.
//
// An instance is inside the sub-process, at the check. Before, for a skip, a
// cancel and a hold naming the sub-process alike: the plan was applicable, the
// apply said it had acted on one instance, nothing was skipped, ended or held
// and nothing recorded, and the instance was on version 2.
func TestADecisionOnASubProcessIsRefusedInThePlan(t *testing.T) {
	for _, kind := range everyNodeAction {
		t.Run(string(kind), func(t *testing.T) {
			f := newFixture(t)
			v1, v2 := f.startedOn(t, checkedInside(f.project), checkedInside(f.project))
			instance := f.assertWaitingAt(t, v1, "check")
			request := func(dryRun bool, node string) definition.MigrateInstancesRequest {
				return definition.MigrateInstancesRequest{
					SourceDefinitionID: v1.String(), TargetDefinitionID: v2.String(), DryRun: &dryRun,
					NodeActions: map[string]servicecontracts.NodeAction{
						node: {Kind: kind, Reason: "the checks are under review"},
					},
				}
			}

			// The dry run refuses, naming the sub-process and the step inside
			// it that can be decided.
			_, body := f.migrateOverTheEndpoint(t, request(true, "checks"))
			plan := planOf(t, body)
			refusals := strings.Join(plan.Refusals, "; ")
			t.Logf("refused, and told: %s", refusals)
			if plan.Applicable() {
				t.Fatalf("a %s naming a sub-process, which no instance waits at, was reported as applicable", kind)
			}
			for _, want := range []string{string(kind), `"Checks"`, "sub-process", `"Check the request"`} {
				if !strings.Contains(refusals, want) {
					t.Errorf("the refusal does not say %q: %s", want, refusals)
				}
			}

			// The apply refuses the same thing, having written nothing.
			reply, err := definition.MakeMigrateInstancesEndpoint(f.svc)(f.ctx, request(false, "checks"))
			if err != nil {
				t.Fatalf("migrate: %v", err)
			}
			if answered := reply.(definition.MigrateInstancesResponse); answered.Err == nil || answered.Applied {
				t.Fatalf("the apply should have been refused: applied=%v err=%v", answered.Applied, answered.Err)
			}
			f.assertWaitingAt(t, v1, "check")
			if rows := f.ledger(t, instance.ID); len(rows) != 0 {
				t.Errorf("a refused migration left %d ledger row(s): %+v", len(rows), rows)
			}
			f.assertNoMigrationEntries(t, instance.ID)
			f.assertNothingIsStranded(t)

			// Deciding the step inside, as the refusal says, is taken.
			result, err := f.applyDecided(t, kind, v1, v2, nil,
				servicecontracts.WithNodeActions(request(false, "check").NodeActions), servicecontracts.WithActor("dita"))
			if err != nil {
				t.Fatalf("decide the step inside: %v", err)
			}
			if result.Changed != 1 || len(result.PassedOver) != 0 {
				t.Errorf("the run says it acted on %d and passed over %d, want one and none", result.Changed, len(result.PassedOver))
			}
			if rows := f.ledger(t, instance.ID); len(rows) != 1 || rows[0].Node == nil || rows[0].Node.ID != "check" {
				t.Errorf("the decision at the step inside should be in the ledger: %+v", rows)
			}
		})
	}
}

// A definition is somebody's input, and nothing stops a sub-process in it from
// naming itself as its parent: it deploys. Working out what to decide instead
// of that sub-process used to go round it until the stack gave out, which took
// the server with it — from a dry run, with no instance running.
func TestADryRunOverASubProcessThatIsItsOwnParentAnswers(t *testing.T) {
	f := newFixture(t)
	looped := func() *entities.ProcessDefinition {
		def := checkedInside(f.project)
		def.Key, def.Name = "looped-inside", "Looped inside"
		def.Nodes[1].ParentID = "checks"
		return def
	}
	v1, err := f.svc.CreateDefinition(f.ctx, looped())
	if err != nil {
		t.Fatalf("deploy v1: %v", err)
	}
	v2, err := f.svc.CreateDefinition(f.ctx, looped())
	if err != nil {
		t.Fatalf("deploy v2: %v", err)
	}

	answered := make(chan []string, 1)
	go func() {
		plan, planErr := f.svc.PlanInstanceMigration(f.ctx, v1, v2, nil,
			servicecontracts.WithNodeActions(map[string]servicecontracts.NodeAction{
				"checks": {Kind: servicecontracts.NodeActionHold, Reason: "the checks are under review"},
			}))
		if planErr != nil {
			answered <- []string{"error: " + planErr.Error()}
			return
		}
		answered <- plan.Refusals
	}()
	select {
	case refusals := <-answered:
		told := strings.Join(refusals, "; ")
		if !strings.Contains(told, `"Checks" cannot be taken`) || !strings.Contains(told, `"Check the request"`) {
			t.Fatalf("the dry run should refuse the decision on the sub-process and name the step inside it: %s", told)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("the dry run had not answered after five seconds")
	}
}
