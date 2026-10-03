package instancemigration

import (
	"fmt"
	"regexp"
	"slices"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/gsoultan/metis/server/domains/entities"
	servicecontracts "github.com/gsoultan/metis/server/domains/services/contracts"
)

// What a migration does to an instance that has not moved since it was listed.
//
// The apply now reads each instance again before it decides its work, and asks
// under the instance's lock whether its work can land on the new version. Both
// are for the instance that moved between the listing and its lock. For one
// that did not, nothing may differ: the same result, the same ledger rows, the
// same trail entries, the same tasks.
//
// These were written, and passed, before that change was made. Each tells
// everything the migration left behind and compares it with what it was then.

// anyID finds the ids that differ from one run to the next.
var anyID = regexp.MustCompile(`[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}`)

// pinned is one migration over one project, told from before it ran.
type pinned struct {
	f *fixture
	// trail is how many entries each instance had before the migration, so
	// that only what the migration wrote is told in full.
	trail map[uuid.UUID]int
	order []uuid.UUID
}

// beforeMigrating notes where each instance's trail stands.
func (f *fixture) beforeMigrating(t *testing.T) pinned {
	t.Helper()
	instances, err := f.svc.ListInstances(f.ctx, f.project)
	if err != nil {
		t.Fatalf("list instances: %v", err)
	}
	p := pinned{f: f, trail: map[uuid.UUID]int{}}
	slices.SortFunc(instances, func(a, b entities.ProcessInstance) int { return a.CreatedAt.Compare(b.CreatedAt) })
	for _, instance := range instances {
		entries, err := f.svc.GetAuditLogs(f.ctx, instance.ID)
		if err != nil {
			t.Fatalf("read the trail: %v", err)
		}
		p.trail[instance.ID] = len(entries)
		p.order = append(p.order, instance.ID)
	}
	return p
}

// told is everything the migration left behind, one fact a line. An id is
// written as the place it first appeared in, so two runs tell the same story.
func (p pinned) told(t *testing.T, v1, v2 uuid.UUID, result entities.MigrationResult, err error) string {
	t.Helper()
	f := p.f
	var out strings.Builder
	fmt.Fprintf(&out, "error: %v\n", err)
	fmt.Fprintf(&out, "result: changed=%d passed_over=%d\n", result.Changed, len(result.PassedOver))
	tasks, listErr := f.svc.ListTasks(f.ctx, f.project)
	if listErr != nil {
		t.Fatalf("list tasks: %v", listErr)
	}
	slices.SortFunc(tasks, func(a, b entities.Task) int { return a.CreatedAt.Compare(b.CreatedAt) })
	for n, id := range p.order {
		instance, getErr := f.svc.GetInstance(f.ctx, id)
		if getErr != nil {
			t.Fatalf("read instance: %v", getErr)
		}
		fmt.Fprintf(&out, "instance %d: status=%s on=%s tokens=%v completed=%v\n",
			n+1, instance.Status, instance.Definition.ID, tokenNodes(instance), nodeIDs(instance.CompletedNodes))
		for _, task := range tasks {
			if task.Instance == nil || task.Instance.ID != id {
				continue
			}
			fmt.Fprintf(&out, "  task: node=%s name=%q status=%s assignee=%s owner=%s\n",
				task.NodeID(), task.Name, task.Status, username(task.Assignee), username(task.Owner))
		}
		for _, row := range f.ledger(t, id) {
			fmt.Fprintf(&out, "  row: kind=%s scope=%s origin=%s status=%s actor=%q reason=%q node=%s task=%s definition=%s run=%s entry=%s before=%v after=%v details=%v\n",
				row.Kind, row.Scope, row.Origin, row.Status, row.Actor, row.Reason, nodeOf(row.Node), taskOf(row.Task),
				row.Definition.ID, row.RunID, row.AuditEntryID, row.Before, row.After, row.Details)
		}
		incidents, incErr := f.svc.ListIncidents(f.ctx, id)
		if incErr != nil {
			t.Fatalf("list incidents: %v", incErr)
		}
		for _, incident := range incidents {
			fmt.Fprintf(&out, "  incident: id=%s node=%s status=%s error=%q\n", incident.ID, nodeOf(incident.Node), incident.Status, incident.Error)
		}
		entries, logErr := f.svc.GetAuditLogs(f.ctx, id)
		if logErr != nil {
			t.Fatalf("read the trail: %v", logErr)
		}
		for _, entry := range entries[p.trail[id]:] {
			fmt.Fprintf(&out, "  entry: id=%s type=%s node=%s message=%q narrative=%q data=%v\n",
				entry.ID, entry.Type, nodeOf(entry.Node), entry.Message, entry.Narrative, entry.Data)
		}
	}
	return named(out.String(), map[string]string{v1.String(): "v1", v2.String(): "v2"})
}

