package db

import (
	"context"

	"github.com/google/uuid"
	"github.com/gsoultan/metis/server/domains/entities"
	"github.com/gsoultan/storm/runtime"
)

type (
	txKey          struct{}
	environmentKey struct{}
)

// withTx puts an open transaction on the context, so everything downstream
// joins it rather than opening its own.
func withTx(ctx context.Context, tx runtime.Executor) context.Context {
	return context.WithValue(ctx, txKey{}, tx)
}

func txFrom(ctx context.Context) (runtime.Executor, bool) {
	tx, ok := ctx.Value(txKey{}).(runtime.Executor)
	return tx, ok
}

// InTransaction reports whether ctx carries an open transaction.
//
// TransactMain masks a transaction the caller holds on another database by
// putting a nil one on the context, to mean there is none. This reads that as
// none, and so does a context that never had one.
func InTransaction(ctx context.Context) bool {
	tx, ok := txFrom(ctx)
	return ok && tx != nil
}

// WithEnvironment binds work to one of a project's runtimes.
//
// Applied by the listener a request arrived on: each environment is served on
// its own port, so which runtime a caller is working in is decided by where
// they connected, not by anything they send. An environment named in a header
// would be a value the caller chooses, and choosing production is exactly what
// a staging user must not be able to do.
func WithEnvironment(ctx context.Context, environmentID uuid.UUID) context.Context {
	return context.WithValue(ctx, environmentKey{}, environmentID)
}

// EnvironmentFrom reports which runtime this work belongs to. Absent means the
// main database, which is where accounts, projects and the environment registry
// itself live.
func EnvironmentFrom(ctx context.Context) (uuid.UUID, bool) {
	id, ok := ctx.Value(environmentKey{}).(uuid.UUID)
	return id, ok && id != uuid.Nil
}

// Bind attaches an environment to work, for both persistence layers at once.
//
// There are two bindings while the port is under way — this package's, read by
// the storm repositories, and entities', read by the GORM ones — and a context
// carrying only one reaches the right database through half the code and the
// main database through the other half. That is not a hypothetical: it showed
// up as a foreign key violation, where a definition had been written to an
// environment and the instance referencing it went to main.
//
// So callers bind through here rather than either package directly.
func Bind(ctx context.Context, environmentID uuid.UUID) context.Context {
	return WithEnvironment(entities.WithEnvironment(ctx, environmentID), environmentID)
}
