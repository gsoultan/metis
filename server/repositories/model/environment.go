package model

import (
	"time"

	"github.com/gsoultan/storm"
)

// Environment is one runtime a project deploys into — dev, staging, production
// — and the database that runtime owns.
//
// This row lives in the main database. What the environment's own database
// holds is the runtime: deployed models, instances, tasks, jobs. Nothing is
// shared between two of them, which is stronger than scoping rows by a column
// because there is no query that could reach across even if one were written
// wrong.
type Environment struct {
	storm.Model

	Project Project

	Name string
	// Port is where this environment is served. Unique across the installation
	// rather than per project: two listeners cannot share one, and finding that
	// out at bind time takes the server down rather than one form.
	Port int
	// Driver is the database engine backing this runtime.
	Driver string
	// Connection holds host, port, username, password, db_name and ssl_enabled as
	// jsonb. Encrypted by the repository before it is written: a database
	// password is every credential in that runtime at once.
	// A string, not storm.JSON, and that is the whole point: what is stored is
	// ciphertext. A database password is worth more than any one connector's
	// token — it is every credential in that runtime at once — so the column
	// holds an encrypted blob, and declaring it jsonb would both fail to parse
	// and, on the day the column types are reconciled, destroy it.
	Connection string
	Enabled    bool

	DeletedAt *time.Time
}

func (e *Environment) Schema(t *storm.Table) {
	// GORM's defaults, kept. See fill.go.
	t.Col(&e.Port).Default(zero)
	// What a database GORM created holds where these are NULL: the value GORM
	// read a NULL as. See fill.go.
	t.Col(&e.Name).FillNulls(blank)
	t.Col(&e.Driver).FillNulls(blank)
	t.Col(&e.Connection).FillNulls(blank)
	t.Col(&e.Enabled).FillNulls(no)
	t.Col(&e.Name).Size(63)
	t.Col(&e.Driver).Size(32)
	// Both scoped to the live rows, which is what SoftDelete makes them: a
	// removed environment must not go on holding a port nothing serves, or a
	// name nobody can reuse.
	t.Unique(&e.Project, &e.Name)
	t.Col(&e.Port).Unique()
	t.Col(&e.DeletedAt).Index()
	// Declared, so the predicate is compiled into every read of this table
	// rather than written out at each call site. See the note in doc.go.
	t.SoftDelete(&e.DeletedAt)
}
