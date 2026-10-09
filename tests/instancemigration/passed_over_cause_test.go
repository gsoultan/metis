package instancemigration

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

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
// reads it. Steps and the two counts are pointers so that one left out can be
// told from an empty list or a nought.
type causedReply struct {
	PassedOver []struct {
		InstanceID string        `json:"instance_id"`
		Cause      string        `json:"cause"`
		Steps      *[]causedStep `json:"steps"`
		StepsInAll *int          `json:"steps_in_all"`
		Reason     string        `json:"reason"`
	} `json:"passed_over"`
	PassedOverInAll *int `json:"passed_over_in_all"`
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
// that names each step by that name and never by its id. Those are all its
// steps, and it is all the reply passed over, and each count says so.
func requireCause(t *testing.T, reply causedReply, body, cause string, steps ...causedStep) {
	t.Helper()
	requireCauseOf(t, reply, body, cause, len(steps), steps...)
}

// requireCauseOf is requireCause for a cause about more steps than it lists:
// inAll is how many there were.
func requireCauseOf(t *testing.T, reply causedReply, body, cause string, inAll int, steps ...causedStep) {
	t.Helper()
	if len(reply.PassedOver) != 1 {
		t.Fatalf("passed_over is %s, want the one instance", body)
	}
	if reply.PassedOverInAll == nil || *reply.PassedOverInAll != 1 {
		t.Fatalf("passed_over_in_all is %v beside one instance passed over: %s", reply.PassedOverInAll, body)
	}
	if got := reply.PassedOver[0].StepsInAll; got == nil || *got != inAll {
		t.Fatalf("steps_in_all is %v, want %d: %s", got, inAll, body)
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
		if !strings.Contains(body, `"steps":[],"steps_in_all":0`) {
			t.Errorf("a cause about no step should carry an empty list of steps, and say there were none: %s", body)
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

// fannedOutKey is the process of fannedOut.
const fannedOutKey = "fanned-out"

// fannedOut is start → first → twenty-five checks side by side → sign → end,
// or — without them — start → sign → end. Skipping the first step puts an
// instance on all twenty-five at once, and the new version has none of them.
// The second check has a name longer than anything that shows a name keeps.
func fannedOut(projectID uuid.UUID, withChecks bool) *entities.ProcessDefinition {
	def := &entities.ProcessDefinition{
		Project: &entities.Project{ID: projectID}, Key: fannedOutKey, Name: "Fanned out",
		Nodes: []*entities.Node{
			{ID: "start", Type: entities.StartEvent, Outgoing: []string{"s"}},
			{ID: "sign", Name: "Sign", Type: entities.UserTask, Assignee: "sasha", Incoming: []string{"s"}, Outgoing: []string{"e"}},
			{ID: "end", Type: entities.EndEvent, Incoming: []string{"e"}},
		},
		Flows: []*entities.SequenceFlow{{ID: "e", SourceRef: "sign", TargetRef: "end"}},
	}
	if !withChecks {
		def.Flows = append(def.Flows, &entities.SequenceFlow{ID: "s", SourceRef: "start", TargetRef: "sign"})
		return def
	}
	def.Nodes[1].Incoming = []string{"j"}
	split := &entities.Node{ID: "split", Type: entities.ParallelGateway, Incoming: []string{"f"}}
	join := &entities.Node{ID: "join", Type: entities.ParallelGateway, Outgoing: []string{"j"}}
	def.Nodes = append(def.Nodes,
		&entities.Node{ID: "first", Name: "First", Type: entities.UserTask, Assignee: "fay", Incoming: []string{"s"}, Outgoing: []string{"f"}},
		split, join)
	def.Flows = append(def.Flows,
		&entities.SequenceFlow{ID: "s", SourceRef: "start", TargetRef: "first"},
		&entities.SequenceFlow{ID: "f", SourceRef: "first", TargetRef: "split"},
		&entities.SequenceFlow{ID: "j", SourceRef: "join", TargetRef: "sign"})
	for n := 1; n <= 25; n++ {
		id, in, out := fmt.Sprintf("check%02d", n), fmt.Sprintf("in%02d", n), fmt.Sprintf("out%02d", n)
		name := fmt.Sprintf("Check %02d", n)
		if n == 2 {
			name = longCheckName
		}
		split.Outgoing, join.Incoming = append(split.Outgoing, in), append(join.Incoming, out)
		def.Nodes = append(def.Nodes, &entities.Node{ID: id, Name: name, Type: entities.UserTask, Assignee: "cyd", Incoming: []string{in}, Outgoing: []string{out}})
		def.Flows = append(def.Flows,
			&entities.SequenceFlow{ID: in, SourceRef: "split", TargetRef: id},
			&entities.SequenceFlow{ID: out, SourceRef: id, TargetRef: "join"})
	}
	return def
}

// longCheckName is a step's name of three hundred characters, none of them
// one byte long.
var longCheckName = strings.Repeat("é", 300)

// Every list shown to a person has a size and says how much it leaves out.
// An instance passed over for holding work on twenty-five steps is listed
// with the first ten of them — in the order they had, a name of three
// hundred characters cut as the ledger cuts one — and with how many there
// were. The sentence beside them names the same ten and counts the rest.
func TestAPassedOverInstanceListsTenOfItsStepsAndSaysHowManyThereWere(t *testing.T) {
	f := newFixture(t)
	v1, err := f.svc.CreateDefinition(f.ctx, fannedOut(f.project, true))
	if err != nil {
		t.Fatalf("deploy v1: %v", err)
	}
	if _, err := f.svc.StartProcess(f.ctx, f.project, fannedOutKey, nil); err != nil {
		t.Fatalf("start: %v", err)
	}
	v2, err := f.svc.CreateDefinition(f.ctx, fannedOut(f.project, false))
	if err != nil {
		t.Fatalf("deploy v2: %v", err)
	}
	apply := false
	reply, body := f.causesOverTheEndpoint(t, definition.MigrateInstancesRequest{
		SourceDefinitionID: v1.String(), TargetDefinitionID: v2.String(), DryRun: &apply,
		NodeActions: map[string]servicecontracts.NodeAction{
			"first": {Kind: servicecontracts.NodeActionSkip, Reason: "the first step was dropped"},
		},
	})
	want := make([]causedStep, 0, 10)
	for n := 1; n <= 10; n++ {
		step := causedStep{NodeID: fmt.Sprintf("check%02d", n), Name: fmt.Sprintf("Check %02d", n)}
		if n == 2 {
			step.Name = strings.Repeat("é", 255)
		}
		want = append(want, step)
	}
	requireCauseOf(t, reply, body, "nowhere_to_land", 25, want...)
	reason := reply.PassedOver[0].Reason
	// The sentence is cut where the list is: the first ten steps, a name no
	// longer than the list gives it, and how many were left out. It named
	// every step in full until it was bounded too, and this asserted that.
	if !strings.Contains(reason, `"Check 01", "`+strings.Repeat("é", 255)+`", "Check 03"`) || !strings.Contains(reason, `"Check 10" and 15 more`) ||
		strings.Contains(reason, longCheckName) || strings.Contains(reason, `"Check 11"`) {
		t.Errorf("the sentence beside the list is not cut where the list is: %.200s…", reason)
	}
}

// A definition is somebody's input, and nothing stops a sub-process in it
// from naming itself as its parent: it deploys, and an instance runs on it.
// Naming the step such an instance was passed over at goes round nothing.
func TestAnInstancePassedOverInAVersionWhoseSubProcessIsItsOwnParentIsNamedItsStep(t *testing.T) {
	f, listing := newRacedFixture(t)
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
	if _, err := f.svc.StartProcess(f.ctx, f.project, "looped-inside", nil); err != nil {
		t.Fatalf("start: %v", err)
	}
	v2, err := f.svc.CreateDefinition(f.ctx, looped())
	if err != nil {
		t.Fatalf("deploy v2: %v", err)
	}
	listing.atTheEndpointsApplyListing(func() { f.completeTaskOn(t, "check", "cyd") })
	apply := false
	type answer struct {
		reply causedReply
		body  string
	}
	answered := make(chan answer, 1)
	go func() {
		reply, body := f.causesOverTheEndpoint(t, definition.MigrateInstancesRequest{
			SourceDefinitionID: v1.String(), TargetDefinitionID: v2.String(), DryRun: &apply,
			NodeActions: map[string]servicecontracts.NodeAction{
				"check": {Kind: servicecontracts.NodeActionCancel, Reason: "the request was withdrawn"},
			},
		})
		answered <- answer{reply, body}
	}()
	select {
	case got := <-answered:
		reached(t, listing)
		requireCause(t, got.reply, got.body, "left_the_step", causedStep{NodeID: "check", Name: "Check the request"})
	case <-time.After(20 * time.Second):
		t.Fatal("the migration had not answered after twenty seconds")
	}
}

// An instance passed over because two of its counters would land on one step
// is told that cause by a run, not only by the sentence's own test.
//
// The plan refuses a mapping that merges two counters it can see. Here one
// review of two is done when the plan is made — one counter, on the review —
// and the other branch arrives at the join after the run has listed the
// instance: a second counter, on the join, which the mapping puts onto the
// same step.
func TestAnInstanceWhoseCountersWouldMergeIsPassedOverForThatCause(t *testing.T) {
	f, listing := newRacedFixture(t)
	v1, err := f.svc.CreateDefinition(f.ctx, twoCounters(f.project))
	if err != nil {
		t.Fatalf("deploy two-counters: %v", err)
	}
	if _, err := f.svc.StartProcess(f.ctx, f.project, "two-counters", map[string]any{"items": []any{"x", "y"}}); err != nil {
		t.Fatalf("start an instance: %v", err)
	}
	f.completeTaskOn(t, "review", "ada")
	v2, err := f.svc.CreateDefinition(f.ctx, oneNode(f.project))
	if err != nil {
		t.Fatalf("deploy v2: %v", err)
	}
	mapping := map[string]string{"review": "gate", "sign": "gate", "join": "gate"}
	if plan, err := f.svc.PlanInstanceMigration(f.ctx, v1, v2, mapping); err != nil || !plan.Applicable() {
		t.Fatalf("the plan, made while one counter is held: %+v %v, want it applicable", plan.Refusals, err)
	}
	listing.atTheEndpointsApplyListing(func() { f.completeTaskOn(t, "sign", "bo") })
	apply := false
	reply, body := f.causesOverTheEndpoint(t, definition.MigrateInstancesRequest{
		SourceDefinitionID: v1.String(), TargetDefinitionID: v2.String(), DryRun: &apply, NodeMapping: mapping,
	})
	reached(t, listing)
	requireCause(t, reply, body, "counters_would_merge")
	if reason := reply.PassedOver[0].Reason; !strings.Contains(reason, "their progress cannot be added together") {
		t.Errorf("the sentence beside the cause: %q", reason)
	}
	// Left as its lock found it: on the version it runs, a review still to do
	// and the other branch waiting at the join.
	instance := f.onlyInstance(t)
	if instance.Definition == nil || instance.Definition.ID != v1 || len(instance.Tokens) != 2 {
		t.Errorf("the instance is on %v holding %d token(s), want the version it ran, at the review and the join", instance.Definition, len(instance.Tokens))
	}
	f.assertNoMigrationEntries(t, instance.ID)
}

// written is what a run did as a route lists it: read back from the JSON the
// view writes, so that a result read at the service is held to the same shape
// as a reply.
func written(t *testing.T, result entities.MigrationResult) (causedReply, string) {
	t.Helper()
	body, err := json.Marshal(map[string]any{"passed_over": deviation.PassedOverViewsOf(result.PassedOver), "passed_over_in_all": len(result.PassedOver)})
	if err != nil {
		t.Fatalf("encode the result: %v", err)
	}
	var read causedReply
	if err := json.Unmarshal(body, &read); err != nil {
		t.Fatalf("decode the result: %v (%s)", err, body)
	}
	return read, string(body)
}
