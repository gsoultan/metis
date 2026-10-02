package bpmn_test

import (
	"strings"
	"testing"

	"github.com/gsoultan/metis/server/domains/entities"
)

// BPMN 2.0.2 §10.3.8 (MultiInstanceLoopCharacteristics.completionCondition):
// when it evaluates to true the remaining instances are cancelled and a token
// is produced — whether the process was drawn here or arrived in a file.
//
// Import stored the condition where nothing evaluates it, so the same approval
// ended at two signatures when designed and waited for three when imported.
func TestAnImportedTwoOfThreeApprovalBehavesAsADesignedOne(t *testing.T) {
	h := newEngineHarness(t, "Imported Approval Project")
	ctx := h.Ctx()
	const file = `<?xml version="1.0" encoding="UTF-8"?>
<definitions xmlns="http://www.omg.org/spec/BPMN/20100524/MODEL"
             xmlns:camunda="http://camunda.org/schema/1.0/bpmn">
  <process id="imported-two-of-three" name="Imported approval" isExecutable="true">
    <startEvent id="start"><outgoing>f1</outgoing></startEvent>
    <userTask id="approve" name="Approve the purchase">
      <incoming>f1</incoming><outgoing>f2</outgoing>
      <multiInstanceLoopCharacteristics camunda:collection="approvers" camunda:elementVariable="approver">
        <completionCondition>nrOfCompletedInstances &gt;= 2</completionCondition>
      </multiInstanceLoopCharacteristics>
    </userTask>
    <userTask id="record" name="Record the outcome">
      <incoming>f2</incoming><outgoing>f3</outgoing>
    </userTask>
    <endEvent id="end"><incoming>f3</incoming></endEvent>
    <sequenceFlow id="f1" sourceRef="start" targetRef="approve"/>
    <sequenceFlow id="f2" sourceRef="approve" targetRef="record"/>
    <sequenceFlow id="f3" sourceRef="record" targetRef="end"/>
  </process>
</definitions>`
	definitionID, err := h.svc.ImportDefinition(ctx, h.projID, []byte(file))
	if err != nil {
		t.Fatalf("import: %v", err)
	}
	instanceID, err := h.svc.StartProcess(ctx, h.projID, "imported-two-of-three",
		map[string]any{"approvers": []any{"ana", "budi", "citra"}})
	if err != nil {
		t.Fatalf("start: %v", err)
	}

	open := openIterationTasks(ctx, t, h, instanceID, "approve")
	if len(open) != 3 {
		t.Fatalf("three approvers were asked and %d task(s) are open", len(open))
	}
	if err := completeAs(ctx, h, open[0], "carol", nil); err != nil {
		t.Fatalf("first approval: %v", err)
	}
	if h.waitingAt(ctx, t, instanceID, "record") {
		t.Fatal("one approval of the two required moved the process on")
	}
	if err := completeAs(ctx, h, open[1], "carol", nil); err != nil {
		t.Fatalf("second approval: %v", err)
	}

	if seen := tasksEverOn(t, h, instanceID, "record"); seen != 1 {
		t.Fatalf("after the second approval the process moved on %d times, want once", seen)
	}
	third, err := h.svc.GetTask(ctx, open[2].ID)
	if err != nil {
		t.Fatalf("re-read the third approval: %v", err)
	}
	if third.Status != entities.TaskCanceled {
		t.Fatalf("the approval nobody needs any more is %q, want it withdrawn", third.Status)
	}
	requireRefusedInPlainWords(t, completeAs(ctx, h, third, "carol", nil), third)
	finishRecording(ctx, t, h, instanceID)

	// And the file that comes back out says the same thing.
	exported, err := h.svc.ExportDefinition(ctx, definitionID)
	if err != nil {
		t.Fatalf("export: %v", err)
	}
	if !strings.Contains(string(exported), "nrOfCompletedInstances &gt;= 2") {
		t.Errorf("export dropped the completion condition\n---\n%s", exported)
	}
}
