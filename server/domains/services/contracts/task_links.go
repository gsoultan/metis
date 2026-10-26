package contracts

import (
	"context"
	"time"

	"github.com/google/uuid"
	"github.com/gsoultan/metis/server/domains/entities"
)

// IssuedTaskLink is a link just issued, with the token that opens it. The
// token is answered here once and kept nowhere: it is to be sent to the
// person and forgotten.
type IssuedTaskLink struct {
	Person    entities.Person
	TaskID    uuid.UUID
	Token     string
	ExpiresAt time.Time
}

// TaskLinkView is what opening a link shows: the task, and who it is for.
type TaskLinkView struct {
	Task      entities.Task
	Person    entities.Person
	ExpiresAt time.Time
}

// TaskLinkService lets a person act on one task, once, through a link, with
// no sign-in.
//
// A link is good for one task and one person. It works once and expires; it
// stops working the moment its task is completed, withdrawn or given to
// somebody else, or its person is removed or deactivated. Every link that is
// no longer good is refused the same way, so a link says nothing about why.
type TaskLinkService interface {
	// IssueTaskLinks issues a link to a task for each person it names — its
	// assignee, or its candidate users and the members of its candidate
	// groups — who has an address to send it to.
	IssueTaskLinks(ctx context.Context, taskID uuid.UUID) ([]IssuedTaskLink, error)

	// ReadTaskLink shows the task a link is for, and changes nothing: a mail
	// scanner that opens every link must not act on any task.
	ReadTaskLink(ctx context.Context, token string) (TaskLinkView, error)

	// CompleteByTaskLink completes the task as the link's person, with the
	// variables its form declares, and uses the link up. On a pool task the
	// first link submitted wins; the others stop working.
	CompleteByTaskLink(ctx context.Context, token string, vars map[string]any) error
}
