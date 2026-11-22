package impl

import (
	"errors"
	"testing"

	"github.com/gsoultan/metis/internal/pkg/features"
	"github.com/gsoultan/metis/server/domains/entities"
	"github.com/gsoultan/metis/server/domains/logic"
)

// A stored script task does not run while the script-tasks flag is off. It
// fails at the step, naming why, and leaves the instance's variables alone.
func TestAStoredScriptTaskFailsAtItsStepWhileScriptsAreOff(t *testing.T) {
	defer features.OverrideForTest(features.ScriptTasks, false)()

	instance := &entities.ProcessInstance{Variables: map[string]any{"total": 1.0}}
	node := entities.Node{ID: "normalise", Type: entities.ScriptTask, Script: "total = 2"}
	err := (&ScriptTaskHandler{}).DoExecute(t.Context(), instance, &entities.ProcessDefinition{}, node, "")

	if !errors.Is(err, logic.ErrScriptTasksDisabled) {
		t.Fatalf("err = %v, want ErrScriptTasksDisabled", err)
	}
	if instance.Variables["total"] != 1.0 {
		t.Errorf("total = %v; the script ran", instance.Variables["total"])
	}
}
