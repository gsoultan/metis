package instancemigration

import (
	"fmt"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/gsoultan/metis/server/domains/entities"
	servicecontracts "github.com/gsoultan/metis/server/domains/services/contracts"
)

// What a migration does to the timers and the waiting events of an instance
// that has not moved since it was listed.
//
// unmoved_pins_test.go tells the instance, its tasks, its ledger, its incidents
// and its trail. It does not tell the two other kinds of row a migration
// re-points, and the change that stops a migration rewriting finished work
// reaches both: the loop that re-points an instance's timers and queued calls,
// and the landing check the plan and the rewrite share. For an instance whose
// timers and waiting events are all still live, nothing may differ.
//
// These were written, and passed, before that change was made.

// toldWithItsRows is a migration's plan and, once applied, what it left of the
// instance: where it stands, and every timer and waiting event it has, as the
// database holds them. An id is written as the place it first appeared in.
func (f *fixture) toldWithItsRows(t *testing.T, v1, v2 uuid.UUID, plan entities.MigrationPlan, result entities.MigrationResult, err error) string {
	t.Helper()
	var out strings.Builder
	fmt.Fprintf(&out, "plan: instances=%d refusals=%v warnings=%v\n", plan.Instances, plan.Refusals, plan.Warnings)
	for _, move := range plan.Moves {
		fmt.Fprintf(&out, "  move: %s→%s tokens=%d tasks=%d jobs=%d events=%d mapped=%v\n",
			move.From, move.To, move.Tokens, move.Tasks, move.Jobs, move.Events, move.Mapped)
	}
	fmt.Fprintf(&out, "error: %v\n", err)
	fmt.Fprintf(&out, "result: changed=%d passed_over=%d\n", result.Changed, len(result.PassedOver))
	instance := f.onlyInstance(t)
	fmt.Fprintf(&out, "instance: status=%s on=%s tokens=%v completed=%v\n",
		instance.Status, instance.Definition.ID, tokenNodes(instance), nodeIDs(instance.CompletedNodes))
	for _, task := range f.openTasks(t) {
		fmt.Fprintf(&out, "  open task: node=%s name=%q\n", task.NodeID(), task.Name)
	}
	for _, row := range f.jobRows(t, instance.ID) {
		fmt.Fprintf(&out, "  job: %s\n", row)
	}
	for _, row := range f.waitingEventRows(t, instance.ID) {
		fmt.Fprintf(&out, "  waiting event: %s\n", row)
	}
	return named(out.String(), map[string]string{v1.String(): "v1", v2.String(): "v2"})
}

// jobRows is an instance's timers and queued calls as the database holds
// them, oldest first: which version and step each names, and how it stands.
func (f *fixture) jobRows(t *testing.T, instanceID uuid.UUID) []string {
	t.Helper()
	return f.rowsAsText(t, `
		SELECT 'definition=' || definition_id || ' node=' || node_id || ' type=' || type || ' status=' || status
		  FROM jobs WHERE instance_id = ? AND deleted_at IS NULL ORDER BY created_at, id`, instanceID)
}

// waitingEventRows is the messages and signals an instance waits for, as the
// database holds them.
func (f *fixture) waitingEventRows(t *testing.T, instanceID uuid.UUID) []string {
	t.Helper()
	return f.rowsAsText(t, `
		SELECT 'node=' || node_id || ' type=' || type || ' event=' || event_name
		  FROM event_subscriptions WHERE instance_id = ? AND deleted_at IS NULL ORDER BY node_id, event_name`, instanceID)
}

// rowsAsText reads one text column, asked of the database and not of the code
// under test.
func (f *fixture) rowsAsText(t *testing.T, query string, args ...any) []string {
	t.Helper()
	var rows []string
	if err := f.db.WithContext(f.ctx).Raw(query, args...).Scan(&rows).Error; err != nil {
		t.Fatalf("read the rows: %v", err)
	}
	return rows
}

// coolingOff is start → a one-hour wait → a step → end, and start → the step →
// end when withWait is false: a version that dropped the wait.
func coolingOff(projectID uuid.UUID, waitID, workID string, withWait bool) *entities.ProcessDefinition {
	def := &entities.ProcessDefinition{
		Project: &entities.Project{ID: projectID},
		Key:     "cooling-off",
		Name:    "Cooling-off period",
		Nodes: []*entities.Node{
			{ID: "start", Type: entities.StartEvent, Outgoing: []string{"c1"}},
			{ID: workID, Type: entities.UserTask, Name: "Carry on", Assignee: "ada", Incoming: []string{"c2"}, Outgoing: []string{"c3"}},
			{ID: "end", Type: entities.EndEvent, Incoming: []string{"c3"}},
		},
		Flows: []*entities.SequenceFlow{
			{ID: "c3", SourceRef: workID, TargetRef: "end"},
		},
	}
	if !withWait {
		def.Nodes[1].Incoming = []string{"c1"}
		def.Flows = append(def.Flows, &entities.SequenceFlow{ID: "c1", SourceRef: "start", TargetRef: workID})
		return def
	}
	def.Nodes = append(def.Nodes, &entities.Node{
		ID: waitID, Name: "Cooling off", Type: entities.IntermediateCatchEvent,
		Properties: map[string]any{"timer_duration": "PT1H"},
		Incoming:   []string{"c1"}, Outgoing: []string{"c2"},
	})
	def.Flows = append(def.Flows,
		&entities.SequenceFlow{ID: "c1", SourceRef: "start", TargetRef: waitID},
		&entities.SequenceFlow{ID: "c2", SourceRef: waitID, TargetRef: workID})
	return def
}

