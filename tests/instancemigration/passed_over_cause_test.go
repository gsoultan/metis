package instancemigration

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/gsoultan/metis/server/domains/entities"
	servicecontracts "github.com/gsoultan/metis/server/domains/services/contracts"
	"github.com/gsoultan/metis/server/endpoints/definition"
	"github.com/gsoultan/metis/server/endpoints/deviation"
)

// causedStep is a step a passed-over entry is about, as a client reads it.
type causedStep struct {
	NodeID string `json:"node_id"`
	Name   string `json:"name"`
}

// causedReply is a reply that lists passed-over instances — the migrate
// route's, or the approval's of a migration — as a client that translates it
// reads it. Steps is a pointer so that a list left out can be told from an
// empty one.
type causedReply struct {
	PassedOver []struct {
		InstanceID string        `json:"instance_id"`
		Cause      string        `json:"cause"`
		Steps      *[]causedStep `json:"steps"`
		Reason     string        `json:"reason"`
	} `json:"passed_over"`
}

// causesOverTheEndpoint asks the migration endpoint — and, for an apply it
// sends for approval, the approval's — and reads the reply as JSON.
func (f *fixture) causesOverTheEndpoint(t *testing.T, request definition.MigrateInstancesRequest) (causedReply, string) {
	t.Helper()
	body, err := json.Marshal(f.answerOf(t, request))
	if err != nil {
		t.Fatalf("encode the reply: %v", err)
	}
	var read causedReply
	if err := json.Unmarshal(body, &read); err != nil {
		t.Fatalf("decode the reply: %v (%s)", err, body)
	}
	return read, string(body)
}

// requireCause fails unless the reply passed over exactly one instance, for
// the cause given — one the server's closed set names — at the steps given,
// each by its id and by the name people know it by, with a sentence beside it
// that names each step by that name and never by its id.
func requireCause(t *testing.T, reply causedReply, body, cause string, steps ...causedStep) {
	t.Helper()
	if len(reply.PassedOver) != 1 {
		t.Fatalf("passed_over is %s, want the one instance", body)
	}
	entry := reply.PassedOver[0]
	if entry.Cause != cause || !entities.PassedOverCause(entry.Cause).Valid() {
		t.Fatalf("the cause is %q, want %q: %s", entry.Cause, cause, body)
	}
	if entry.Steps == nil {
		t.Fatalf("steps is left out or null, want a list always: %s", body)
	}
	if len(*entry.Steps) != len(steps) {
		t.Fatalf("the steps are %+v, want %+v: %s", *entry.Steps, steps, body)
	}
	for i, step := range steps {
		if (*entry.Steps)[i] != step {
			t.Fatalf("the steps are %+v, want %+v: %s", *entry.Steps, steps, body)
		}
		if step.Name != step.NodeID && (!strings.Contains(entry.Reason, step.Name) || strings.Contains(entry.Reason, step.NodeID)) {
			t.Errorf("the sentence beside the cause should name %q and not %q: %q", step.Name, step.NodeID, entry.Reason)
		}
	}
	if entry.Reason == "" {
		t.Errorf("the sentence beside the cause is gone: %s", body)
	}
}

