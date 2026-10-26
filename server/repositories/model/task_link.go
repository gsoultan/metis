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
	// Task and Participant have no foreign key. One to tasks would make
	// issuing a link wait on a row the engine holds, as
	// retention_holds.instance_id would; and the participant lives in the
	// main database's directory, which an environment's database does not
	// carry.
	Task        Task
	Participant WorkflowUser

	TokenHash string
	ExpiresAt time.Time
	UsedAt    *time.Time
	VoidedAt  *time.Time
}

func (l *TaskLink) Schema(t *storm.Table) {
	t.Name("task_links")
	t.Col(&l.Task).NoIndex().AcknowledgeNoFK("issuing a link must not wait on a task row the engine holds")
	t.Col(&l.Participant).NoIndex().AcknowledgeNoFK("the participant lives in the main database's directory")
	t.Col(&l.TokenHash).Size(64)
	// A link is found by its token, and never by anything else.
	t.Index(&l.TokenHash).Unique().Named("ux_task_links_token_hash")
	// A task's links, voided together once one of them is used.
	t.Index(&l.Task).Named("ix_task_links_task")
}
