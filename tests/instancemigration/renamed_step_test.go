package instancemigration

import (
	"errors"
	"maps"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/gsoultan/metis/server/domains/entities"
	servicecontracts "github.com/gsoultan/metis/server/domains/services/contracts"
	"github.com/gsoultan/metis/server/domains/services/impl"
)

// Finished work under a mapping that renames a step, and under one that
// redirects it.
//
// A mapping has two shapes. A rename says "this step is that step": the id it
// maps to is new, and nothing else is mapped onto it. A redirect says "send
// the open work over there": the id it maps to is another step of the old
// version, or several steps are sent to one. Finished work follows a rename,
// because the step it was done on is still the same step and the new
// version's rules name it by its new id. It does not follow a redirect: that
// would record that somebody did a step they did not do.

// fourEyes is submit → approve, where whoever did the submit may not do the
// approval. The ids are the parameters: the second version renames both, and
// its rule names the submit by its new id.
func fourEyes(projectID uuid.UUID, submit, approve string) *entities.ProcessDefinition {
	return &entities.ProcessDefinition{
		Project: &entities.Project{ID: projectID},
		Key:     "four-eyes",
		Name:    "Four eyes",
		Nodes: []*entities.Node{
			{ID: "start", Type: entities.StartEvent, Outgoing: []string{"e1"}},
			{ID: submit, Name: "Submit the request", Type: entities.UserTask, Assignee: "ada", Incoming: []string{"e1"}, Outgoing: []string{"e2"}},
			{
				ID: approve, Name: "Approve the request", Type: entities.UserTask, CandidateUsers: []*entities.User{{Username: "ada"}, {Username: "bo"}},
				Properties: map[string]any{"separation_of_duties": submit},
				Incoming:   []string{"e2"}, Outgoing: []string{"e3"},
			},
			{ID: "end", Type: entities.EndEvent, Incoming: []string{"e3"}},
		},
		Flows: []*entities.SequenceFlow{
			{ID: "e1", SourceRef: "start", TargetRef: submit},
			{ID: "e2", SourceRef: submit, TargetRef: approve},
			{ID: "e3", SourceRef: approve, TargetRef: "end"},
		},
	}
}

// TestAfterARenameWhoeverDidOneHalfOfAFourEyesCheckMayNotDoTheOther.
//
// Root cause: separation of duties finds who performed a step by reading the
// instance's completed tasks by step id, against the rule of the version the
// instance now runs, which names the step by its new id. A finished task kept
// the old id, so after a migration that only renamed the steps the rule found
// nobody, and the person who submitted the request could approve it.
func TestAfterARenameWhoeverDidOneHalfOfAFourEyesCheckMayNotDoTheOther(t *testing.T) {
	f := newFixture(t)
	v1, v2 := f.startedOn(t, fourEyes(f.project, "submit", "approve"), fourEyes(f.project, "request", "signOff"))
	f.completeTaskOn(t, "submit", "ada")
	instance := f.assertWaitingAt(t, v1, "approve")
	before := f.finishedTasks(t, instance.ID)

	result, err := f.svc.ApplyInstanceMigration(f.ctx, v1, v2, map[string]string{"submit": "request", "approve": "signOff"},
		servicecontracts.WithActor("dita"))
	if err != nil {
		t.Fatalf("apply: %v", err)
	}
	if result.Changed != 1 || len(result.PassedOver) != 0 {
		t.Errorf("the run says it acted on %d and passed over %d, want one and none", result.Changed, len(result.PassedOver))
	}
	f.assertWaitingAt(t, v2, "signOff")
	f.assertNothingIsStranded(t)

	// The finished submit is the same row, on the step's new id and in
	// nothing else different: still completed, still ada's.
	renamed := maps.Clone(before)
	for id, told := range renamed {
		renamed[id] = strings.Replace(told, "node=submit ", "node=request ", 1)
	}
	assertRowsUntouched(t, "finished task, but for its step's new id", renamed, f.finishedTasks(t, instance.ID))
	// It was not work in progress, and the migration's entry does not list it
	// as re-pointed.
	if moves := f.entryOf(t, instance.ID, impl.EventInstanceMigrated).Data["task_moves"]; len(moves.([]any)) != 1 {
		t.Errorf("the trail lists %v as work re-pointed; only the approval was in progress", moves)
	}

	// And the rule holds: ada, who submitted it, may neither take nor give
	// the approval.
	approval := f.openTasks(t)[0]
	if err := f.svc.ClaimTask(f.ctx, approval.ID, "ada"); !errors.Is(err, impl.ErrTaskForbidden) {
		t.Errorf("ada, who did the submit, claims the approval: err=%v, want it refused", err)
	}
	if err := f.svc.CompleteTask(f.ctx, approval.ID, "ada", nil); !errors.Is(err, impl.ErrTaskForbidden) {
		t.Errorf("ada, who did the submit, completes the approval: err=%v, want it refused", err)
	}
	if err := f.svc.CompleteTask(f.ctx, approval.ID, "bo", nil); err != nil {
		t.Fatalf("bo, who did not, completes the approval: %v", err)
	}
	if done := f.onlyInstance(t); done.Status != entities.ProcessCompleted {
		t.Errorf("the instance is %s; with the approval given it should have finished", done.Status)
	}
}

// A redirect leaves finished work exactly as it is, both ways a mapping can be
// one: onto a step the old version has too, and several steps onto one new id.
func TestARedirectDoesNotMoveFinishedWork(t *testing.T) {
	redirects := map[string]struct {
		second  func(f *fixture) *entities.ProcessDefinition
		mapping map[string]string
		waiting string
	}{
		"onto a step the old version has too": {
			second: quotationV2, mapping: map[string]string{"opsApprove": "salesApprove"}, waiting: "salesApprove",
		},
		"two steps onto one new id": {
			second: func(f *fixture) *entities.ProcessDefinition {
				def := quotationV2(f)
				def.Nodes[1].ID, def.Flows[0].TargetRef, def.Flows[1].SourceRef = "review", "review", "review"
				return def
			},
			mapping: map[string]string{"supervisorReview": "review", "opsApprove": "review"}, waiting: "salesApprove",
		},
	}
	for name, redirect := range redirects {
		t.Run(name, func(t *testing.T) {
			f := newFixture(t)
			v1, v2 := f.startedOn(t, quotationV1(f), redirect.second(f))
			f.completeTaskOn(t, "supervisorReview", "sam")
			f.completeTaskOn(t, "opsApprove", "ollie")
			instance := f.assertWaitingAt(t, v1, "salesApprove")
			before := f.finishedTasks(t, instance.ID)

			if _, err := f.svc.ApplyInstanceMigration(f.ctx, v1, v2, redirect.mapping, servicecontracts.WithActor("dita")); err != nil {
				t.Fatalf("apply: %v", err)
			}
			f.assertWaitingAt(t, v2, redirect.waiting)
			f.assertNothingIsStranded(t)
			assertRowsUntouched(t, "finished task", before, f.finishedTasks(t, instance.ID))
			f.runsToItsEnd(t)
		})
	}
}
