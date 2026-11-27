package model

import (
	"time"

	"github.com/gsoultan/storm"
)

// WorkflowUser is somebody a process can assign work to: an approver, a
// reviewer, whoever the model names.
//
// Scoped to a project, and unique within it. Two projects naming an approver
// "ada" mean two different people, and a directory shared across projects would
// make onboarding one team an act that touches every other team's inbox.
//
// It carries no roles or permissions. What a participant may do is decided by
// the process that names them — a task is offered to an assignee or a candidate
// group, and completing it is the whole of the permission. Access control lives
// on PlatformUser, which is a different population doing a different job.
//
// A task does not reference this row. Assignee and the candidate lists are
// usernames, because a process is authored against people who may have no
// account yet, and refusing to deploy a model until every name in it exists
// would make authoring depend on onboarding. This table is what turns a name
// into somebody who can sign in and act on it.
type WorkflowUser struct {
	storm.Model

	Project Project

	// Username is the name a process model refers to. Unique within the
	// project, which is the population a process can address.
	Username string

	// A participant signs in to work their inbox, so they carry credentials.
	// Separate from the platform account of the same person, if they have one:
	// the two populations authenticate against different tables.
	//
	// Nullable because a participant can be imported before they have ever set
	// a password — a CSV of five hundred names is a directory, not five hundred
	// credentials — and a row with no hash simply cannot sign in yet.
	PasswordHash    *string
	TokensValidFrom *time.Time

	// ExternalID is this person's id in the customer's own application: the
	// key a directory import recognises them by, across renames. Optional, and
	// unique within the project when set — two projects, or two organizations,
	// may each have a person 1001, and they are different people.
	ExternalID *string

	DisplayName *string
	Email       *string

	// Active is how somebody stops receiving work without deleting the record
	// of what they did. Deleting them would orphan every task they completed.
	Active bool

	Groups []WorkflowGroupMembership

	DeletedAt *time.Time
}

func (u *WorkflowUser) Schema(t *storm.Table) {
	t.Col(&u.Username).Size(255)
	// Across the deleted rows: removing somebody marks their row, and an import
	// naming them again is somebody saying they are back. Scoped to the live
	// rows the upsert would stop conflicting and insert a second participant,
	// leaving their tasks and group memberships attached to the first.
	t.UniqueAcrossDeleted(&u.Project, &u.Username)
	t.Col(&u.ExternalID).Size(255)
	// Across the deleted rows for the same reason as the username: somebody
	// the source names again is the same person coming back, not a new one.
	t.UniqueAcrossDeleted(&u.Project, &u.ExternalID)
	t.Col(&u.Email).Size(320)
	t.Col(&u.Active).Index()
	t.Col(&u.DeletedAt).Index()
	// Declared, so the predicate is compiled into every read of this table
	// rather than written out at each call site. See the note in doc.go.
	t.SoftDelete(&u.DeletedAt)
}
