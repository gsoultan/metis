package instancemigration

import (
	"fmt"
	"maps"
	"os"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/gsoultan/metis/server/domains/entities"
	observersimpl "github.com/gsoultan/metis/server/domains/observers/impl"
	servicecontracts "github.com/gsoultan/metis/server/domains/services/contracts"
	"github.com/gsoultan/metis/server/domains/services/impl"
	"github.com/gsoultan/metis/server/repositories"
	"github.com/gsoultan/metis/server/repositories/models"
)

// The query docs/upgrading.md gives an operator to find the tasks 0.4.0
// reopened, run as the document has it over rows the server wrote.
//
// Its first version read the steps a migration re-pointed, and who completed a
// task, out of audit_logs.data. The repository encrypts that column, in 0.4.0
// as now, so on a real database both were NULL and the query listed nothing:
// an operator with reopened tasks would have been told there were none. It had
// been checked only over rows written by hand, in the clear.
//
// So every row here is written through the service or the repository. What
// 0.4.0's rewrite did is done again the way it did it: each task on a mapped
// step rebuilt from the step it lands on and written with Task().Update,
// whatever its status, and the instance_migrated entry written through the
// audit writer with 0.4.0's sentence and data.

// reopenedTaskQuery is the query as the document prints it.
func reopenedTaskQuery(t *testing.T) string {
	t.Helper()
	doc, err := os.ReadFile("../../docs/upgrading.md")
	if err != nil {
		t.Fatalf("read the upgrading guide: %v", err)
	}
	_, section, found := strings.Cut(string(doc), "\n## A task a migration reopened\n")
	if !found {
		t.Fatal("the upgrading guide has no section on a task a migration reopened")
	}
	_, after, found := strings.Cut(section, "```sql\n")
	query, _, closed := strings.Cut(after, "\n```")
	if !found || !closed {
		t.Fatal("the section has no query")
	}
	return query
}

// listedAsReopened runs the document's query and returns its rows by task id.
func (f *fixture) listedAsReopened(t *testing.T) map[string]map[string]string {
	t.Helper()
	sqlDB, err := f.db.DB()
	if err != nil {
		t.Fatalf("reach the database: %v", err)
	}
	// Not through a query builder: the text has question marks of its own.
	rows, err := sqlDB.QueryContext(f.ctx, reopenedTaskQuery(t))
	if err != nil {
		t.Fatalf("the document's query does not run: %v", err)
	}
	defer rows.Close()
	columns, err := rows.Columns()
	if err != nil {
		t.Fatalf("read the columns: %v", err)
	}
	listed := map[string]map[string]string{}
	for rows.Next() {
		values := make([]any, len(columns))
		for i := range values {
			values[i] = new(any)
		}
		if err := rows.Scan(values...); err != nil {
			t.Fatalf("read a row: %v", err)
		}
		row := map[string]string{}
		for i, column := range columns {
			switch value := (*values[i].(*any)).(type) {
			case nil:
				row[column] = "<null>"
			case []byte:
				row[column] = string(value)
			default:
				row[column] = fmt.Sprint(value)
			}
		}
		listed[row["task_id"]] = row
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("read the rows: %v", err)
	}
	return listed
}

