package impl

import (
	"context"
	"strconv"

	"github.com/gsoultan/metis/internal/pkg/envvar"
	"github.com/gsoultan/metis/server/domains/entities"
)

// EnvAllowUnassignedTaskClaims brings back the rule this replaced: a task with
// no assignee and no candidates may be claimed and completed by anybody signed
// in to its organization.
//
// That rule read the absence of a constraint as "anyone", so somebody in
// accounts payable could pick up and complete an approval nobody had meant
// them to have. Such a task is the administrators' and operators' now
// (entities.Task.FallsToOperators).
//
// It changes what running installations do — a step modelled with nobody
// named, which members used to take, is refused to them — so the old rule stays
// available for a migration window: off by default, and announced at boot when
// on. An installation that sets it should give each of those steps an assignee
// or candidates, then turn it off.
const EnvAllowUnassignedTaskClaims = "METIS_ALLOW_UNASSIGNED_TASK_CLAIMS"

// AllowUnassignedTaskClaims reports whether the old rule is back.
//
// Read on each use, as the gateway's legacy setting is: the environment does
// not change under a running process, and a task that names somebody never
// asks.
func AllowUnassignedTaskClaims() bool {
	allowed, err := strconv.ParseBool(envvar.Get(EnvAllowUnassignedTaskClaims))
	return err == nil && allowed
}

// mayTakeUnnamedTask reports whether userID may take a task nobody was named
// for: an administrator or an operator, or anybody while the old rule is back.
//
// The roles are the signed-in caller's — the ones every role gate reads — and
// they count only when the caller is the person acting. Every endpoint passes
// the caller as the actor; a call naming somebody else is refused rather than
// lent the caller's roles.
func mayTakeUnnamedTask(ctx context.Context, userID string) bool {
	caller := signedIn(ctx)
	if caller != nil && userID != "" && caller.Username == userID && entities.TakesUnnamedWork(caller.Roles) {
		return true
	}
	return AllowUnassignedTaskClaims()
}
