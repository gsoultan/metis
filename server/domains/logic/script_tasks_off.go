package logic

import (
	"errors"
	"fmt"

	"github.com/gsoultan/metis/internal/pkg/features"
)

// ErrScriptTasksDisabled is what a script refused by the script-tasks flag
// wraps, so a caller can tell "switched off here" from a script that failed.
var ErrScriptTasksDisabled = errors.New("script tasks are switched off on this server")

// ErrScriptTasksOff says a script was not run because the flag is off, and what
// to do about it: the step it was in (when there is one), the variable that
// turns it on, and where the list of affected steps is.
func ErrScriptTasksOff(nodeID string) error {
	where := "a script"
	if nodeID != "" {
		where = "step " + nodeID
	}
	return fmt.Errorf("%s runs JavaScript, and %w: a script has no memory limit, so one can run the server out of memory. "+
		"Rewrite it as FEEL mappings or a service task, or ask whoever runs Metis to set %s=true. "+
		"GET /api/v1/definitions/script-tasks lists every script task",
		where, ErrScriptTasksDisabled, features.EnvName(features.ScriptTasks))
}
