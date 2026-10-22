package model

import (
	"time"

	"github.com/gsoultan/storm"
)

// OutgoingWebhook is an address this installation posts a project's events to.
type OutgoingWebhook struct {
	storm.Model

	Project Project

	Name string
	URL  string

	// Events is a JSON array of the event names it is sent, such as
	// "process.completed". A JSON list rather than a table: it is read whole,
	// with the webhook, and matched by the queue in one predicate.
	Events storm.JSON

	// Secret signs every delivery, sealed by the repository. Never returned by
	// the API after the response that created it.
	Secret string

	Enabled bool

	DeletedAt *time.Time
}

func (w *OutgoingWebhook) Schema(t *storm.Table) {
	t.Col(&w.Name).Size(255)
	t.Col(&w.URL).Size(2048)
	t.Col(&w.Events).Default(emptyJSONList)
	t.Col(&w.Secret).Size(512)
	// Every event a project raises asks which of its webhooks want it, so the
	// question is answered by an index over the live, switched-on rows alone.
	// The predicate is spelled as PostgreSQL prints it back, so the drift
	// report reads the index as the one declared.
	t.Index(&w.Project).Where("(enabled AND (deleted_at IS NULL))").Named("ix_outgoing_webhooks_project_live")
	t.Col(&w.DeletedAt).Index()
	// Declared, so the predicate is compiled into every read of this table
	// rather than written out at each call site. See the note in doc.go.
	t.SoftDelete(&w.DeletedAt)
}
