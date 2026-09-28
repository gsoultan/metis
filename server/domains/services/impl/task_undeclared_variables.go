package impl

import (
	"strconv"

	"github.com/google/uuid"
	"github.com/rs/zerolog/log"

	"github.com/gsoultan/metis/internal/pkg/envvar"
	"github.com/gsoultan/metis/server/repositories/models"
)

// EnvAllowUndeclaredTaskVariables brings back the rule this replaced:
// completing a task may set any process variable, whether or not the task's
// form has a field for it.
//
// That rule let whoever completed a step write business data beyond it — the
// approver of a refund could rewrite its amount, or name somebody else as the
// one who approved it. A completion now sets only the variables its task's
// form declares (admitVariables).
//
// It changes what running installations do — an integration that completes
// tasks with variables no form declares is refused them — so the old rule stays
// available for a migration window: off by default, and announced at boot when
// on. While it is on, each step completed with variables its form does not
// declare is named in the log once, with those variables, so its form can be
// given the fields before the setting is turned off.
const EnvAllowUndeclaredTaskVariables = "METIS_ALLOW_UNDECLARED_TASK_VARIABLES"

// AllowUndeclaredTaskVariables reports whether the old rule is back.
//
// Read on each use, as AllowUnassignedTaskClaims is: the environment does not
// change under a running process, and a completion its form covers never asks.
func AllowUndeclaredTaskVariables() bool {
	allowed, err := strconv.ParseBool(envvar.Get(EnvAllowUndeclaredTaskVariables))
	return err == nil && allowed
}

// reportUndeclared names a step, once, whose completion set variables its form
// does not declare while the old rule is back: the list of forms to fix before
// the setting can be turned off. It names the variables, never their values.
func (s *taskService) reportUndeclared(task models.TaskModel, definitionKey string, undeclared []string) {
	// The project as well as the definition's key, which is unique only within
	// its project: two projects' copies of one process are two forms to fix.
	project := uuid.UUID(task.ProjectID).String()
	if !s.undeclaredReports.first(project + "\x00" + definitionKey + "\x00" + task.NodeID) {
		return
	}
	shown, more := shownNames(undeclared)
	event := log.Warn().
		Str("setting", EnvAllowUndeclaredTaskVariables).
		Str("project", project).
		Str("definition", definitionKey).
		Str("node", task.NodeID).
		Strs("variables", shown)
	if more > 0 {
		event = event.Int("more_variables", more)
	}
	event.Msg("A completion of this step set variables its form does not declare, which only this setting allows. " +
		"Give the step's form those fields, then turn the setting off.")
}
