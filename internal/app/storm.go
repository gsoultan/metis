package app

import (
	"context"
	"fmt"
	"strings"

	"github.com/gsoultan/metis/internal/pkg/config"
	"github.com/gsoultan/metis/internal/pkg/envvar"
	"github.com/gsoultan/metis/internal/pkg/peopletoken"
	servicecontracts "github.com/gsoultan/metis/server/domains/services/contracts"
	serviceimpl "github.com/gsoultan/metis/server/domains/services/impl"
	"github.com/gsoultan/metis/server/repositories/db"
	"github.com/gsoultan/metis/server/repositories/model"
	"github.com/gsoultan/metis/server/repositories/pg"
	"github.com/gsoultan/storm"
	"github.com/gsoultan/storm/schema"
	"github.com/rs/zerolog/log"
)

// openStorm connects the repositories to the installation's database.
//
// Opened before the migrations run, because they run through it.
func (a *App) openStorm(ctx context.Context) error {
	dsn, err := a.resolveDSN()
	if err != nil {
		return err
	}
	conn, err := db.Open(ctx, dsn)
	if err != nil {
		return fmt.Errorf("could not open the database: %w", err)
	}
	a.storm = conn
	log.Info().Msg("Database connection open")
	return nil
}

// ensureStormSchema reports how the tables differ from the model, and seeds
// the platform roles.
//
// Both run at every boot: an installation upgraded into the roles has none,
// and an upgrade that needs somebody to remember a manual step is an upgrade
// that half the installations will not have had.
func (a *App) ensureStormSchema(ctx context.Context) error {
	want, err := stormModel()
	if err != nil {
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
	// The migrations leave every database in the model's shape, so there is
	// none unless somebody changed a table by hand. What would break a read or
	// a write is a column the one has and the other does not; the rest is said
	// at debug, because a warning per difference at every boot is how
	// somebody learns to scroll past the one that matters.
	var breaking []string
	for _, statement := range drift {
		if strings.Contains(statement, "ADD COLUMN") || strings.Contains(statement, "DROP COLUMN") {
			breaking = append(breaking, statement)
		}
		log.Debug().Str("statement", statement).Msg("Schema drift")
	}
	a.schemaDrift.Store(int64(len(breaking)))
	switch {
	case len(breaking) > 0:
		log.Warn().Strs("statements", breaking).Int("total", len(drift)).
			Msg("A table is missing a column the model names. Queries against it will fail; reconcile it with a migration.")
	case len(drift) > 0:
		log.Info().Int("differences", len(drift)).
			Msg("The schema differs from the model in ways that do not affect a query (types, nullability, index names). Run with debug logging to list them.")
	}
	// Refusing is opt-in. The default stays a warning because an operator
	// restarting a service at 3am should not be blocked by a column the running
	// code may never read — but an installation that would rather fail loudly at
	// deploy time than serve a broken feature can say so.
	if len(breaking) > 0 && refuseSchemaDrift() {
		return fmt.Errorf("refusing to start: %d columns the model names are missing and %s is set",
			len(breaking), envRefuseSchemaDrift)
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

// resolveDSN answers which database this installation uses: the one its
// config.yaml names, or else DATABASE_URL.
//
// An unreadable config is a refusal, not a warning. It used to log and fall
// through — and with no DATABASE_URL set, "fall through" meant creating a
// fresh, empty SQLite file and serving from it. The process then passed its own
// readiness probe while every list in the product was empty and every write
// went somewhere nobody would look for it. An installation that has been set up
// has a database; failing to read which one is not a reason to invent a
// different answer.
func (a *App) resolveDSN() (string, error) {
	if config.Exists(config.DefaultConfigPath) {
		cfg, err := config.Load(config.DefaultConfigPath)
		if err != nil {
			return "", fmt.Errorf(
				"could not read %s, and this installation has been set up, so its database is the one "+
					"named there. Fix the file or move it aside to run setup again: %w",
				config.DefaultConfigPath, err)
		}
		return dsnFromConfig(cfg)
	}

	if dsn := envvar.Get("DATABASE_URL"); dsn != "" {
		log.Info().Msg("Using PostgreSQL database from DATABASE_URL...")
		return dsn, nil
	}

	// There is no file to fall back to. A server deployment that meant to set
	// DATABASE_URL and did not used to come up healthy and empty on one, which
	// is the worst way to be misconfigured: nothing to read, no error, and a
	// readiness probe that passes. The setup wizard is served by a running
	// server, so it cannot be the way out of this: it creates the first
	// organization and administrator in the database DATABASE_URL names.
	return "", fmt.Errorf(
		"no database is configured. Set DATABASE_URL to a PostgreSQL connection string, "+
			"or restore the %s a setup wrote; the setup wizard then runs on that database",
		config.DefaultConfigPath)
}

// dsnFromConfig reads the connection a config.yaml stores.
func dsnFromConfig(cfg *config.Config) (string, error) {
	// Same precedence as setupEncryption: the key stored alongside the data
	// wins, because it is the one the data was encrypted with.
	encKey := cfg.EncryptionKey
	if encKey == "" {
		encKey = envvar.Get("ENCRYPTION_KEY")
	}
	dsn, err := cfg.DecryptConnectionString(encKey)
	if err != nil {
		return "", fmt.Errorf(
			"could not decrypt the database connection string in %s — this almost always means the "+
				"encryption key does not match the one used when the system was set up: %w",
			config.DefaultConfigPath, err)
	}
	// Refused rather than defaulted. This used to fall through to SQLite for
	// anything it did not recognise, so an installation whose config named MySQL
	// would come up on a fresh empty file beside its real data — every process,
	// task and definition apparently gone, with a successful startup log.
	if !config.SupportedDriver(cfg.Database.Driver) {
		return "", fmt.Errorf(
			"this runs on PostgreSQL; %q is not a database engine it supports. "+
				"Migrate the data to PostgreSQL and point %s at it; see docs/upgrading.md",
			cfg.Database.Driver, config.DefaultConfigPath)
	}
	log.Info().Msg("Using PostgreSQL database from config...")
	return dsn, nil
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

// peopleSignInService builds the People sign-in, or nothing.
//
// Nothing without a People directory (any engine but PostgreSQL) or without a
// key to seal tokens with; the facade then refuses every People sign-in with a
// reason rather than accepting one it cannot check.
func (a *App) peopleSignInService() servicecontracts.PeopleSignInService {
	if a.storm == nil {
		return serviceimpl.NewUnavailablePeopleSignInService(
			"people cannot sign in: the People directory needs PostgreSQL")
	}
	if a.peopleKeys == nil {
		return serviceimpl.NewUnavailablePeopleSignInService(
			"people cannot sign in: set " + peopletoken.EnvKey + " to enable it")
	}
	return serviceimpl.NewPeopleSignInService(pg.NewPersonSignInRepository(a.storm), a.peopleKeys,
		serviceimpl.WithPersonAPIKeys(pg.NewPersonAPIKeyRepository(a.storm)))
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