// named replaces every id by a name: the one given for it, or the order it
// first appears in.
func named(told string, known map[string]string) string {
	seen := map[string]string{}
	return anyID.ReplaceAllStringFunc(told, func(id string) string {
		if name, ok := known[id]; ok {
			return name
		}
		if _, ok := seen[id]; !ok {
			seen[id] = fmt.Sprintf("id%d", len(seen)+1)
		}
		return seen[id]
	})
}

func tokenNodes(instance entities.ProcessInstance) []string {
	nodes := make([]string, 0, len(instance.Tokens))
	for _, token := range instance.Tokens {
		nodes = append(nodes, nodeOf(token.Node))
	}
	slices.Sort(nodes)
	return nodes
}

func nodeIDs(nodes []*entities.Node) []string {
	ids := make([]string, 0, len(nodes))
	for _, node := range nodes {
		ids = append(ids, nodeOf(node))
	}
	return ids
}

func nodeOf(node *entities.Node) string {
	if node == nil {
		return "<none>"
	}
	if node.Name == "" {
		return node.ID
	}
	return node.ID + "/" + node.Name
}

func taskOf(task *entities.Task) string {
	if task == nil {
		return "<none>"
	}
	return task.ID.String()
}

func username(user *entities.User) string {
	if user == nil {
		return "<nobody>"
	}
	return user.Username
}

// assertToldAs fails, showing both, when what the migration left differs from
// what it left before the change.
func assertToldAs(t *testing.T, got, want string) {
	t.Helper()
	if strings.TrimSpace(got) != strings.TrimSpace(want) {
		t.Fatalf("an instance that had not moved is no longer migrated as it was.\n--- now ---\n%s\n--- before ---\n%s", got, want)
	}
}

// waitingAtSupervisorReview starts a quotation and leaves it at its first step,
// which both versions have.
func (f *fixture) waitingAtSupervisorReview(t *testing.T) (v1, v2 uuid.UUID) {
	t.Helper()
	v1, err := f.svc.CreateDefinition(f.ctx, quotationV1(f))
	if err != nil {
		t.Fatalf("deploy v1: %v", err)
	}
	if _, err := f.svc.StartProcess(f.ctx, f.project, "quotation", nil); err != nil {
		t.Fatalf("start a quotation: %v", err)
	}
	v2, err = f.svc.CreateDefinition(f.ctx, quotationV2(f))
	if err != nil {
		t.Fatalf("deploy v2: %v", err)
	}
	return v1, v2
}

// A migration that only moves work, of an instance on a step both versions
// have: the instance changes version and nothing else.
func TestPinAMappingOnlyMigrationOfAnUnmovedInstance(t *testing.T) {
	f := newFixture(t)
	v1, v2 := f.waitingAtSupervisorReview(t)
	before := f.beforeMigrating(t)
	result, err := f.svc.ApplyInstanceMigration(f.ctx, v1, v2, nil, servicecontracts.WithActor("dita"))
	assertToldAs(t, before.told(t, v1, v2, result, err), pinMappingOnly)
}

// The same with a mapping that re-points the work: the task is rebuilt from
// the step it lands on.
func TestPinARepointingMigrationOfAnUnmovedInstance(t *testing.T) {
	f := newFixture(t)
	first, second := f.parkedOnOpsApprove(t)
	v1, v2 := uuidOf(t, first), uuidOf(t, second)
	before := f.beforeMigrating(t)
	result, err := f.svc.ApplyInstanceMigration(f.ctx, v1, v2, map[string]string{"opsApprove": "salesApprove"},
		servicecontracts.WithActor("dita"))
	assertToldAs(t, before.told(t, v1, v2, result, err), pinRepointing)
}

// A skip, a cancel and a hold of an instance that is where the listing found
// it: the decision, its ledger row, its trail entry, and for a skip the move
// that follows.
func TestPinADecisionAboutAnUnmovedInstance(t *testing.T) {
	want := map[servicecontracts.NodeActionKind]string{
		servicecontracts.NodeActionSkip:   pinSkip,
		servicecontracts.NodeActionCancel: pinCancel,
		servicecontracts.NodeActionHold:   pinHold,
	}
	for _, kind := range everyNodeAction {
		t.Run(string(kind), func(t *testing.T) {
			f := newFixture(t)
			first, second := f.parkedOnOpsApprove(t)
			v1, v2 := uuidOf(t, first), uuidOf(t, second)
			before := f.beforeMigrating(t)
			result, err := f.svc.ApplyInstanceMigration(f.ctx, v1, v2, nil, decideOps(kind, "the role was eliminated")...)
			assertToldAs(t, before.told(t, v1, v2, result, err), want[kind])
		})
	}
}

