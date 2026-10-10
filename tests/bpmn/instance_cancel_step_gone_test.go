package bpmn_test

import (
	"reflect"
	"testing"

	"github.com/gsoultan/metis/server/domains/entities"
	serviceimpl "github.com/gsoultan/metis/server/domains/services/impl"
	"github.com/gsoultan/metis/server/repositories/models"
)

// Final-wave ruling FW-2. An instance holding a token on a step its version
// does not have is what a migration of an earlier release could leave
// (docs/upgrading.md, "An instance a migration left with nothing to do", case
// 1). Nothing else reaches it, so a cancel may name that step: it is shown by
// its id, since the version has no name for it, and the cancel is recorded as
// ended there. A waive and a hold still refuse a step the version lacks, and
// the sentence that says where the instance waits names the step by its id.
//
// No release since creates such an instance, so the row is written through
// the repository: the instance is put on a version without the step it is on,
// as that migration left it.
func TestACancelCanNameAStepTheInstancesVersionNoLongerHas(t *testing.T) {
	h := newEngineHarness(t, "Cancel Step Gone Project")
	events := &eventLog{}
	h.dispatcher.Register(events)
	h.recordsAsProductionDoes()
	w := newWaiver(h)
	ctx := h.Ctx()
	id := w.start(t, opsApproval(h.projID, "ops-step-gone"), nil)
	without, err := h.svc.CreateDefinition(ctx, &entities.ProcessDefinition{
		Project: &entities.Project{ID: h.projID}, Key: "ops-step-gone", Name: "Quotation approval",
		Nodes: []*entities.Node{
			{ID: "start", Type: entities.StartEvent},
			{ID: "salesApprove", Type: entities.UserTask, Name: "Sales approve"},
			{ID: "end", Type: entities.EndEvent},
		},
		Flows: []*entities.SequenceFlow{{ID: "q1", SourceRef: "start", TargetRef: "salesApprove"}, {ID: "q3", SourceRef: "salesApprove", TargetRef: "end"}},
	})
	if err != nil {
		t.Fatalf("deploy the version without the step: %v", err)
	}
	row, err := h.repo.Process().Get(ctx, id)
	if err != nil {
		t.Fatalf("read the instance: %v", err)
	}
	row.DefinitionID = models.UUID(without)
	if err := h.repo.Process().Update(ctx, row); err != nil {
		t.Fatalf("put the instance on the version without its step: %v", err)
	}
	if tokensOn(t, h, id, "opsApprove") != 1 {
		t.Fatal("the instance holds no token on the step its version lacks; this test needs one")
	}
	task := theOpenTask(t, h, id, "opsApprove")

	noSuchStep := `This process has no step "opsApprove".`
	for _, kind := range []entities.DeviationKind{entities.DeviationWaive, entities.DeviationHold} {
		if plan := w.preview(t, deviationCommand(kind, id, "opsApprove", nil)); plan.Applicable() || !said(plan.Refusals, noSuchStep) {
			t.Errorf("a %s at a step the version lacks: refusals:%s\nwant\n  %s", kind, lines(plan.Refusals), noSuchStep)
		}
	}
	// A step the version lacks and the instance is not on is still no step.
	if plan := w.preview(t, deviationCommand(entities.DeviationCancel, id, "archive", nil)); !reflect.DeepEqual(plan.Refusals, []string{`This process has no step "archive".`}) {
		t.Errorf("a cancel at a step the version lacks and the instance is not on: refusals:%s", lines(plan.Refusals))
	}
	unnamed := w.preview(t, deviationCommand(entities.DeviationCancel, id, "", nil))
	if want := "This instance is waiting at “opsApprove”; say which of those steps it is to be ended at."; !reflect.DeepEqual(unnamed.Refusals, []string{want}) {
		t.Errorf("a cancel that names no step: refusals:%s\nwant only\n  %s", lines(unnamed.Refusals), want)
	}

	cancel := deviationCommand(entities.DeviationCancel, id, "opsApprove", nil)
	plan := w.preview(t, cancel)
	wantWarnings := []string{"“Operations approve” is with ollie, who will be told it was withdrawn."}
	if !plan.Applicable() || plan.NodeID != "opsApprove" || plan.NodeName != "opsApprove" || !reflect.DeepEqual(plan.Warnings, wantWarnings) ||
		len(plan.OpenWork) != 1 || plan.OpenWork[0].TaskID != task.ID || plan.OpenWork[0].NodeName != "opsApprove" {
		t.Fatalf("a cancel at the step the version lacks: step %q named %q, open work %+v\nrefusals:%s\nwarnings:%s",
			plan.NodeID, plan.NodeName, plan.OpenWork, lines(plan.Refusals), lines(plan.Warnings))
	}

	w.mustApply(t, cancel)
	requireInstanceStatus(ctx, t, h, id, entities.ProcessCancelled)
	if now, err := h.svc.GetTask(ctx, task.ID); err != nil || now.Status != entities.TaskCanceled {
		t.Errorf("the task on the step is %q (%v), want it withdrawn", now.Status, err)
	}
	if told := toldOfWithdrawal(events); !reflect.DeepEqual(told, map[string]int{"ollie": 1}) {
		t.Errorf("withdrawals were announced to %v, want ollie told once", told)
	}
	rows := w.ledger(t, id)
	if len(rows) != 1 || rows[0].Kind != entities.DeviationCancel || rows[0].Node == nil || rows[0].Node.ID != "opsApprove" || rows[0].Node.Name != "opsApprove" {
		t.Fatalf("the ledger: %+v", rows)
	}
	entry := theEntryOf(t, h, id, serviceimpl.EventInstanceCancelled)
	if want := "This instance was ended at “opsApprove” by ana. Reason: " + waiveReason + "."; entry.Narrative != want || entry.Data["node_id"] != "opsApprove" {
		t.Errorf("the entry reads %q with %+v\nwant\n  %s", entry.Narrative, entry.Data, want)
	}
}