// startedOn deploys v1, starts one instance of it and deploys v2.
func (f *fixture) startedOn(t *testing.T, first, second *entities.ProcessDefinition) (v1, v2 uuid.UUID) {
	t.Helper()
	v1, err := f.svc.CreateDefinition(f.ctx, first)
	if err != nil {
		t.Fatalf("deploy v1: %v", err)
	}
	if _, err := f.svc.StartProcess(f.ctx, f.project, first.Key, nil); err != nil {
		t.Fatalf("start: %v", err)
	}
	v2, err = f.svc.CreateDefinition(f.ctx, second)
	if err != nil {
		t.Fatalf("deploy v2: %v", err)
	}
	return v1, v2
}

// planAndApply is the plan of a migration that waits for a second
// administrator, and then its approved apply.
func (f *fixture) planAndApply(t *testing.T, v1, v2 uuid.UUID, mapping map[string]string, opts ...servicecontracts.MigrationOption) string {
	t.Helper()
	plan, err := f.svc.PlanInstanceMigration(f.ctx, v1, v2, mapping, opts...)
	if err != nil {
		t.Fatalf("plan: %v", err)
	}
	result, err := f.applyWithApproval(t, v1, v2, mapping, opts...)
	return f.toldWithItsRows(t, v1, v2, plan, result, err)
}

// planAndApplyOnOneCall is the plan of a migration one administrator applies,
// and then its apply.
func (f *fixture) planAndApplyOnOneCall(t *testing.T, v1, v2 uuid.UUID, mapping map[string]string, opts ...servicecontracts.MigrationOption) string {
	t.Helper()
	plan, err := f.svc.PlanInstanceMigration(f.ctx, v1, v2, mapping, opts...)
	if err != nil {
		t.Fatalf("plan: %v", err)
	}
	result, err := f.applyOnOneCall(t, v1, v2, mapping, opts...)
	return f.toldWithItsRows(t, v1, v2, plan, result, err)
}

// A timer that is still waiting moves with its step: the row names the new
// version and the step's new id, and is still pending.
func TestPinAMappingMovesATimerThatIsStillWaiting(t *testing.T) {
	f := newFixture(t)
	v1, v2 := f.startedOn(t, coolingOff(f.project, "wait", "oldWork", true), coolingOff(f.project, "pause", "newWork", true))
	told := f.planAndApplyOnOneCall(t, v1, v2, map[string]string{"wait": "pause", "oldWork": "newWork"}, servicecontracts.WithActor("dita"))
	assertToldAs(t, told, pinLiveTimer)
}

// A waiting event moves with its step too.
func TestPinAMappingMovesAWaitingEvent(t *testing.T) {
	f := newFixture(t)
	v1, v2 := f.startedOn(t, waitingOnMessage(f.project, "wait"), waitingOnMessage(f.project, "awaitErp"))
	told := f.planAndApplyOnOneCall(t, v1, v2, map[string]string{"wait": "awaitErp"}, servicecontracts.WithActor("dita"))
	assertToldAs(t, told, pinWaitingEvent)
}

// A skip of a wait the new version dropped advances the instance and moves it.
// The wait's timer cannot be deleted, so it is left behind, still pending and
// naming a step the new version does not have; when it comes due the engine
// finds no token for it and dismisses it. The instance must not be held back
// for that row: nothing but the passing of the hour would ever release it.
func TestPinASkipOfATimerWaitMovesTheInstanceAndItsTimerIsDismissedWhenDue(t *testing.T) {
	f := newFixture(t)
	v1, v2 := f.startedOn(t, coolingOff(f.project, "wait", "work", true), coolingOff(f.project, "wait", "work", false))
	told := f.planAndApply(t, v1, v2, nil,
		servicecontracts.WithNodeActions(map[string]servicecontracts.NodeAction{
			"wait": {Kind: servicecontracts.NodeActionSkip, Reason: "the cooling-off period was dropped"},
		}), servicecontracts.WithActor("dita"))
	assertToldAs(t, told, pinSkippedTimer)

	// The hour passes.
	instance := f.onlyInstance(t)
	if err := f.db.WithContext(f.ctx).Exec(
		`UPDATE jobs SET next_run_at = now() - interval '1 minute' WHERE instance_id = ?`, instance.ID).Error; err != nil {
		t.Fatalf("bring the timer due: %v", err)
	}
	if err := f.svc.ProcessPendingJobs(f.ctx); err != nil {
		t.Fatalf("process pending jobs: %v", err)
	}
	f.assertWaitingAt(t, v2, "work")
	if rows := f.jobRows(t, instance.ID); len(rows) != 1 || !strings.HasSuffix(rows[0], "status=completed") {
		t.Errorf("the timer left behind should have been dismissed when it came due: %v", rows)
	}
	f.runsToItsEnd(t)
}

const pinLiveTimer = `
plan: instances=1 refusals=[] warnings=[]
  move: wait→pause tokens=1 tasks=0 jobs=1 events=0 mapped=true
error: <nil>
result: changed=1 passed_over=0
instance: status=active on=v2 tokens=[pause] completed=[start]
  job: definition=v2 node=pause type=timer status=pending
`

const pinWaitingEvent = `
plan: instances=1 refusals=[] warnings=[]
  move: wait→awaitErp tokens=1 tasks=0 jobs=0 events=1 mapped=true
error: <nil>
result: changed=1 passed_over=0
instance: status=active on=v2 tokens=[awaitErp] completed=[start]
  waiting event: node=awaitErp type=message event=erp-replied
`

const pinSkippedTimer = `
plan: instances=1 refusals=[] warnings=[]
  move: wait→wait tokens=1 tasks=0 jobs=1 events=0 mapped=false
error: <nil>
result: changed=1 passed_over=0
instance: status=active on=v2 tokens=[work] completed=[start wait]
  open task: node=work name="Carry on"
  job: definition=v2 node=wait type=timer status=pending
`
