package instancemigration

import (
	"maps"
	"slices"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/gsoultan/metis/server/domains/entities"
	servicecontracts "github.com/gsoultan/metis/server/domains/services/contracts"
	"github.com/gsoultan/metis/server/domains/services/impl"
)

// What a migration does to work that is already finished.
//
// A mapping says where the work in progress on a step goes. The rewrite
// applied it to every row of the instance that named the step, whatever the
// row's status: a task completed weeks ago was rebuilt from the step the
// mapping pointed at and set claimed or unclaimed, as though it had just been
// offered. The approval that was given became work to do again, in somebody
// else's name, and the record of who gave it was gone.

// finishedTasks is every completed or canceled task of the instance, each
// told in full as the database holds it — the columns that say what happened
// and when, the time it was last written included.
func (f *fixture) finishedTasks(t *testing.T, instanceID uuid.UUID) map[string]string {
	t.Helper()
	return f.rowsByID(t, `
		SELECT id::text AS id,
		       'node=' || node_id || ' name=' || name || ' description=' || coalesce(description, '<null>') ||
		       ' type=' || type || ' status=' || status || ' assignee=' || coalesce(assignee, '<null>') ||
		       ' owner=' || coalesce(owner, '<null>') || ' delegation=' || coalesce(delegation_state, '<null>') ||
		       ' candidates=' || coalesce(candidate_users::text, '<null>') || '/' || coalesce(candidate_groups::text, '<null>') ||
		       ' priority=' || priority || ' due=' || coalesce(due_date::text, '<null>') ||
		       ' form=' || coalesce(form_key, '<null>') || '/' || md5(coalesce(form_definition, '')) ||
		       ' variables=' || md5(coalesce(variables::text, '')) || ' written=' || updated_at::text AS told
		  FROM tasks WHERE instance_id = ? AND status IN ('completed', 'canceled') AND deleted_at IS NULL`, instanceID)
}

// jobsInFull is every timer and queued call of the instance, told in full.
func (f *fixture) jobsInFull(t *testing.T, instanceID uuid.UUID) map[string]string {
	t.Helper()
	return f.rowsByID(t, `
		SELECT id::text AS id,
		       'definition=' || definition_id || ' node=' || node_id || ' type=' || type || ' status=' || status ||
		       ' retries=' || retries || ' due=' || next_run_at::text || ' written=' || updated_at::text AS told
		  FROM jobs WHERE instance_id = ? AND deleted_at IS NULL`, instanceID)
}

// incidentsInFull is every incident of the instance, told in full.
func (f *fixture) incidentsInFull(t *testing.T, instanceID uuid.UUID) map[string]string {
	t.Helper()
	return f.rowsByID(t, `
		SELECT id::text AS id,
		       'definition=' || coalesce(definition_id::text, '<null>') || ' node=' || node_id || ' status=' || status ||
		       ' resolved=' || coalesce(resolved_at::text, '<null>') || ' written=' || updated_at::text AS told
		  FROM incidents WHERE instance_id = ? AND deleted_at IS NULL`, instanceID)
}

// rowsByID reads rows as (id, told) pairs from the database, not through the
// code under test.
func (f *fixture) rowsByID(t *testing.T, query string, args ...any) map[string]string {
	t.Helper()
	var rows []struct {
		ID   string
		Told string
	}
	if err := f.db.WithContext(f.ctx).Raw(query, args...).Scan(&rows).Error; err != nil {
		t.Fatalf("read the rows: %v", err)
	}
	byID := make(map[string]string, len(rows))
	for _, row := range rows {
		byID[row.ID] = row.Told
	}
	return byID
}

// assertRowsUntouched fails, naming each row that differs, unless the rows are
// exactly the rows they were.
func assertRowsUntouched(t *testing.T, what string, before, after map[string]string) {
	t.Helper()
	if len(before) == 0 {
		t.Fatalf("there was no %s before the migration; the test is not exercising the case", what)
	}
	for _, id := range slices.Sorted(maps.Keys(before)) {
		if after[id] != before[id] {
			t.Errorf("a %s was rewritten by the migration.\n  was: %s\n  is:  %s", what, before[id], after[id])
		}
	}
	if len(after) != len(before) {
		t.Errorf("there were %d %s(s) before the migration and %d after", len(before), what, len(after))
	}
}

