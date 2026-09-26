package app

import (
	"context"
	"fmt"
	"strings"

	"github.com/gsoultan/metis/internal/pkg/config"
	"github.com/gsoultan/metis/internal/pkg/envvar"
	servicecontracts "github.com/gsoultan/metis/server/domains/services/contracts"
	serviceimpl "github.com/gsoultan/metis/server/domains/services/impl"
	"github.com/gsoultan/metis/server/repositories/db"
	"github.com/gsoultan/metis/server/repositories/model"
	"github.com/gsoultan/metis/server/repositories/pg"
	"github.com/gsoultan/storm"
	"github.com/gsoultan/storm/schema"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/rs/zerolog/log"
)

// openStorm connects the storm repositories to the same database GORM is using.
//
// Both layers run side by side while the port happens: a repository that has
// moved reads through storm, the rest still read through GORM, and they are
// looking at one database either way. Deciding which is used happens once, at
// the composition root, so a repository can move without the service that calls
// it changing.
//
// Storm is PostgreSQL-only, so on any other engine there is simply no storm
// connection and the features built on it are unavailable — reported plainly at
// boot rather than as a failure at the first request. That is the honest state
// of a port in progress; it is not a silent fallback, because a fallback would
// mean two layers disagreeing about where the data is.
func (a *App) openStorm(ctx context.Context) error {
	_, dsn, err := a.resolveDSN()
	if err != nil {
		return err
	}
	if dsn == "" {
		// Unreachable from Run, which refuses to start without a database
		// (resolveDialector); kept so an App built without one has no storm
		// connection rather than a failed one.
		log.Info().Msg("No database configured; no storm connection.")
		return nil
	}

	conn, err := db.Open(ctx, dsn)
	if err != nil {
		return fmt.Errorf("could not open the storm connection: %w", err)
	}
	a.storm = conn
	log.Info().Msg("Storm connection open")
	return nil
}

// ensureStormSchema creates the tables only storm knows about, and seeds the
// platform roles.
//
// Runs after the GORM migrations, not with the connection that reaches them: it
// reconciles column defaults on tables those migrations create, and creating
// them first would be reconciling a schema that is one version behind.
//
// Both are idempotent and both run at every boot, for the same reason: an
// installation upgraded into these features has neither, and an upgrade that
// needs somebody to remember a manual step is an upgrade that half the
// installations will not have had.
func (a *App) ensureStormSchema(ctx context.Context) error {
	want, err := stormModel()
	if err != nil {
		return err
	}
	if err := ensureStormTables(ctx, a.storm.Main(), want); err != nil {
		return err
	}

	// Named, not fixed, and not fatal. An installation whose tables predate a
	// model change keeps working; what it must not do is keep working while
	// nobody can tell. See db.ReportDrift for why the alternative fails late and
	// somewhere else.
	drift, err := db.ReportDrift(ctx, a.storm.Main(), want)
	if err != nil {
		log.Warn().Err(err).Msg("Could not compare the storm tables against the model. Any drift will go unreported.")
	}
	// Reported as one line plus the statements that would break a query, not
	// one warning per difference.
	//
	// GORM's AutoMigrate and the storm model disagree about roughly sixty
	// columns on a fresh installation — text where the model says varchar,
	// nullable where it says NOT NULL, its own index names. None of it breaks a
	// read or a write, and sixty warnings at every boot is how somebody learns
	// to scroll past the one that would.
	if len(drift) > 0 {
		var breaking []string
		for _, statement := range drift {
			if strings.Contains(statement, "ADD COLUMN") || strings.Contains(statement, "DROP COLUMN") {
				breaking = append(breaking, statement)
			}
			log.Debug().Str("statement", statement).Msg("Schema drift")
		}
		if len(breaking) > 0 {
			log.Warn().Strs("statements", breaking).Int("total", len(drift)).
				Msg("A storm table is missing a column the model names. Queries against it will fail; reconcile it with a migration.")
		} else {
			log.Info().Int("differences", len(drift)).
				Msg("The schema differs from the model in ways that do not affect a query (types, nullability, index names). Run with debug logging to list them.")
		}
	}

	accounts := pg.NewPlatformUserRepository(a.storm)
	if err := accounts.EnsureBuiltInRoles(ctx); err != nil {
		return fmt.Errorf("could not create the built-in platform roles: %w", err)
	}
	return nil
}