// A skip in a run of two instances, of which only the first waits at the step
// being skipped: the second is moved with nothing decided about it.
func TestPinASkipLeavesAnInstanceElsewhereToBeMoved(t *testing.T) {
	f := newFixture(t)
	v1, err := f.svc.CreateDefinition(f.ctx, quotationV1(f))
	if err != nil {
		t.Fatalf("deploy v1: %v", err)
	}
	// The first goes on to the operations approval; the second stays at the
	// step before it.
	if _, err := f.svc.StartProcess(f.ctx, f.project, "quotation", nil); err != nil {
		t.Fatalf("start a quotation: %v", err)
	}
	f.completeTaskOn(t, "supervisorReview", "sam")
	if _, err := f.svc.StartProcess(f.ctx, f.project, "quotation", nil); err != nil {
		t.Fatalf("start a quotation: %v", err)
	}
	v2, err := f.svc.CreateDefinition(f.ctx, quotationV2(f))
	if err != nil {
		t.Fatalf("deploy v2: %v", err)
	}
	before := f.beforeMigrating(t)
	result, err := f.svc.ApplyInstanceMigration(f.ctx, v1, v2, nil, decideOps(servicecontracts.NodeActionSkip, "the role was eliminated")...)
	assertToldAs(t, before.told(t, v1, v2, result, err), pinSkipOfOneOfTwo)
}

const pinMappingOnly = `
error: <nil>
result: changed=1 passed_over=0
instance 1: status=active on=v2 tokens=[supervisorReview] completed=[start]
  task: node=supervisorReview name="Supervisor review" status=claimed assignee=sam owner=<nobody>
  entry: id=id1 type=instance_migrated node= message="migrated from version 1 to version 2" narrative="This instance was moved from version 1 to version 2 of \"quotation\" by a migration, authorised by dita." data=map[actor:dita process_key:quotation run_id:id2 source_version:1 target_version:2 task_moves:[] waived_controls:<nil>]
`

const pinRepointing = `
error: <nil>
result: changed=1 passed_over=0
instance 1: status=active on=v2 tokens=[salesApprove] completed=[start supervisorReview]
  task: node=supervisorReview name="Supervisor review" status=completed assignee=sam owner=<nobody>
  task: node=salesApprove name="Sales approve" status=claimed assignee=sasha owner=<nobody>
  entry: id=id1 type=instance_migrated node= message="migrated from version 1 to version 2" narrative="This instance was moved from version 1 to version 2 of \"quotation\" by a migration, authorised by dita. Work in progress was re-pointed: opsApprove→salesApprove." data=map[actor:dita process_key:quotation run_id:id2 source_version:1 target_version:2 task_moves:[opsApprove→salesApprove] waived_controls:<nil>]
`

const pinSkip = `
error: <nil>
result: changed=1 passed_over=0
instance 1: status=active on=v2 tokens=[salesApprove] completed=[start supervisorReview opsApprove]
  task: node=supervisorReview name="Supervisor review" status=completed assignee=sam owner=<nobody>
  task: node=opsApprove name="Operations approve" status=canceled assignee=ollie owner=<nobody>
  task: node=salesApprove name="Sales approve" status=claimed assignee=sasha owner=<nobody>
  row: kind=waive scope=task origin=migration status=applied actor="dita" reason="the role was eliminated" node=opsApprove/Operations approve task=id1 definition=v1 run=id2 entry=id3 before=map[tasks:map[id1:map[assignee:ollie status:claimed]]] after=map[tasks:map[id1:map[status:canceled]]] details=map[withdrawn:1]
  entry: id=id4 type=task_created node=salesApprove/Sales approve message="" narrative="Task \"Sales approve\" became available" data=map[actor:]
  entry: id=id3 type=node_skipped node=opsApprove message="skip opsApprove during migration" narrative="\"opsApprove\" was skipped without being performed, by dita, when \"quotation\" moved from version 1 to version 2. Reason: the role was eliminated." data=map[action:skip actor:dita deviation_id:id5 node_id:opsApprove reason:the role was eliminated run_id:id2]
  entry: id=id6 type=instance_migrated node= message="migrated from version 1 to version 2" narrative="This instance was moved from version 1 to version 2 of \"quotation\" by a migration, authorised by dita." data=map[actor:dita process_key:quotation run_id:id2 source_version:1 target_version:2 task_moves:[] waived_controls:<nil>]
`