// migrateAs040 moves an instance the way 0.4.0's rewrite did, through the
// repository: every task on a mapped step is rebuilt from the step it lands
// on and offered again, whatever its status, and the entry lists every step
// that had a task as work re-pointed.
func migrateAs040(t *testing.T, f *fixture, repo repositories.Repository, instanceID, targetID uuid.UUID, mapping map[string]string) {
	t.Helper()
	instance, err := repo.Process().Get(f.ctx, instanceID)
	if err != nil {
		t.Fatalf("read the instance: %v", err)
	}
	source, err := repo.Definition().Get(f.ctx, uuid.UUID(instance.DefinitionID))
	if err != nil {
		t.Fatalf("read the source version: %v", err)
	}
	target, err := repo.Definition().Get(f.ctx, targetID)
	if err != nil {
		t.Fatalf("read the target version: %v", err)
	}
	landing := map[string]models.FlowNode{}
	for _, node := range target.Nodes {
		landing[node.ID] = node
	}
	mapped := func(id string) string {
		if to, ok := mapping[id]; ok {
			return to
		}
		return id
	}

	instance.DefinitionID = models.UUID(targetID)
	for i := range instance.Tokens {
		instance.Tokens[i].NodeID = mapped(instance.Tokens[i].NodeID)
	}
	for i := range instance.CompletedNodes {
		instance.CompletedNodes[i] = mapped(instance.CompletedNodes[i])
	}
	if err := repo.Process().Update(f.ctx, instance); err != nil {
		t.Fatalf("move the instance: %v", err)
	}

	tasks, err := repo.Task().ListByInstance(f.ctx, instanceID)
	if err != nil {
		t.Fatalf("list the tasks: %v", err)
	}
	var pairs []string
	for _, task := range tasks {
		to := mapped(task.NodeID)
		if to == task.NodeID {
			continue
		}
		if pair := task.NodeID + "→" + to; !slices.Contains(pairs, pair) {
			pairs = append(pairs, pair)
		}
		node := landing[to]
		task.NodeID, task.Name, task.Description, task.Type = node.ID, node.Name, node.Documentation, node.Type
		task.Priority, task.FormKey = node.Priority, node.FormKey
		task.CandidateUsers, task.CandidateGroups = slices.Clone(node.CandidateUsers), slices.Clone(node.CandidateGroups)
		task.Owner, task.DelegationState = "", ""
		task.Assignee, task.Status = node.Assignee, models.TaskClaimed
		if node.Assignee == "" {
			task.Status = models.TaskUnclaimed
		}
		if err := repo.Task().Update(f.ctx, task); err != nil {
			t.Fatalf("rebuild a task as 0.4.0 did: %v", err)
		}
	}
	slices.Sort(pairs)

	narrative := fmt.Sprintf("This instance was moved from version %d to version %d of %q by a migration, authorised by %s.",
		source.Version, target.Version, target.Key, "dita")
	if len(pairs) > 0 {
		narrative += fmt.Sprintf(" Work in progress was re-pointed: %s.", strings.Join(pairs, ", "))
	}
	if err := impl.NewAuditWriter(repo.Audit()).RecordEvent(f.ctx, entities.AuditEntry{
		ID:        uuid.New(),
		Type:      impl.EventInstanceMigrated,
		Message:   fmt.Sprintf("migrated from version %d to version %d", source.Version, target.Version),
		Narrative: narrative,
		Timestamp: time.Now(),
		Data: map[string]any{
			"source_version": source.Version, "target_version": target.Version, "process_key": target.Key,
			"task_moves": pairs, "run_id": uuid.NewString(), "actor": "dita",
		},
		Project:  &entities.Project{ID: uuid.UUID(instance.ProjectID)},
		Instance: &entities.ProcessInstance{ID: instanceID},
	}); err != nil {
		t.Fatalf("write the migration's entry: %v", err)
	}
}

// taskOn is the id of the instance's one task on a step.
func (f *fixture) taskOn(t *testing.T, instanceID uuid.UUID, nodeID string) string {
	t.Helper()
	ids := f.rowsAsText(t, `SELECT id::text FROM tasks WHERE instance_id = ? AND node_id = ? AND deleted_at IS NULL`, instanceID, nodeID)
	if len(ids) != 1 {
		t.Fatalf("%d task(s) on %q, want one", len(ids), nodeID)
	}
	return ids[0]
}

