package app

import (
	"context"
	"fmt"

	"github.com/google/uuid"
	"github.com/gsoultan/metis/internal/pkg/config"
	"github.com/gsoultan/metis/internal/pkg/redaction"
	"github.com/gsoultan/metis/server/domains/entities"
	"github.com/gsoultan/metis/server/repositories/db"
	"github.com/gsoultan/metis/server/repositories/migrations"
	"github.com/gsoultan/metis/server/repositories/models"
	"github.com/rs/zerolog/log"
)

// openEnvironments connects to every enabled environment's database and brings
// its schema up to date, without serving any of them.
//
// For the maintenance commands that have to reach every database this
// installation holds — resealing after a key rotation is one. A server does not
// call this: it opens each environment as it starts serving it
// (serveEnvironments), through the same openEnvironmentDatabases.
//
// One database that will not open must not stop the others. A project with a
// development, staging and production runtime has three chances to be
// misconfigured, and refusing over any of them takes down the two that were
// fine — including production, over a development database somebody pointed at
// a laptop. So a failure is recorded against that environment and the rest
// carry on.
func (a *App) openEnvironments(ctx context.Context) error {
	rows, err := a.repo.Environment().ListAll(entities.WithSystemContext(ctx))
	if err != nil {
		return fmt.Errorf("could not read the environments to open: %w", err)
	}

	var opened, skipped, failed int
	for _, row := range rows {
		if !row.Enabled {
			skipped++
			continue
		}
		if err := a.openEnvironmentDatabases(ctx, row); err != nil {
			failed++
			// Named, at error level, with the environment: this is the message
			// somebody reads when a runtime is missing, and "which one" is the
			// first thing they need.
			log.Error().Str("error", redaction.RedactError(err)).
				Str("environment", row.Name).
				Int("port", row.Port).
				Msg("This environment's database could not be opened; the others are unaffected.")
			continue
		}
		opened++
	}

	if len(rows) > 0 {
		log.Info().
			Int("opened", opened).
			Int("disabled", skipped).
			Int("failed", failed).
			Msg("Environments")
	}
	return nil
}

// openEnvironmentDatabases migrates one environment's database and registers
// its storm pool, so that work bound to the environment reaches it.
//
// GORM connects only to migrate and seed, and lets go before the pool that
// serves the environment is registered: a second pool held for the life of the
// process was one more set of server sessions per environment, idle, and read
// by nothing after boot.
func (a *App) openEnvironmentDatabases(ctx context.Context, row models.EnvironmentModel) error {
	id := uuid.UUID(row.ID)
	if err := a.migrateEnvironment(ctx, row); err != nil {
		return err
	}
	if err := a.openEnvironmentStorm(ctx, row, id); err != nil {
		a.closeEnvironmentConnections(id)
		return err
	}
	return nil
}

// closeEnvironmentConnections lets go of one environment's database. Work
// bound to the environment is refused from here on, with
// ErrEnvironmentUnavailable rather than a fall back to the main database.
func (a *App) closeEnvironmentConnections(id uuid.UUID) {
	if a.storm != nil {
		if pool, open := a.storm.ForgetEnvironment(id); open {
			pool.Close()
		}
	}
}

// migrateEnvironment connects to one environment's database, migrates it,
// seeds its identity and disconnects.
//
// The schema is the same one the main database gets. An environment holds only
// the runtime — deployed models, instances, tasks — so the identity tables it
// also creates stay empty; leaving them there costs nothing and means one
// migration list rather than two lists that would drift.
func (a *App) migrateEnvironment(ctx context.Context, row models.EnvironmentModel) error {
	dsn, err := environmentDSN(row)
	if err != nil {
		return err
	}

	if !config.SupportedDriver(row.Driver) {
		return fmt.Errorf("this runs on PostgreSQL; %q is not a database engine it supports", row.Driver)
	}
	pool, err := db.NewPool(ctx, config.PostgresURL(dsn))
	if err != nil {
		return err
	}
	defer pool.Close()
	// Opening does not contact the server, so without this a database that is
	// simply not there would be registered as ready and fail at the first
	// request, instead of here, where the failure is reported and retried.
	if err := pool.Ping(ctx); err != nil {
		return fmt.Errorf("the database did not answer: %w", err)
	}

	// Migrations rewrite rows across every tenant in this runtime, which is
	// what they are for.
	migrateCtx := entities.WithSystemContext(ctx)
	result, err := migrations.Run(migrateCtx, pool, migrations.Steps())
	if err != nil {
		return fmt.Errorf("could not migrate this environment's database: %w", err)
	}
	if result.Created || len(result.Applied) > 0 {
		log.Info().
			Str("environment", row.Name).
			Bool("created", result.Created).
			Ints64("applied", result.Applied).
			Msg("Environment database migrated")
	}

	// Without the project and organization rows, every tenant-scoped query in
	// this database joins an empty table and matches nothing. See
	// SeedEnvironmentIdentity.
	projectID := uuid.UUID(row.ProjectID)
	if projectID == uuid.Nil {
		return errNoProject
	}
	project, organization, err := a.identityFor(ctx, projectID)
	if err != nil {
		return err
	}
	return SeedEnvironmentIdentity(migrateCtx, pool, project, organization)
}

// environmentDSN builds the connection string for a stored environment.
//
// The stored map is read back decrypted — EncryptedMap does that at the
// persistence boundary — so this is the point where a database password exists
// in memory. It goes straight into a DSN and is not logged: every log line
// about an environment names it and its port, never its connection.
func environmentDSN(row models.EnvironmentModel) (string, error) {
	fields := config.DatabaseFields{
		Host:       stringField(row.Connection, "host"),
		Port:       intField(row.Connection, "port"),
		Username:   stringField(row.Connection, "username"),
		Password:   stringField(row.Connection, "password"),
		DBName:     stringField(row.Connection, "db_name"),
		SSLEnabled: boolField(row.Connection, "ssl_enabled"),
	}
	if fields.DBName == "" {
		return "", fmt.Errorf("this environment names no database")
	}
	return config.BuildConnectionString(row.Driver, fields), nil
}

// The three readers below take the comma-ok form on purpose. The connection map
// is stored as JSON and comes back with whatever shape it was written with — a
// port that went in as an int returns as a float64 after a round trip — so a
// bare assertion here is a panic at boot on a row somebody edited by hand.
func stringField(m models.EncryptedMap, key string) string {
	if value, ok := m[key].(string); ok {
		return value
	}
	return ""
}

func intField(m models.EncryptedMap, key string) int {
	switch value := m[key].(type) {
	case int:
		return value
	case int64:
		return int(value)
	case float64:
		return int(value)
	}
	return 0
}

func boolField(m models.EncryptedMap, key string) bool {
	value, ok := m[key].(bool)
	return ok && value
}

// openEnvironmentStorm opens the pgx pool the ported repositories read through.
//
// A second pool onto the same database rather than a shared one, because the
// two layers speak different drivers. They are opened from the same stored
// connection so they cannot disagree about which database that is.
func (a *App) openEnvironmentStorm(ctx context.Context, row models.EnvironmentModel, id uuid.UUID) error {
	if a.storm == nil {
		return nil
	}
	dsn, err := environmentDSN(row)
	if err != nil {
		return err
	}
	pool, err := db.NewPool(ctx, config.PostgresURL(dsn))
	if err != nil {
		return fmt.Errorf("could not open this environment's storm connection: %w", err)
	}
	if previous := a.storm.RegisterEnvironment(id, pool); previous != nil {
		previous.Close()
	}
	return nil
}