const pinCancel = `
error: <nil>
result: changed=1 passed_over=0
instance 1: status=cancelled on=v1 tokens=[] completed=[start supervisorReview]
  task: node=supervisorReview name="Supervisor review" status=completed assignee=sam owner=<nobody>
  task: node=opsApprove name="Operations approve" status=canceled assignee=ollie owner=<nobody>
  row: kind=cancel scope=instance origin=migration status=applied actor="dita" reason="the role was eliminated" node=opsApprove/Operations approve task=id1 definition=v1 run=id2 entry=id3 before=map[instance:map[status:active] tasks:map[id1:map[assignee:ollie status:claimed]]] after=map[instance:map[status:cancelled] tasks:map[id1:map[status:canceled]]] details=map[withdrawn:1]
  entry: id=id3 type=instance_cancelled node=opsApprove message="cancel opsApprove during migration" narrative="This instance was ended at \"opsApprove\" by dita, rather than moved to version 2 of \"quotation\". Reason: the role was eliminated." data=map[action:cancel actor:dita deviation_id:id4 node_id:opsApprove reason:the role was eliminated run_id:id2]
`

const pinHold = `
error: <nil>
result: changed=1 passed_over=0
instance 1: status=active on=v1 tokens=[opsApprove] completed=[start supervisorReview]
  task: node=supervisorReview name="Supervisor review" status=completed assignee=sam owner=<nobody>
  task: node=opsApprove name="Operations approve" status=claimed assignee=ollie owner=<nobody>
  row: kind=hold scope=instance origin=migration status=applied actor="dita" reason="the role was eliminated" node=opsApprove/Operations approve task=<none> definition=v1 run=id1 entry=id2 before=map[] after=map[incident:map[id:id3 status:open]] details=map[]
  incident: id=id3 node=opsApprove status=open error="held out of the migration from version 1 to version 2 of \"quotation\" by dita: the role was eliminated"
  entry: id=id2 type=instance_held node=opsApprove message="hold opsApprove during migration" narrative="This instance was held at \"opsApprove\" by dita rather than moved to version 2 of \"quotation\", and raised as an incident for somebody to decide. Reason: the role was eliminated." data=map[action:hold actor:dita deviation_id:id4 node_id:opsApprove reason:the role was eliminated run_id:id1]
`

const pinSkipOfOneOfTwo = `
error: <nil>
result: changed=2 passed_over=0
instance 1: status=active on=v2 tokens=[salesApprove] completed=[start supervisorReview opsApprove]
  task: node=supervisorReview name="Supervisor review" status=completed assignee=sam owner=<nobody>
  task: node=opsApprove name="Operations approve" status=canceled assignee=ollie owner=<nobody>
  task: node=salesApprove name="Sales approve" status=claimed assignee=sasha owner=<nobody>
  row: kind=waive scope=task origin=migration status=applied actor="dita" reason="the role was eliminated" node=opsApprove/Operations approve task=id1 definition=v1 run=id2 entry=id3 before=map[tasks:map[id1:map[assignee:ollie status:claimed]]] after=map[tasks:map[id1:map[status:canceled]]] details=map[withdrawn:1]
  entry: id=id4 type=task_created node=salesApprove/Sales approve message="" narrative="Task \"Sales approve\" became available" data=map[actor:]
  entry: id=id3 type=node_skipped node=opsApprove message="skip opsApprove during migration" narrative="\"opsApprove\" was skipped without being performed, by dita, when \"quotation\" moved from version 1 to version 2. Reason: the role was eliminated." data=map[action:skip actor:dita deviation_id:id5 node_id:opsApprove reason:the role was eliminated run_id:id2]
  entry: id=id6 type=instance_migrated node= message="migrated from version 1 to version 2" narrative="This instance was moved from version 1 to version 2 of \"quotation\" by a migration, authorised by dita." data=map[actor:dita process_key:quotation run_id:id2 source_version:1 target_version:2 task_moves:[] waived_controls:<nil>]
instance 2: status=active on=v2 tokens=[supervisorReview] completed=[start]
  task: node=supervisorReview name="Supervisor review" status=claimed assignee=sam owner=<nobody>
  entry: id=id7 type=instance_migrated node= message="migrated from version 1 to version 2" narrative="This instance was moved from version 1 to version 2 of \"quotation\" by a migration, authorised by dita." data=map[actor:dita process_key:quotation run_id:id2 source_version:1 target_version:2 task_moves:[] waived_controls:<nil>]
`