// stormModel is the schema the storm repositories write against.
func stormModel() (*schema.Schema, error) {
	want, err := storm.Build(model.All()...)
	if err != nil {
		// The model layer not building is a programming error, not a
		// configuration one, and it is the same failure `make generate` would
		// have reported. Refusing to boot is right: the generated store was
		// built from a model that no longer holds.
		return nil, fmt.Errorf("the model layer does not build: %w", err)
	}
	return want, nil
}

// ensureStormTables gives one database what storm writes against: the tables
// only storm knows about, and the column defaults it leaves to the database.
//
// Every database the repositories reach needs it, the main one and each
// environment's. An environment's used to have only the GORM migrations, so
// the first job a process there scheduled failed on insert with a NULL where
// storm expected the database to fill one in.
func ensureStormTables(ctx context.Context, pool *pgxpool.Pool, want *schema.Schema) error {
	created, err := db.EnsureTables(ctx, pool, want)
	if err != nil {
		return fmt.Errorf("could not create the storm tables: %w", err)
	}
	if len(created) > 0 {
		log.Info().Strs("tables", created).Msg("Created tables for the storm repositories")
	}

	// The tables GORM made have no database defaults on the columns storm
	// expects the database to supply. Reconciled here rather than left to drift,
	// because the symptom is a decode panic on a NULL timestamp, or a refused
	// insert, rather than anything that reads like a schema problem.
	defaulted, err := db.EnsureColumnDefaults(ctx, pool, want)
	if err != nil {
		return fmt.Errorf("could not reconcile the storm column defaults: %w", err)
	}
	if len(defaulted) > 0 {
		log.Info().Strs("columns", defaulted).Msg("Gave shared columns the defaults storm writes against")
	}
	return nil
}

// resolveDSN answers which database this installation uses, in the same order
// resolveDialector does.
//
// Split out so GORM and storm cannot disagree about the answer. Two independent
// resolutions is how one layer ends up talking to the configured database and
// the other to a freshly created file beside it.
func (a *App) resolveDSN() (driver, dsn string, err error) {
	if config.Exists(config.DefaultConfigPath) {
		cfg, err := config.Load(config.DefaultConfigPath)
		if err != nil {
			return "", "", fmt.Errorf("could not read %s: %w", config.DefaultConfigPath, err)
		}
		encKey := cfg.EncryptionKey
		if encKey == "" {
			encKey = envvar.Get("ENCRYPTION_KEY")
		}
		connection, err := cfg.DecryptConnectionString(encKey)
		if err != nil {
			return "", "", fmt.Errorf("could not decrypt the connection string in %s: %w", config.DefaultConfigPath, err)
		}
		return cfg.Database.Driver, connection, nil
	}

	if url := envvar.Get("DATABASE_URL"); url != "" {
		return config.DriverPostgres, url, nil
	}
	// No config and no DATABASE_URL means an installation that has not been set
	// up. There is no file to fall back to any more: a database this creates
	// silently is one somebody starts using and then loses.
	return config.DriverPostgres, "", nil
}

// participantService builds the storm-backed participant directory, or nothing.
//
// Nothing is a supported answer: an installation not on PostgreSQL has no storm
// connection, and the facade substitutes a service that refuses with a reason
// rather than one that panics or quietly returns an empty directory.
func (a *App) participantService() servicecontracts.WorkflowUserService {
	if a.storm == nil {
		return nil
	}
	if a.participants == nil {
		a.participants = serviceimpl.NewWorkflowUserService(pg.NewWorkflowUserRepository(a.storm))
	}
	return a.participants
}

// participantSyncService builds the directory registry, or nothing.
//
// It shares the participant service rather than building a second one: the
// syncer writes through the same upserts a manual import does, and two of them
// would be two answers to what an import means.
func (a *App) participantSyncService(locker servicecontracts.DistributedLocker) servicecontracts.ParticipantSyncService {
	if a.storm == nil {
		return nil
	}
	directory := pg.NewWorkflowUserRepository(a.storm)
	return serviceimpl.NewParticipantSyncService(
		pg.NewParticipantSourceRepository(a.storm),
		serviceimpl.WorkflowUserServiceFor(a.participantService()),
		directory,
		locker,
	)
}

// platformUserService builds the account manager, or nothing.
//
// Nothing on any engine but PostgreSQL, like the participant directory. The
// facade substitutes a service that refuses with the reason, so an installation
// on SQLite keeps working — it simply manages its administrators the way it did
// before this page existed.
func (a *App) platformUserService() servicecontracts.PlatformUserService {
	if a.storm == nil {
		return nil
	}
	if a.accounts == nil {
		a.accounts = serviceimpl.NewPlatformUserService(pg.NewPlatformUserRepository(a.storm))
	}
	return a.accounts
}
