package sqlconnector

import (
	"context"
	"database/sql"
	"time"
)

// dialect is what differs between the servers a lookup can read.
type dialect interface {
	// label names the server in a message a person reads.
	label() string
	// open parses a connection string, holds its hosts to the operator's
	// policy, and opens a pool on it — forcing the settings a lookup depends on
	// whatever the string asked for.
	open(dsn string, hosts hostPolicy) (*sql.DB, error)
	// lexModes are the readings a query has to be clean under. The first is
	// the server's default, and is the one parameters are found with.
	lexModes() []lexMode
	// placeholders says how the server wants a parameter written.
	placeholders() placeholderStyle
	// begin opens the transaction a lookup runs in and applies its time limit.
	begin(ctx context.Context, db *sql.DB, limit time.Duration) (*sql.Tx, error)
}

// placeholderStyle is how one server spells a parameter.
type placeholderStyle struct {
	// format writes the placeholder for the nth distinct parameter, from 1.
	format func(ordinal int) string
	// reuse says a parameter named twice is one placeholder written twice.
	// MySQL's ? cannot be reused: each one takes the next value.
	reuse bool
	// arg wraps a value for the driver, for a server that binds by name.
	arg func(ordinal int, value any) any
}
