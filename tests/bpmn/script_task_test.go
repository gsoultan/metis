package bpmn_test

import (
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/gsoultan/gobpm/server/domains/entities"
)

// A script task that writes a variable back.
//
// setVar is the documented way for a script to change process data — it is the
// helper the engine binds into every script scope. What was never checked is
// whether the value it writes actually survives the end of the script.

func scriptDefinition(projID uuid.UUID, key, script string) entities.ProcessDefinition {
	return entities.ProcessDefinition{
		Project: &entities.Project{ID: projID},
		Key:     key,
		Nodes: []*entities.Node{
			{ID: "start", Type: entities.StartEvent},
			{ID: "compute", Type: entities.ScriptTask, Name: "Work out the total", Script: script},
			{ID: "review", Type: entities.UserTask, Name: "Review the total"},
			{ID: "end", Type: entities.EndEvent},
		},
		Flows: []*entities.SequenceFlow{
			{ID: "f1", SourceRef: "start", TargetRef: "compute"},
			{ID: "f2", SourceRef: "compute", TargetRef: "review"},
			{ID: "f3", SourceRef: "review", TargetRef: "end"},
		},
	}
}

// runScriptProcess starts a one-script process and returns the instance
// variables as they were persisted afterwards.
func (h engineHarness) runScriptProcess(t *testing.T, key, script string, vars map[string]any) map[string]any {
	t.Helper()
	ctx := t.Context()

	def := scriptDefinition(h.projID, key, script)
	if _, err := h.svc.CreateDefinition(ctx, &def); err != nil {
		t.Fatalf("create definition: %v", err)
	}
	instanceID, err := h.svc.StartProcess(ctx, h.projID, key, vars)
	if err != nil {
		t.Fatalf("start process: %v", err)
	}
	instance, err := h.engine.GetInstance(ctx, instanceID)
	if err != nil {
		t.Fatalf("reload instance: %v", err)
	}
	return instance.Variables
}

// setVar on a variable the process already had must stick.
func TestScriptTaskSetVarUpdatesAnExistingVariable(t *testing.T) {
	h := newEngineHarness(t, "Script SetVar Project")

	got := h.runScriptProcess(t, "script-setvar", `setVar("total", 99);`, map[string]any{"total": 10})

	if total := toFloat(t, got["total"]); total != 99 {
		t.Errorf("setVar wrote 99 but the process kept %v — the write was discarded", got["total"])
	}
}

// Plain assignment to an existing variable is the other way to write one.
func TestScriptTaskAssignmentUpdatesAnExistingVariable(t *testing.T) {
	h := newEngineHarness(t, "Script Assign Project")

	got := h.runScriptProcess(t, "script-assign", `total = 99;`, map[string]any{"total": 10})

	if total := toFloat(t, got["total"]); total != 99 {
		t.Errorf("assignment wrote 99 but the process kept %v", got["total"])
	}
}

// setVar naming a variable the process did not have yet must add it.
func TestScriptTaskSetVarCreatesANewVariable(t *testing.T) {
	h := newEngineHarness(t, "Script NewVar Project")

	got := h.runScriptProcess(t, "script-newvar", `setVar("discount", 5);`, map[string]any{"total": 10})

	if _, ok := got["discount"]; !ok {
		t.Fatalf("setVar did not create the variable; got %v", got)
	}
	if discount := toFloat(t, got["discount"]); discount != 5 {
		t.Errorf("expected discount 5, got %v", got["discount"])
	}
}

// setVar must win over the value the script was given, even when the script
// also reads the variable afterwards.
func TestScriptTaskSetVarIsVisibleToTheRestOfTheScript(t *testing.T) {
	h := newEngineHarness(t, "Script Readback Project")

	got := h.runScriptProcess(t, "script-readback",
		`setVar("total", 99); setVar("doubled", total * 2);`,
		map[string]any{"total": 10})

	if total := toFloat(t, got["total"]); total != 99 {
		t.Errorf("expected total 99, got %v", got["total"])
	}
	if doubled := toFloat(t, got["doubled"]); doubled != 198 {
		t.Errorf("the script read a stale total after setVar: expected doubled 198, got %v", got["doubled"])
	}
}

// The engine's own ExecuteScript is a second implementation of the same thing
// and carries the same contract.
func TestExecuteScriptSetVarUpdatesAnExistingVariable(t *testing.T) {
	ctx := t.Context()
	h := newEngineHarness(t, "ExecuteScript Project")

	out, err := h.engine.ExecuteScript(ctx, `setVar("total", 99);`, "javascript", map[string]any{"total": 10})
	if err != nil {
		t.Fatalf("execute script: %v", err)
	}

	if total := toFloat(t, out["total"]); total != 99 {
		t.Errorf("setVar wrote 99 but ExecuteScript returned %v", out["total"])
	}
}

// A runaway script must be interrupted rather than holding its goroutine and
// transaction. Process definitions are untrusted input.
func TestScriptTaskIsBoundedByTheScriptTimeout(t *testing.T) {
	t.Setenv("GOBPM_SCRIPT_TIMEOUT", "200ms")
	ctx := t.Context()
	h := newEngineHarness(t, "Script Timeout Project")

	def := scriptDefinition(h.projID, "script-runaway", `while (true) {}`)
	if _, err := h.svc.CreateDefinition(ctx, &def); err != nil {
		t.Fatalf("create definition: %v", err)
	}

	_, err := h.svc.StartProcess(ctx, h.projID, "script-runaway", nil)
	if err == nil {
		t.Fatal("a script task with an infinite loop ran to completion")
	}
	if !strings.Contains(err.Error(), "budget") && !strings.Contains(err.Error(), "script") {
		t.Errorf("the error does not explain that the script was cut off: %v", err)
	}
}

// JSON numbers come back as float64; goja may hand back int64. Compare as numbers.
func toFloat(t *testing.T, v any) float64 {
	t.Helper()
	switch n := v.(type) {
	case float64:
		return n
	case float32:
		return float64(n)
	case int:
		return float64(n)
	case int64:
		return float64(n)
	default:
		t.Fatalf("expected a number, got %T (%v)", v, v)
		return 0
	}
}