// TestAMappingDoesNotReopenAnApprovalThatWasGiven.
//
// Root cause: the rewrite applied the mapping to every task of the instance,
// whatever its status, and a task that changes step is rebuilt from the step
// it lands on — offered again, claimed by whoever that step names.
//
// Nothing races. The operations manager approved a quotation, which now waits
// for the sales manager. The quotation is migrated with the mapping that sends
// the operations approval's work to the sales approval. Afterwards the
// operations manager's completed task was an open sales approval — two open
// sales approvals for one token — and no task said the operations approval had
// been given, or by whom.
func TestAMappingDoesNotReopenAnApprovalThatWasGiven(t *testing.T) {
	f := newFixture(t)
	first, second := f.parkedOnOpsApprove(t)
	v1, v2 := uuidOf(t, first), uuidOf(t, second)
	f.completeTaskOn(t, "opsApprove", "ollie")
	instance := f.assertWaitingAt(t, v1, "salesApprove")
	given := f.finishedTasks(t, instance.ID)
	if len(given) != 2 {
		t.Fatalf("%d task(s) are finished, want the supervisor's review and the operations approval", len(given))
	}

	result, err := f.svc.ApplyInstanceMigration(f.ctx, v1, v2, map[string]string{"opsApprove": "salesApprove"},
		servicecontracts.WithActor("dita"))
	if err != nil {
		t.Fatalf("apply: %v", err)
	}
	if result.Changed != 1 || len(result.PassedOver) != 0 {
		t.Errorf("the run says it acted on %d and passed over %d, want one and none", result.Changed, len(result.PassedOver))
	}

	// One sales approval is open, as before, on the version it now runs.
	f.assertWaitingAt(t, v2, "salesApprove")
	f.assertNothingIsStranded(t)
	// And what was done is still what was done, to the last column.
	assertRowsUntouched(t, "finished task", given, f.finishedTasks(t, instance.ID))

	// Nothing in progress was on the mapped step, so the migration's entry
	// does not say that work in progress was re-pointed from it.
	entry := f.entryOf(t, instance.ID, impl.EventInstanceMigrated)
	if strings.Contains(entry.Narrative, "re-pointed") {
		t.Errorf("the trail says work in progress was re-pointed; only a finished task was on that step: %s", entry.Narrative)
	}
	f.runsToItsEnd(t)
}

// everyKindOfRow is draft → a one-hour wait → check → approve, where a message
// that the request was recalled interrupts the check and goes straight to the
// approval. Each step's id is the parameter: the second version renames them
// all, so that a mapping names every one.
func everyKindOfRow(projectID uuid.UUID, draft, wait, check, approve string) *entities.ProcessDefinition {
	return &entities.ProcessDefinition{
		Project: &entities.Project{ID: projectID},
		Key:     "recallable-request",
		Name:    "Recallable request",
		Nodes: []*entities.Node{
			{ID: "start", Type: entities.StartEvent, Outgoing: []string{"e1"}},
			{ID: draft, Name: "Draft the request (" + draft + ")", Type: entities.UserTask, Assignee: "sam", Incoming: []string{"e1"}, Outgoing: []string{"e2"}},
			{
				ID: wait, Name: "Cooling off", Type: entities.IntermediateCatchEvent,
				Properties: map[string]any{"timer_duration": "PT1H"},
				Incoming:   []string{"e2"}, Outgoing: []string{"e3"},
			},
			{ID: check, Name: "Check the request (" + check + ")", Type: entities.UserTask, Assignee: "cyd", Incoming: []string{"e3"}, Outgoing: []string{"e4"}},
			{
				ID: "recalled", Name: "The request was recalled", Type: entities.BoundaryEvent, AttachedToRef: check,
				Properties: map[string]any{"message_name": "request-recalled"}, Outgoing: []string{"e5"},
			},
			{ID: approve, Name: "Approve the request (" + approve + ")", Type: entities.UserTask, Assignee: "ollie", Incoming: []string{"e4", "e5"}, Outgoing: []string{"e6"}},
			{ID: "end", Type: entities.EndEvent, Incoming: []string{"e6"}},
		},
		Flows: []*entities.SequenceFlow{
			{ID: "e1", SourceRef: "start", TargetRef: draft},
			{ID: "e2", SourceRef: draft, TargetRef: wait},
			{ID: "e3", SourceRef: wait, TargetRef: check},
			{ID: "e4", SourceRef: check, TargetRef: approve},
			{ID: "e5", SourceRef: "recalled", TargetRef: approve},
			{ID: "e6", SourceRef: approve, TargetRef: "end"},
		},
	}
}