// Rulings addendum §12. The reply named an instance it passed over and said
// why in an English sentence, which a dialog in another language could only
// show as it stood. Each entry now also carries the cause as a code and the
// steps it concerns, by id and by name; the sentence is unchanged beside
// them. The same on the reply of an approval, for a run that needed one.
func TestAPassedOverInstanceCarriesItsCauseAndItsStepsName(t *testing.T) {
	apply := false

	t.Run("it left the step a decision was about", func(t *testing.T) {
		f, listing := newRacedFixture(t)
		v1, v2 := f.parkedOnOpsApprove(t)
		listing.atTheEndpointsApplyListing(func() { f.completeTaskOn(t, "opsApprove", "ollie") })
		reply, body := f.causesOverTheEndpoint(t, definition.MigrateInstancesRequest{
			SourceDefinitionID: v1, TargetDefinitionID: v2, DryRun: &apply,
			NodeActions: map[string]servicecontracts.NodeAction{
				"opsApprove": {Kind: servicecontracts.NodeActionCancel, Reason: "the step should never have been there; re-quote"},
			},
		})
		reached(t, listing)
		requireCause(t, reply, body, "left_the_step", causedStep{NodeID: "opsApprove", Name: "Operations approve"})
	})

	t.Run("it had finished: a cause that is about no step", func(t *testing.T) {
		f, listing := newRacedFixture(t)
		v1, v2 := f.parkedOnOpsApprove(t)
		listing.atTheEndpointsApplyListing(func() {
			f.completeTaskOn(t, "opsApprove", "ollie")
			f.completeTaskOn(t, "salesApprove", "sasha")
		})
		reply, body := f.causesOverTheEndpoint(t, definition.MigrateInstancesRequest{
			SourceDefinitionID: v1, TargetDefinitionID: v2, DryRun: &apply,
			NodeMapping: map[string]string{"opsApprove": "salesApprove"},
		})
		reached(t, listing)
		requireCause(t, reply, body, "no_longer_running")
		if !strings.Contains(body, `"steps":[]`) {
			t.Errorf("a cause about no step should carry an empty list of steps: %s", body)
		}
	})

	// Nothing races in the two below, and each needs a second administrator:
	// the entry is on the approval's reply.
	t.Run("an approved skip landed it on a step the new version lacks", func(t *testing.T) {
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
		reply, body := f.causesOverTheEndpoint(t, definition.MigrateInstancesRequest{
			SourceDefinitionID: v1.String(), TargetDefinitionID: v2.String(), DryRun: &apply,
			NodeActions: map[string]servicecontracts.NodeAction{
				"firstApprove": {Kind: servicecontracts.NodeActionSkip, Reason: "the approvals were dropped"},
			},
		})
		requireCause(t, reply, body, "nowhere_to_land", causedStep{NodeID: "secondApprove", Name: "Second approval"})
		f.assertWaitingAt(t, v1, "secondApprove")
	})

	// The rest of the closed set, each in the window that gives it.
	t.Run("another run had already moved it", func(t *testing.T) {
		f, listing := newRacedFixture(t)
		first, second := f.parkedOnOpsApprove(t)
		chained := map[string]string{"opsApprove": "supervisorReview", "supervisorReview": "salesApprove"}
		listing.atTheEndpointsApplyListing(func() {
			if _, err := f.svc.ApplyInstanceMigration(f.ctx, uuidOf(t, first), uuidOf(t, second), chained, servicecontracts.WithActor("dita")); err != nil {
				t.Errorf("the first run: %v", err)
			}
		})
		reply, body := f.causesOverTheEndpoint(t, definition.MigrateInstancesRequest{
			SourceDefinitionID: first, TargetDefinitionID: second, DryRun: &apply, NodeMapping: chained,
		})
		reached(t, listing)
		requireCause(t, reply, body, "already_moved")
	})

	t.Run("it arrived after the migration was planned", func(t *testing.T) {
		f, listing := newRacedFixture(t)
		v1, err := f.svc.CreateDefinition(f.ctx, controlledTwoStep(f.project))
		if err != nil {
			t.Fatalf("deploy v1: %v", err)
		}
		if _, err := f.svc.StartProcess(f.ctx, f.project, "controlled-two-step", nil); err != nil {
			t.Fatalf("start: %v", err)
		}
		f.completeTaskOn(t, "opsApprove", "ada")
		v2, err := f.svc.DeployDefinition(f.ctx, controlledTwoStepWithout(f.project), false)
		if err != nil {
			t.Fatalf("stage v2: %v", err)
		}
		listing.atTheEndpointsApplyPlan(func() {
			if _, err := f.svc.StartProcess(f.ctx, f.project, "controlled-two-step", nil); err != nil {
				t.Errorf("start the late instance: %v", err)
			}
		})
		reply, body := f.causesOverTheEndpoint(t, definition.MigrateInstancesRequest{
			SourceDefinitionID: v1.String(), TargetDefinitionID: v2.String(), DryRun: &apply,
			NodeMapping: map[string]string{"opsApprove": "salesApprove"},
		})
		reached(t, listing)
		requireCause(t, reply, body, "not_planned_for")
	})

	t.Run("it reached a step the migration decides, after its decisions were taken", func(t *testing.T) {
		f, listing, locks := newLockRacedFixture(t)
		v1, v2 := f.waitingAtSupervisorReview(t)
		beforeTheRewriteLocks(listing, locks, func() { f.completeTaskOn(t, "supervisorReview", "sam") })
		result, err := f.applyWithApproval(t, v1, v2, nil, decideOps(servicecontracts.NodeActionHold, "ask the account manager")...)
		if err != nil {
			t.Fatalf("apply: %v", err)
		}
		if !locks.fired {
			t.Fatal("the hook never reached the rewrite's lock; the test is not exercising the window")
		}
		reply, body := written(t, result)
		requireCause(t, reply, body, "waiting_to_be_decided", causedStep{NodeID: "opsApprove", Name: "Operations approve"})
	})

	// The engine leaves no task behind on a step it has left, so the state is
	// made by hand, as the test of the rule itself makes it.
	t.Run("it has a task where the migration decides, and does not wait there", func(t *testing.T) {
		f := newFixture(t)
		v1, v2 := f.waitingAtSupervisorReview(t)
		if err := f.db.WithContext(f.ctx).Exec(`
			INSERT INTO tasks (id, created_at, updated_at, project_id, instance_id, node_id, name, type, status, assignee,
			                   candidate_users, candidate_groups, priority, variables)
			SELECT gen_random_uuid(), now(), now(), project_id, instance_id, 'opsApprove', 'Operations approve', type, 'claimed', 'ollie',
			       candidate_users, candidate_groups, priority, variables
			  FROM tasks WHERE instance_id = ? AND node_id = 'supervisorReview'`, f.onlyInstance(t).ID).Error; err != nil {
			t.Fatalf("leave a task on the operations approval: %v", err)
		}
		result, err := f.applyWithApproval(t, v1, v2, nil, skipOps("the role was eliminated")...)
		if err != nil {
			t.Fatalf("apply: %v", err)
		}
		reply, body := written(t, result)
		requireCause(t, reply, body, "left_where_nothing_decides", causedStep{NodeID: "opsApprove", Name: "Operations approve"})
	})
}

// written is what a run did as a route lists it: read back from the JSON the
// view writes, so that a result read at the service is held to the same shape
// as a reply.
func written(t *testing.T, result entities.MigrationResult) (causedReply, string) {
	t.Helper()
	body, err := json.Marshal(map[string]any{"passed_over": deviation.PassedOverViewsOf(result.PassedOver)})
	if err != nil {
		t.Fatalf("encode the result: %v", err)
	}
	var read causedReply
	if err := json.Unmarshal(body, &read); err != nil {
		t.Fatalf("decode the result: %v (%s)", err, body)
	}
	return read, string(body)
}
