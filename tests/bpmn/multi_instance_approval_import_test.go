package bpmn_test

import (
	"errors"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/gsoultan/metis/internal/pkg/apierr"
	"github.com/gsoultan/metis/server/domains/entities"
)

// importedApproval is a two-of-three approval as a file: start → approve (once
// per approver, until condition holds) → record → end. condition is written
// into the file as given, so it has to be XML-escaped.
func importedApproval(key, condition string) []byte {
	return []byte(`<?xml version="1.0" encoding="UTF-8"?>
<definitions xmlns="http://www.omg.org/spec/BPMN/20100524/MODEL"
             xmlns:camunda="http://camunda.org/schema/1.0/bpmn">
  <process id="` + key + `" name="Imported approval" isExecutable="true">
    <startEvent id="start"><outgoing>f1</outgoing></startEvent>
    <userTask id="approve" name="Approve the purchase">
      <incoming>f1</incoming><outgoing>f2</outgoing>
      <multiInstanceLoopCharacteristics camunda:collection="approvers" camunda:elementVariable="approver">
        <completionCondition>` + condition + `</completionCondition>
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
</definitions>`)
}

// requireTwoOfThree starts the imported approval with three approvers and
// fails unless the second approval ends the step, once, and withdraws the
// third. It returns the instance, waiting at the step after the approval.
func requireTwoOfThree(t *testing.T, h engineHarness, key string) uuid.UUID {
	t.Helper()
	ctx := h.Ctx()
	instanceID, err := h.svc.StartProcess(ctx, h.projID, key,
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
	return instanceID
}

// BPMN 2.0.2 §10.3.8 (MultiInstanceLoopCharacteristics.completionCondition):
// when it evaluates to true the remaining instances are cancelled and a token
// is produced — whether the process was drawn here or arrived in a file.
//
// Import stored the condition where nothing evaluates it, so the same approval
// ended at two signatures when designed and waited for three when imported.
func TestAnImportedTwoOfThreeApprovalBehavesAsADesignedOne(t *testing.T) {
	h := newEngineHarness(t, "Imported Approval Project")
	ctx := h.Ctx()
	definitionID, err := h.svc.ImportDefinition(ctx, h.projID,
		importedApproval("imported-two-of-three", "nrOfCompletedInstances &gt;= 2"))
	if err != nil {
		t.Fatalf("import: %v", err)
	}
	instanceID := requireTwoOfThree(t, h, "imported-two-of-three")
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

// BPMN 2.0.2 §10.3.8: the completionCondition is an Expression, and the
// standard leaves its language to the tool. The files people import write it
// the way their modeler does: Camunda 7 and Flowable as ${…}, Camunda 8 as
// FEEL with a leading "=".
//
// Import kept the wrapper. The evaluator could not read the result, answered
// false, and a two-of-three approval waited for the third signature with
// nothing anywhere to say its condition had not been understood.
func TestAnImportedConditionInAnotherModelersFormStillEndsTheStepAtTwo(t *testing.T) {
	for name, condition := range map[string]string{
		"camunda-7": "${nrOfCompletedInstances &gt;= 2}",
		"camunda-8": "= nrOfCompletedInstances &gt;= 2",
	} {
		t.Run(name, func(t *testing.T) {
			h := newEngineHarness(t, "Imported Approval Project "+name)
			key := "imported-two-of-three-" + name
			if _, err := h.svc.ImportDefinition(h.Ctx(), h.projID, importedApproval(key, condition)); err != nil {
				t.Fatalf("import: %v", err)
			}
			finishRecording(h.Ctx(), t, h, requireTwoOfThree(t, h, key))
		})
	}
}

// BPMN 2.0.2 §10.3.8: a completionCondition decides when the step ends, so one
// the engine cannot read is a decision it cannot make.
//
// It is refused when the file is imported, in words that name the step, and
// nothing is deployed. Importing it and reading the condition as false would
// run the step for everybody — the answer nobody chose.
func TestAnImportedConditionTheEngineCannotReadIsRefusedAndNothingIsDeployed(t *testing.T) {
	h := newEngineHarness(t, "Unreadable Condition Project")
	ctx := h.Ctx()

	_, err := h.svc.ImportDefinition(ctx, h.projID,
		importedApproval("imported-unreadable", "${nrOfCompletedInstances == 2 &amp;&amp; approved}"))

	if !errors.Is(err, apierr.ErrInvalidArgument) {
		t.Fatalf("the import answered %v, want a refusal the caller can act on", err)
	}
	if text := err.Error(); !strings.Contains(text, "Approve the purchase") || strings.Contains(text, "'approve'") ||
		strings.Contains(text, `"approve"`) {
		t.Fatalf("the refusal should name the step as the diagram does, and not by its id: %q", text)
	}
	if _, startErr := h.svc.StartProcess(ctx, h.projID, "imported-unreadable", nil); startErr == nil {
		t.Fatal("the refused file was deployed all the same: an instance of it started")
	}
}
