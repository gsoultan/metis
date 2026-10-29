package entities

import (
	"context"

	"github.com/google/uuid"
)

type environmentContextKey struct{}

// WithEnvironment binds work to one of a project's runtimes.
//
// Applied by the listener a request arrived on: each environment is served on
// its own port, so which runtime a caller is working in is decided by where
// they connected, not by anything they send. That matters — an environment
// carried in a header or a body would be a value the caller chooses, and
// choosing production is exactly what a staging user must not be able to do.
//
// Everything downstream reads it through the repository layer: a query made
// under this context runs against that environment's database. See
// db.Conn.Executor.
func WithEnvironment(ctx context.Context, environmentID uuid.UUID) context.Context {
	return context.WithValue(ctx, environmentContextKey{}, environmentID)
}

// EnvironmentFrom reports which runtime this work belongs to.
//
// Absent means the main database — the organizations, projects, accounts and
// the environment registry itself all live there, and so does an installation
// that has defined no environments at all.
func EnvironmentFrom(ctx context.Context) (uuid.UUID, bool) {
	id, ok := ctx.Value(environmentContextKey{}).(uuid.UUID)
	return id, ok && id != uuid.Nil
}
