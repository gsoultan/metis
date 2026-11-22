package impl

import (
	"errors"
	"testing"

	"github.com/gsoultan/metis/internal/pkg/apierr"
	"github.com/gsoultan/metis/internal/pkg/features"
	"github.com/gsoultan/metis/server/domains/logic"
)

// POST /api/v1/processes/execute-script runs no JavaScript while the
// script-tasks flag is off, and says so as the caller's error, not a server
// failure.
func TestExecuteScriptIsRefusedWhileScriptsAreOff(t *testing.T) {
	defer features.OverrideForTest(features.ScriptTasks, false)()

	vars, err := (&Engine{}).ExecuteScript(t.Context(), "var a=[]; for(;;){a.push('x'.repeat(100000))}", "javascript", nil)
	if !errors.Is(err, apierr.ErrInvalidArgument) || !errors.Is(err, logic.ErrScriptTasksDisabled) {
		t.Fatalf("err = %v, want an invalid-request refusal", err)
	}
	if vars != nil {
		t.Errorf("variables %v came back from a refused script", vars)
	}
}
