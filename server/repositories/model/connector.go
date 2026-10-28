package model

import (
	"time"

	"github.com/gsoultan/storm"
)

// Connector is a catalogue entry: what an integration is, and what configuring
// one requires. It carries no credentials — those belong to a ConnectorInstance,
// which is per project and per runtime.
type Connector struct {
	storm.Model

	Key         string
	Name        string
	Description *string
	Icon        *string
	Type        string

	// Properties is a jsonb array of ConnectorProperty: the fields a person
	// fills in to configure this connector.
	//
	// Named Properties rather than Schema, which is what the GORM model called
	// it. A struct cannot have a field and a method of the same name, and
	// storm's declaration hook is Schema — so the field had to move. The API's
	// spelling is the adapter's business, not the table's.
	Properties storm.JSON

	DeletedAt *time.Time
}

func (c *Connector) Schema(t *storm.Table) {
	// What a database GORM created holds where these are NULL: the value GORM
	// read a NULL as. See fill.go.
	t.Col(&c.Key).FillNulls(blank)
	t.Col(&c.Name).FillNulls(blank)
	t.Col(&c.Type).FillNulls(blank)
	t.Col(&c.Properties).ConvertFrom(storm.JSONText).FillNulls(jsonNull)
	t.Col(&c.Key).Size(255)
	// Live rows only: a removed connector's key is free again.
	t.Col(&c.Key).Unique()
	t.Col(&c.Type).Size(64)
	t.Col(&c.DeletedAt).Index()
	// Declared, so the predicate is compiled into every read of this table
	// rather than written out at each call site. See the note in doc.go.
	t.SoftDelete(&c.DeletedAt)
	// Restoring by key finds the entry removed last under it, which is a read
	// of the marked rows, and takes it back in the same statement.
	t.ReadsDeleted()
	t.Takes()
}
