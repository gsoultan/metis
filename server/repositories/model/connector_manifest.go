package model

import (
	"time"

	"github.com/gsoultan/storm"
)

// ConnectorManifest is a connector described as a document rather than as Go
// code, so adding an integration does not mean redeploying the engine.
type ConnectorManifest struct {
	storm.Model

	Key     string
	Name    string
	Version int
	// Document is the manifest itself, YAML or JSON, as authored.
	Document string
	Enabled  bool

	DeletedAt *time.Time
}

func (c *ConnectorManifest) Schema(t *storm.Table) {
	// GORM's defaults, kept. See fill.go.
	t.Col(&c.Version).Default(zero)
	t.Col(&c.Enabled).Default(yes)
	// What a database GORM created holds where these are NULL: the value GORM
	// read a NULL as. See fill.go.
	t.Col(&c.Key).FillNulls(blank)
	t.Col(&c.Name).FillNulls(blank)
	t.Col(&c.Document).FillNulls(blank)
	t.Col(&c.Enabled).FillNulls(no)
	// 191 rather than 255: the unique index has to fit MySQL's key length under
	// utf8mb4, and the limit outlived the MySQL back end because shortening a
	// key later is a migration nobody wants.
	t.Col(&c.Key).Size(191)
	// Live rows only: a removed manifest's key is free again.
	t.Col(&c.Key).Unique()
	t.Col(&c.Name).Size(255)
	t.Col(&c.DeletedAt).Index()
	// Declared, so the predicate is compiled into every read of this table
	// rather than written out at each call site. See the note in doc.go.
	t.SoftDelete(&c.DeletedAt)
}
