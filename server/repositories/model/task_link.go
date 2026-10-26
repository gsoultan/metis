package model

import (
	"time"

	"github.com/gsoultan/storm"
)

// TaskLink lets one person act on one task by opening a link, without signing
// in.
//
// Only the token's SHA-256 is kept: the link is the credential, and a backup
// or a `SELECT` must not be enough to act on somebody's work. A link works
// once; UsedAt is when it did, and VoidedAt is when it stopped being good
// for any other reason.
type TaskLink struct {
	storm.Model

	Project Project
	// TaskID and ParticipantID are plain columns. A foreign key to tasks
	// would make issuing a link wait on a row the engine holds, as
	// retention_holds.instance_id would; and the participant lives in the
	// main database's directory, which an environment's database does not
	// carry.
	TaskID        storm.UUID
	ParticipantID storm.UUID

	TokenHash string
	ExpiresAt time.Time
	UsedAt    *time.Time
	VoidedAt  *time.Time
}

func (l *TaskLink) Schema(t *storm.Table) {
	t.Name("task_links")
	t.Col(&l.TokenHash).Size(64)
	// A link is found by its token, and never by anything else.
	t.Index(&l.TokenHash).Unique().Named("ux_task_links_token_hash")
	// A task's links, voided together once one of them is used.
	t.Index(&l.TaskID).Named("ix_task_links_task")
}
