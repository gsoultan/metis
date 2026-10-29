package model

import (
	"time"

	"github.com/gsoultan/storm"
)

// Notification is something addressed to one person.
//
// UserID is a username string rather than a foreign key, for the same reason a
// task's assignee is: notifications are addressed to whoever the process named.
// Project and Instance are optional because a system notification belongs to
// neither.
type Notification struct {
	storm.Model

	UserID string

	Type    string
	Title   string
	Message string
	IsRead  bool
	Link    *string

	Project  *Project
	Instance *ProcessInstance

	DeletedAt *time.Time
}

func (n *Notification) Schema(t *storm.Table) {
	// GORM's defaults, kept. See fill.go.
	t.Col(&n.IsRead).Default(no)
	// What a database GORM created holds where these are NULL: the value GORM
	// read a NULL as. See fill.go.
	t.Col(&n.UserID).FillNulls(blank)
	t.Col(&n.Type).FillNulls(blank)
	t.Col(&n.Title).FillNulls(blank)
	t.Col(&n.Message).FillNulls(blank)
	t.Col(&n.IsRead).FillNulls(no)
	t.Col(&n.UserID).Size(255)
	t.Col(&n.UserID).Index()
	t.Col(&n.Type).Size(64)
	t.Col(&n.Title).Size(255)
	// A notification outlives what it was about. Both references are set to
	// null rather than cascading, because deleting a project must not silently
	// delete the messages telling people what happened in it.
	t.Col(&n.Project).OnDelete(storm.SetNull).Index()
	t.Col(&n.Instance).OnDelete(storm.SetNull).Index()
	t.Col(&n.DeletedAt).Index()
	// A person's unread count per project, and their newest first: the bell
	// and the inbox. Partial, so they are the size of what is still shown.
	t.Index(&n.UserID, &n.IsRead, &n.Project).Where("(deleted_at IS NULL)").Named("ix_notifications_user_unread")
	t.Index(&n.UserID, storm.Desc(&n.CreatedAt), storm.Desc(&n.ID)).Where("(deleted_at IS NULL)").Named("ix_notifications_user_newest")
	// Declared, so the predicate is compiled into every read of this table
	// rather than written out at each call site. See the note in doc.go.
	t.SoftDelete(&n.DeletedAt)
}
