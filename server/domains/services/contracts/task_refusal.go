package contracts

import "github.com/gsoultan/metis/internal/pkg/apierr"

// ErrNobodyNamed refuses a task nobody was named for — no assignee and no
// candidates — to somebody who may not take one. Only an administrator or an
// operator may claim it, complete it or give it to somebody
// (entities.Task.FallsToOperators).
//
// Its text is what the person reads, so it says why, who can, and what to do.
// It is a 403: the task is theirs to see and not theirs to take.
var ErrNobodyNamed = apierr.Forbiddenf("this task has no assignee and no candidates, so only an administrator " +
	"or an operator can take it; ask one of them to take it or to give it to somebody")
