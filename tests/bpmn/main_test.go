package bpmn_test

import (
	"os"
	"testing"
)

// TestMain turns script tasks on for this suite.
//
// They ship off (features.ScriptTasks), and this suite is where the execution
// semantics of a script task are pinned. The flag resolves once per process,
// on its first read, so it is set before any test runs. What happens while it
// is off is pinned beside the code that refuses: the deploy check in
// server/domains/validation and the step in server/domains/handlers/impl.
func TestMain(m *testing.M) {
	if err := os.Setenv("METIS_FEATURE_SCRIPT_TASKS", "true"); err != nil {
		panic("could not turn script tasks on for the BPMN suite: " + err.Error())
	}
	os.Exit(m.Run())
}
