package model

import (
	"time"

	"github.com/gsoultan/storm"
)

// ConnectorInstance is one project's configuration of a connector: the endpoint
// it talks to and the credential it authenticates with.
//
// Per project, and so per runtime — staging's API key is not production's, and
// that is the point of an instance being separate from the catalogue entry.
type ConnectorInstance struct {
	storm.Model

	Project   Project
	Connector Connector

	Name string

	// Config holds whatever this connector needs to authenticate.
	// Encrypted by the repository before it is written: it was once stored as
	// plain JSON, which put every third-party credential in the installation
	// into any database backup.
	// A string, not storm.JSON: this holds ciphertext. See Environment.Connection
	// for why the distinction is load-bearing rather than cosmetic.
	Config string

	DeletedAt *time.Time
}

func (c *ConnectorInstance) Schema(t *storm.Table) {
	// What a database GORM created holds where these are NULL: the value GORM
	// read a NULL as. See fill.go.
	t.Col(&c.Name).FillNulls(blank)
	t.Col(&c.Config).FillNulls(blank)
	t.Col(&c.Name).Size(255)
	t.Col(&c.DeletedAt).Index()
	// Declared, so the predicate is compiled into every read of this table
	// rather than written out at each call site. See the note in doc.go.
	t.SoftDelete(&c.DeletedAt)
}