// putBack is the document's UPDATE with what the query listed filled in: the
// step's old id for a redirect, the id it has now for a rename.
func (f *fixture) putBack(t *testing.T, row map[string]string) {
	t.Helper()
	node := row["step_now"]
	if row["mapping_was"] == "redirect" {
		node = row["step_it_was_on"]
	}
	// Who a withdrawn task had been with is not in the trail: its assignee is
	// left as it is.
	changed := f.db.WithContext(f.ctx).Exec(`
		UPDATE tasks SET status = ?, node_id = ?, name = ?, assignee = CASE WHEN ? = '<null>' THEN assignee ELSE ? END
		 WHERE id = ? AND status IN ('unclaimed', 'claimed', 'delegated', 'escalated')`,
		row["it_was"], node, row["name_it_had"], row["by_whom"], row["by_whom"], row["task_id"])
	if changed.Error != nil || changed.RowsAffected != 1 {
		t.Fatalf("put the task back: %d row(s), err %v", changed.RowsAffected, changed.Error)
	}
}

// TestTheUpgradingQueryFindsTheTasksAnEarlierReleaseReopened.
//
// Root cause of the query that found nothing: it read an encrypted column.
func TestTheUpgradingQueryFindsTheTasksAnEarlierReleaseReopened(t *testing.T) {
	var repo repositories.Repository
	f := newFixtureOver(t, func(real repositories.Repository) repositories.Repository {
		repo = real
		return real
	})
	// As the server is wired: a task the engine withdraws is entered in the
	// trail by this observer.
	f.dispatcher.Register(observersimpl.NewAuditLogObserver(repo.Audit()))

	// A redirect: the operations approval was given, and 0.4.0 reopened it as
	// a second sales approval.
	quotation, err := f.svc.CreateDefinition(f.ctx, quotationV1(f))
	if err != nil {
		t.Fatalf("deploy the quotation: %v", err)
	}
	start := func(key string) uuid.UUID {
		id, startErr := f.svc.StartProcess(f.ctx, f.project, key, nil)
		if startErr != nil {
			t.Fatalf("start %s: %v", key, startErr)
		}
		return id
	}
	complete := func(instanceID uuid.UUID, nodeID, who string) {
		if err := f.svc.CompleteTask(f.ctx, uuid.MustParse(f.taskOn(t, instanceID, nodeID)), who, nil); err != nil {
			t.Fatalf("complete %s: %v", nodeID, err)
		}
	}
	redirected := start("quotation")
	complete(redirected, "supervisorReview", "sam")
	complete(redirected, "opsApprove", "ollie")
	givenByOllie := f.taskOn(t, redirected, "opsApprove")
	genuineSales := f.taskOn(t, redirected, "salesApprove")
	// One this release migrates the same way, which must not be listed, and
	// one whose operations approval was still open, moved and never completed.
	migratedNow := start("quotation")
	complete(migratedNow, "supervisorReview", "sam")
	complete(migratedNow, "opsApprove", "ollie")
	neverCompleted := start("quotation")
	complete(neverCompleted, "supervisorReview", "sam")
	movedOpen := f.taskOn(t, neverCompleted, "opsApprove")
	quotation2, err := f.svc.CreateDefinition(f.ctx, quotationV2(f))
	if err != nil {
		t.Fatalf("deploy the second quotation: %v", err)
	}
	redirect := map[string]string{"opsApprove": "salesApprove"}
	if _, err := f.svc.ApplyInstanceMigration(f.ctx, quotation, quotation2, redirect,
		servicecontracts.WithInstances(migratedNow), servicecontracts.WithActor("dita")); err != nil {
		t.Fatalf("migrate one as this release does: %v", err)
	}
	before := f.finishedTasks(t, redirected)
	migrateAs040(t, f, repo, redirected, quotation2, redirect)
	migrateAs040(t, f, repo, neverCompleted, quotation2, redirect)

	// A rename, with a task that was completed and one a boundary event
	// withdrew: 0.4.0 reopened both on the steps' new ids.
	renamedFrom, renamedTo := f.startedOn(t,
		everyKindOfRow(f.project, "draft", "wait", "check", "approve"),
		everyKindOfRow(f.project, "write", "pause", "verify", "signOff"))
	_ = renamedFrom
	var renamed uuid.UUID
	for _, id := range f.rowsAsText(t, `SELECT id::text FROM process_instances WHERE definition_id = ?`, renamedFrom) {
		renamed = uuid.MustParse(id)
	}
	complete(renamed, "draft", "sam")
	if err := f.db.WithContext(f.ctx).Exec(
		`UPDATE jobs SET next_run_at = now() - interval '1 minute' WHERE instance_id = ?`, renamed).Error; err != nil {
		t.Fatalf("bring the timer due: %v", err)
	}
	if err := f.svc.ProcessPendingJobs(f.ctx); err != nil {
		t.Fatalf("process pending jobs: %v", err)
	}
	if err := f.svc.SendMessage(f.ctx, f.project, "request-recalled", "", nil); err != nil {
		t.Fatalf("recall the request: %v", err)
	}
	draftedBySam, withdrawnCheck, openApproval := f.taskOn(t, renamed, "draft"), f.taskOn(t, renamed, "check"), f.taskOn(t, renamed, "approve")
	beforeRename := f.finishedTasks(t, renamed)
	migrateAs040(t, f, repo, renamed, renamedTo,
		map[string]string{"draft": "write", "wait": "pause", "check": "verify", "approve": "signOff"})

	// The trail's data is not readable in SQL, which is why the query must
	// not read it.
	if kinds := f.rowsAsText(t, `SELECT DISTINCT jsonb_typeof(data::jsonb) FROM audit_logs WHERE type = 'instance_migrated'`); !slices.Equal(kinds, []string{"string"}) {
		t.Fatalf("the migration entries' data is %v in the database; the test is not exercising a sealed column", kinds)
	}

	listed := f.listedAsReopened(t)
	for _, id := range slices.Sorted(maps.Keys(listed)) {
		t.Logf("listed: %v", listed[id])
	}
	want := map[string]map[string]string{
		givenByOllie: {"step_now": "salesApprove", "step_it_was_on": "opsApprove", "name_it_had": "Operations approve",
			"it_was": "completed", "by_whom": "ollie", "mapping_was": "redirect"},
		draftedBySam: {"step_now": "write", "step_it_was_on": "draft", "name_it_had": "Draft the request (draft)",
			"it_was": "completed", "by_whom": "sam", "mapping_was": "rename"},
		withdrawnCheck: {"step_now": "verify", "step_it_was_on": "check", "name_it_had": "Check the request (check)",
			"it_was": "canceled", "by_whom": "<null>", "mapping_was": "rename"},
	}
	for id, columns := range want {
		row, found := listed[id]
		if !found {
			t.Errorf("a task 0.4.0 reopened is not listed: %v", columns)
			continue
		}
		for column, value := range columns {
			if row[column] != value {
				t.Errorf("task %s: %s is %q, want %q", id, column, row[column], value)
			}
		}
	}
	for name, id := range map[string]string{
		"the genuine sales approval, created after the operations approval was given": genuineSales,
		"an open operations approval 0.4.0 moved, never completed":                    movedOpen,
		"the open approval of the renamed process":                                    openApproval,
	} {
		if _, found := listed[id]; found {
			t.Errorf("listed, and it should not be: %s", name)
		}
	}
	if len(listed) != len(want) {
		t.Errorf("%d task(s) are listed, want the %d that were reopened (an instance this release migrated has none)", len(listed), len(want))
	}

	// Put back with the document's UPDATE, each is the row it was — on the
	// step's old id after a redirect, on its new one after a rename — and the
	// query lists nothing.
	for id := range want {
		if row, found := listed[id]; found {
			f.putBack(t, row)
		}
	}
	assertRowsUntouched(t, "finished task put back after a redirect", before, f.finishedTasks(t, redirected))
	for id, told := range beforeRename {
		told = strings.Replace(told, "node=draft ", "node=write ", 1)
		beforeRename[id] = strings.Replace(told, "node=check ", "node=verify ", 1)
	}
	assertRowsUntouched(t, "finished task put back after a rename", beforeRename, f.finishedTasks(t, renamed))
	if still := f.listedAsReopened(t); len(still) != 0 {
		t.Errorf("after the tasks were put back the query still lists %d", len(still))
	}
}
