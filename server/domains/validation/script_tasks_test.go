package validation

import (
	"strings"
	"testing"

	"github.com/gsoultan/metis/internal/pkg/features"
	"github.com/gsoultan/metis/server/domains/entities"
)

// A script in a script task is refused at deploy while the script-tasks flag is
// off: goja has no heap limit, and one script reaches about 1.5 GB inside its
// budget. Wherever the script is carried, because the handler runs any of them.
func TestAScriptTaskIsRefusedAtDeployWhileScriptsAreOff(t *testing.T) {
	defer features.OverrideForTest(features.ScriptTasks, false)()

	for _, node := range []*entities.Node{
		{ID: "in-script", Type: entities.ScriptTask, Script: "total = 1"},
		{ID: "in-condition", Type: entities.ScriptTask, Condition: "total = 1"},
		{ID: "in-property", Type: entities.ScriptTask, Properties: map[string]any{"script": "total = 1"}},
	} {
		errs := errorsFor(node)
		if len(errs) != 1 || !strings.Contains(errs[0], node.ID) || !strings.Contains(errs[0], features.EnvName(features.ScriptTasks)) {
			t.Errorf("%s: errors %v, want one naming the step and %s", node.ID, errs, features.EnvName(features.ScriptTasks))
		}
	}
}

// A script task with nothing to run runs no JavaScript, so it deploys; so does
// a script task once an operator has turned scripts on.
func TestAScriptTaskDeploysWhenItRunsNoScriptOrScriptsAreOn(t *testing.T) {
	restore := features.OverrideForTest(features.ScriptTasks, false)
	if errs := errorsFor(&entities.Node{ID: "empty", Type: entities.ScriptTask}); len(errs) != 0 {
		t.Errorf("an empty script task was refused: %v", errs)
	}
	restore()

	defer features.OverrideForTest(features.ScriptTasks, true)()
	if errs := errorsFor(&entities.Node{ID: "on", Type: entities.ScriptTask, Script: "total = 1"}); len(errs) != 0 {
		t.Errorf("a script task was refused with scripts on: %v", errs)
	}
}
