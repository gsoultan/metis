package impl

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/google/uuid"
	"github.com/rs/zerolog/log"

	"github.com/gsoultan/metis/internal/pkg/apierr"
	"github.com/gsoultan/metis/internal/pkg/envvar"
)

// EnvTasksAllowPlatformActors decides whether Users still claim and complete
// tasks through the platform's routes.
//
// Tasks are People's: a person signs in with a People token and works them
// through /api/v1/people/, or completes one from a task link. Users run the
// platform and are never given work. Until now, though, a task's names were
// matched against Users' accounts, so a running installation has Users doing
// tasks today, and turning that off at once would strand their queues.
//
// So this is a switch-over window. Unset or true, a User named by a task may
// still claim and complete it, and each time they do the log says so, naming
// them and the task, so whoever moves the installation over can see who still
// relies on it. False refuses a User's claim and completion; setting it back
// is the rollback. The default flips to off in a later release.
const EnvTasksAllowPlatformActors = "METIS_TASKS_ALLOW_PLATFORM_ACTORS"

// PlatformActorsAllowed reports whether Users may still claim and complete
// tasks. Anything but a false value leaves the window open: a typo must not
// stop every queue on the installation.
//
// Read on each use, as the other migration settings are.
func PlatformActorsAllowed() bool {
	raw := strings.TrimSpace(envvar.Get(EnvTasksAllowPlatformActors))
	if raw == "" {
		return true
	}
	allowed, err := strconv.ParseBool(raw)
	return err != nil || allowed
}

// ErrPlatformActorsRetired refuses a User's claim or completion once the
// switch-over window is closed. It is a 403: the task is there, and Users are
// no longer the ones who do it.
var ErrPlatformActorsRetired = fmt.Errorf("%w: tasks are done by People now; sign in with a People login to work this task", apierr.ErrForbidden)

// asPlatformActor runs a User's claim or completion while the window is open,
// and says in the log that it happened, so the Users who still do tasks can be
// found and moved to People before the window closes.
func asPlatformActor(action string, taskID uuid.UUID, username string, act func() error) error {
	if !PlatformActorsAllowed() {
		return ErrPlatformActorsRetired
	}
	if err := act(); err != nil {
		return err
	}
	log.Info().
		Str("setting", EnvTasksAllowPlatformActors).
		Str("action", action).
		Str("user", username).
		Str("task_id", taskID.String()).
		Msg("A User acted on a task through the platform's routes. Tasks are People's: add this person to the project's People and give them a People login before this setting is turned off.")
	return nil
}