// TestAMappingChangesOnlyWhatIsStillOpen.
//
// One instance with one of each: a completed task, a canceled task, a timer
// that already fired, an incident that was resolved, and one open task. Every
// step is renamed by the new version and named by the mapping. Only the open
// task follows it. The rest say what happened, on the version it happened on,
// and are not written at all.
func TestAMappingChangesOnlyWhatIsStillOpen(t *testing.T) {
	f := newFixture(t)
	v1, v2 := f.startedOn(t,
		everyKindOfRow(f.project, "draft", "wait", "check", "approve"),
		everyKindOfRow(f.project, "write", "pause", "verify", "signOff"))
	instance := f.onlyInstance(t)

	// The draft is completed, the hour passes, and the request is recalled
	// while it is being checked: the check is withdrawn and the approval opens.
	f.completeTaskOn(t, "draft", "sam")
	if err := f.db.WithContext(f.ctx).Exec(
		`UPDATE jobs SET next_run_at = now() - interval '1 minute' WHERE instance_id = ?`, instance.ID).Error; err != nil {
		t.Fatalf("bring the timer due: %v", err)
	}
	if err := f.svc.ProcessPendingJobs(f.ctx); err != nil {
		t.Fatalf("process pending jobs: %v", err)
	}
	f.assertWaitingAt(t, v1, "check")
	if err := f.svc.SendMessage(f.ctx, f.project, "request-recalled", "", nil); err != nil {
		t.Fatalf("recall the request: %v", err)
	}
	f.assertWaitingAt(t, v1, "approve")
	// And the wait once failed and was put right: an incident, resolved.
	if err := f.db.WithContext(f.ctx).Exec(`
		INSERT INTO incidents (id, created_at, updated_at, instance_id, definition_id, node_id, error, status, resolved_at)
		VALUES (gen_random_uuid(), now() - interval '2 hours', now() - interval '1 hour', ?, ?, 'wait',
		        'the timer could not be queued', 'resolved', now() - interval '1 hour')`, instance.ID, v1).Error; err != nil {
		t.Fatalf("record a resolved incident: %v", err)
	}

	tasks, jobs, incidents := f.finishedTasks(t, instance.ID), f.jobsInFull(t, instance.ID), f.incidentsInFull(t, instance.ID)
	if len(tasks) != 2 || len(jobs) != 1 || len(incidents) != 1 {
		t.Fatalf("before the migration there are %d finished tasks, %d jobs and %d incidents; want the draft and the check, the timer, and the one incident",
			len(tasks), len(jobs), len(incidents))
	}
	for _, told := range jobs {
		if !strings.Contains(told, "status=completed") {
			t.Fatalf("the timer has not fired; the test is not exercising a finished job: %s", told)
		}
	}

	mapping := map[string]string{"draft": "write", "wait": "pause", "check": "verify", "approve": "signOff"}
	result, err := f.svc.ApplyInstanceMigration(f.ctx, v1, v2, mapping, servicecontracts.WithActor("dita"))
	if err != nil {
		t.Fatalf("apply: %v", err)
	}
	if result.Changed != 1 || len(result.PassedOver) != 0 {
		t.Errorf("the run says it acted on %d and passed over %d, want one and none", result.Changed, len(result.PassedOver))
	}

	// The open task moved, and was rebuilt from the step it landed on.
	f.assertWaitingAt(t, v2, "signOff")
	if open := f.openTasks(t); len(open) == 1 && open[0].Name != "Approve the request (signOff)" {
		t.Errorf("the open task is named %q; it should have been rebuilt from the step it landed on", open[0].Name)
	}
	f.assertNothingIsStranded(t)

	// Nothing else did.
	assertRowsUntouched(t, "finished task", tasks, f.finishedTasks(t, instance.ID))
	assertRowsUntouched(t, "finished job", jobs, f.jobsInFull(t, instance.ID))
	assertRowsUntouched(t, "resolved incident", incidents, f.incidentsInFull(t, instance.ID))

	if moves := f.entryOf(t, instance.ID, impl.EventInstanceMigrated).Data["task_moves"]; len(moves.([]any)) != 1 {
		t.Errorf("the trail lists %v as work re-pointed; only the approval was in progress", moves)
	}
	f.runsToItsEnd(t)
}
