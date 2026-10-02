package impl

import (
	"context"
	"strings"
	"unicode/utf8"

	"github.com/gsoultan/metis/internal/pkg/apierr"
	"github.com/gsoultan/metis/server/domains/entities"
	servicecontracts "github.com/gsoultan/metis/server/domains/services/contracts"
)

// handOverAction is what a refused hand-over says the caller cannot do.
const handOverAction = "hand it to someone else"

// handOverCaller is who is asking for a hand-over, as far as the rules care:
// whether they hold the task, and what their roles let them do.
//
// It is worked out from the task as it stands under the row lock. The endpoint
// used to decide this from an unlocked read, so a hand-over racing another was
// judged against a task that had already changed hands.
type handOverCaller struct {
	username string
	// holdsTask: the caller is the task's current assignee.
	holdsTask bool
	// administrator: the caller holds ADMIN, globally or in the organization
	// the request is for — what principal.HasRole answers at an endpoint.
	administrator bool
	// takesUnnamed: the caller may take a task nobody was named for.
	takesUnnamed bool
}

// handOverCallerFor describes actor as the caller of a hand-over of task.
//
// The roles are the signed-in account's in the organization the request is
// for, and they count only when that account is the actor: a call naming
// somebody else is not lent the caller's roles (as mayTakeUnnamedTask).
func handOverCallerFor(ctx context.Context, actor string, task entities.Task) handOverCaller {
	caller := handOverCaller{
		username:  actor,
		holdsTask: actor != "" && task.AssigneeUsername() == actor,
	}
	account := signedIn(ctx)
	if account == nil || actor == "" || account.Username != actor {
		return caller
	}
	organization := entities.ActingOrganization(ctx)
	caller.administrator = account.HoldsRoleIn(organization, entities.RoleAdmin)
	caller.takesUnnamed = entities.TakesUnnamedWork(account.RolesIn(organization))
	return caller
}

// mayHandOver refuses an assign or a delegate from anyone but the task's
// holder or an administrator. A task nobody was named for is the operators'
// as well (entities.Task.FallsToOperators); anybody else is told who can.
func (c handOverCaller) mayHandOver(task entities.Task) error {
	if c.administrator || c.holdsTask {
		return nil
	}
	if task.FallsToOperators() {
		if c.takesUnnamed {
			return nil
		}
		return servicecontracts.ErrNobodyNamed
	}
	return apierr.Forbiddenf("only the person holding this task, or an administrator, can %s", handOverAction)
}

// mayRelease refuses a release from anyone but the holder or an administrator.
// Only a task somebody holds can be released, so the operators' share of the
// work nobody was named for does not come into it.
func (c handOverCaller) mayRelease() error {
	if c.administrator || c.holdsTask {
		return nil
	}
	return apierr.Forbiddenf("only the person holding this task, or an administrator, can %s", handOverAction)
}

// reasonFor returns the reason, trimmed, and refuses a hand-over that needs
// one and has none. Somebody handing on their own task need not explain it;
// anyone else changing whose work it is must, and it is kept with the entry.
// doing completes "say why you are …".
func (c handOverCaller) reasonFor(reason, doing string) (string, error) {
	reason = strings.TrimSpace(reason)
	if utf8.RuneCountInString(reason) > servicecontracts.MaxHandOverReasonLength {
		return "", apierr.Invalidf("the reason is longer than %d characters; say it more briefly",
			servicecontracts.MaxHandOverReasonLength)
	}
	if reason == "" && !c.holdsTask {
		return "", apierr.Invalidf("say why you are %s: a reason is required from anyone but the person holding the task", doing)
	}
	return reason, nil
}

// mayEdit refuses a change to a task's name, priority or due date from anyone
// but its holder or an administrator. They are how its holder orders their
// day; a task nobody holds is an administrator's to change.
func (c handOverCaller) mayEdit() error {
	if c.administrator || c.holdsTask {
		return nil
	}
	return apierr.Forbiddenf("only the person holding this task, or an administrator, can change its name, priority or due date")
}
