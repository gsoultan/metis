package app

import (
	"context"
	"crypto/rand"
	"errors"
	"flag"
	"fmt"
	"math/big"
	"net"
	"net/http"
	"net/http/pprof"
	"os"
	"os/exec"
	"os/signal"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/gsoultan/metis/internal/pkg/envvar"
	"github.com/gsoultan/metis/internal/pkg/secrets"

	"github.com/gsoultan/metis/internal/pkg/tracing"
	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"

	"github.com/google/uuid"
	pbservices "github.com/gsoultan/metis/api/proto/services"
	"github.com/gsoultan/metis/internal/pkg/auth"
	"github.com/gsoultan/metis/internal/pkg/config"
	"github.com/gsoultan/metis/internal/pkg/crypto"
	"github.com/gsoultan/metis/internal/pkg/health"
	"github.com/gsoultan/metis/internal/pkg/logger"
	"github.com/gsoultan/metis/internal/pkg/metrics"
	"github.com/gsoultan/metis/internal/pkg/redaction"
	"github.com/gsoultan/metis/server/domains/entities"
	"github.com/gsoultan/metis/server/domains/observers/impl"
	"github.com/gsoultan/metis/server/domains/services"
	serviceimpl "github.com/gsoultan/metis/server/domains/services/impl"
	"github.com/gsoultan/metis/server/endpoints"
	"github.com/gsoultan/metis/server/interceptors"
	authinterceptor "github.com/gsoultan/metis/server/interceptors/auth"
	"github.com/gsoultan/metis/server/interceptors/tenant"
	"github.com/gsoultan/metis/server/repositories"
	gorms "github.com/gsoultan/metis/server/repositories/gorms"
	"github.com/gsoultan/metis/server/repositories/migrations"
	models "github.com/gsoultan/metis/server/repositories/models"
	"github.com/gsoultan/metis/server/transports/grpcs"
	https "github.com/gsoultan/metis/server/transports/https"

	"github.com/glebarez/sqlite"
	"github.com/rs/zerolog/log"
	"golang.org/x/sync/errgroup"
	"google.golang.org/grpc"
	"gorm.io/driver/mysql"
	"gorm.io/driver/postgres"
	"gorm.io/driver/sqlserver"
	"gorm.io/gorm"
)

// version identifies this build in traces and, later, anywhere else that needs
// to say which build produced something.
//
// Set at build time with:
//
//	go build -ldflags "-X github.com/gsoultan/metis/internal/app.version=$(git describe --tags --always)"
//
// It defaults to "dev" rather than a fake version number, because a trace
// labelled with a version that was never released is worse than one that admits
// it does not know.
var version = "dev"

type App struct {
	db         *gorm.DB
	repo       repositories.Repository
	svc        services.ServiceFacade
	sse        *impl.SSEObserver
	validator  *auth.TokenValidator
	initDBOnce func()
}

const (
	defaultHTTPMaxBodyBytes        int64 = 2 << 20
	defaultHTTPMaxRequestsPerLimit       = 240
	defaultHTTPMaxInFlightRequests       = 128
	defaultHTTPMaxQueuedRequests         = 256
	defaultHTTPIdempotencyTTL            = 15 * time.Minute
	httpShutdownTimeout                  = 5 * time.Second
	defaultHTTPReadHeaderTimeout         = 2 * time.Second
	defaultHTTPIdleTimeout               = 120 * time.Second
	defaultHTTPMaxHeaderBytes            = 1 << 20
	defaultPprofAddress                  = "127.0.0.1:6060"
	envPprofEnabled                      = "METIS_PPROF_ENABLED"
	envPprofAddress                      = "METIS_PPROF_ADDRESS"

	// Metrics listen on their own address, away from the public API, so a
	// scrape endpoint is never published alongside it. The default binds to
	// loopback: a deployment that needs to be scraped across a pod network must
	// say so explicitly rather than being exposed by accident.
	//
	// 9464 is the registered OpenTelemetry Prometheus exporter port. Using 9090
	// would collide with a Prometheus running on the same host.
	defaultMetricsAddress = "127.0.0.1:9464"
	envMetricsEnabled     = "METIS_METRICS_ENABLED"
	envMetricsAddress     = "METIS_METRICS_ADDRESS"

	defaultHTTPAddress = ":8080"
	defaultGRPCAddress = ":8081"
	envHTTPAddress     = "METIS_HTTP_ADDRESS"
	envGRPCAddress     = "METIS_GRPC_ADDRESS"

	// envResetPassword supplies the new password for --reset-password, so that
	// a chosen one need not be typed where it will be recorded.
	envResetPassword = "METIS_NEW_PASSWORD"
)

// resolveAddress returns the listen address from env, falling back to a
// default. Both were previously hardcoded, so two instances could not run side
// by side and a container could not be told which port to publish.
func resolveAddress(envVar, fallback string) string {
	if v := strings.TrimSpace(envvar.Get(envVar)); v != "" {
		return v
	}
	return fallback
}

func profilingEnabled() bool {
	enabledValue, exists := envvar.Lookup(envPprofEnabled)
	if !exists {
		return false
	}

	enabled, err := strconv.ParseBool(enabledValue)
	if err != nil {
		log.Warn().
			Err(err).
			Str("env", envPprofEnabled).
			Msg("Ignoring invalid pprof enable value")
		return false
	}

	return enabled
}

func resolvePprofAddress() string {
	address := strings.TrimSpace(envvar.Get(envPprofAddress))
	if address == "" {
		return defaultPprofAddress
	}
	return address
}

// metricsEnabled reports whether to serve the scrape endpoint. Unlike pprof
// this defaults to on: an SLO nobody is measuring is not an objective, and the
// endpoint is bound to loopback unless configured otherwise.
func metricsEnabled() bool {
	enabledValue, exists := envvar.Lookup(envMetricsEnabled)
	if !exists {
		return true
	}

	enabled, err := strconv.ParseBool(enabledValue)
	if err != nil {
		log.Warn().
			Err(err).
			Str("env", envMetricsEnabled).
			Msg("Ignoring invalid metrics enable value")
		return true
	}

	return enabled
}

func newPprofHandler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /debug/pprof/", pprof.Index)
	mux.HandleFunc("GET /debug/pprof/cmdline", pprof.Cmdline)
	mux.HandleFunc("GET /debug/pprof/profile", pprof.Profile)
	mux.HandleFunc("GET /debug/pprof/symbol", pprof.Symbol)
	mux.HandleFunc("GET /debug/pprof/trace", pprof.Trace)

	for _, profile := range []string{"allocs", "block", "goroutine", "heap", "mutex", "threadcreate"} {
		mux.Handle("GET /debug/pprof/"+profile, pprof.Handler(profile))
	}

	return mux
}

// sqliteDSNWithBusyTimeout gives a SQLite DSN a lock-wait budget.
//
// Without one, SQLite answers a locked database with SQLITE_BUSY immediately,
// and the very first install hits it: the job worker polls every two seconds,
// so an API write racing one poll returned "database is locked (5)" to the
// user. Five seconds of patience is the difference between a working install
// and one that fails on its second request.
func sqliteDSNWithBusyTimeout(dsn string) string {
	if strings.Contains(dsn, "_pragma=busy_timeout") {
		return dsn
	}
	separator := "?"
	if strings.Contains(dsn, "?") {
		separator = "&"
	}
	return dsn + separator + "_pragma=busy_timeout(5000)"
}

// serializeSQLitePool caps a SQLite connection pool at one connection.
//
// SQLite is a single-writer file database, and a pool pretends otherwise. Two
// pooled connections inside transactions deadlock the classic way: one holds a
// read lock and asks to write while the other holds the write lock — and for
// that upgrade SQLite returns SQLITE_BUSY immediately, busy_timeout ignored by
// design. The very first install hit it: the job worker polls in transactions
// every two seconds, so the second API write raced one and failed with
// "database is locked". One connection makes the pool tell the truth. The
// server databases — Postgres, MySQL, SQL Server — keep their real pools.
func serializeSQLitePool(db *gorm.DB) {
	if db.Name() != "sqlite" {
		return
	}
	if sqlDB, err := db.DB(); err == nil {
		sqlDB.SetMaxOpenConns(1)
	}
}

func newHTTPServer(address string, handler http.Handler) *http.Server {
	return &http.Server{
		Addr:              address,
		Handler:           handler,
		ReadHeaderTimeout: defaultHTTPReadHeaderTimeout,
		IdleTimeout:       defaultHTTPIdleTimeout,
		MaxHeaderBytes:    defaultHTTPMaxHeaderBytes,
	}
}

func New() *App {
	return &App{
		initDBOnce: sync.OnceFunc(func() {
			log.Info().Msg("Initializing database connection...")
		}),
	}
}

func (a *App) Run() error {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	// 0. Flag Parsing
	buildUI := flag.Bool("build-ui", false, "Build the UI using bun")
	resetPassword := flag.String("reset-password", "", "Set a new password for the named user, then exit")
	flag.Parse()

	if *buildUI {
		return a.handleBuildUI(ctx)
	}

	// 0. Initialize Logger
	logger.Init()

	// Tracing before anything that might be worth tracing. It is off unless an
	// OTLP endpoint is configured, and the shutdown function is safe to call
	// either way, so there is no conditional cleanup to get wrong.
	// The liveness probe reports the build, so "which version is running" has an
	// answer that does not depend on finding the startup log.
	health.SetBuildVersion(version)

	shutdownTracing, err := tracing.Init(ctx, version)
	if err != nil {
		return fmt.Errorf("failed to start tracing: %w", err)
	}
	defer shutdownTracing(context.Background())

	a.initDBOnce()

	// 1. Install the at-rest encryption key before any repository can read or
	//    write an encrypted column.
	if err := a.setupEncryption(); err != nil {
		return err
	}

	// 2. Initialize DB with GORM
	if err := a.setupDatabase(); err != nil {
		return err
	}

	// 3. Initialize Domain
	if err := a.setupService(ctx); err != nil {
		return err
	}

	// A password reset is a maintenance task, not a server. It runs against the
	// configured database and then exits, without opening a port — an operator
	// doing this has usually been locked out, and starting a server they cannot
	// log into would not help.
	if *resetPassword != "" {
		return a.handleResetPassword(ctx, *resetPassword)
	}

	// 4. Setup Transports
	a.setupAuth(ctx)

	// 5. Start Servers using errgroup
	return a.runServers(ctx)
}

// handleResetPassword sets a new password for one account and prints it.
//
// There was no way to change a password at all, so a forgotten one had no
// answer: there is no default account by design, and an installation with a
// single administrator was simply unreachable. Reading it from the environment
// keeps a chosen password out of the shell history; without one, a strong
// password is generated, which is the better default for a recovery step.
func (a *App) handleResetPassword(ctx context.Context, username string) error {
	password, generated := envvar.Get(envResetPassword), false
	if password == "" {
		var err error
		if password, err = generatePassword(); err != nil {
			return fmt.Errorf("could not generate a password: %w", err)
		}
		generated = true
	}

	if err := a.svc.SetPassword(ctx, username, password); err != nil {
		return err
	}

	fmt.Printf("Password updated for %q.\n", username)
	if generated {
		fmt.Printf("New password: %s\n", password)
		fmt.Println("Sign in with it and change it; it is on screen and in this terminal's scrollback.")
	}
	return nil
}

// generatePassword returns a password from the crypto/rand source.
//
// The alphabet omits characters that are read wrongly when a password is copied
// off a screen — no O/0, l/1/I — because this one is going to be.
func generatePassword() (string, error) {
	const alphabet = "abcdefghijkmnopqrstuvwxyzABCDEFGHJKLMNPQRSTUVWXYZ23456789"
	const length = 20

	out := make([]byte, length)
	for i := range out {
		n, err := rand.Int(rand.Reader, big.NewInt(int64(len(alphabet))))
		if err != nil {
			return "", err
		}
		out[i] = alphabet[n.Int64()]
	}
	return string(out), nil
}

func (a *App) handleBuildUI(ctx context.Context) error {
	fmt.Println("Building UI...")
	if _, err := exec.LookPath("bun"); err != nil {
		return fmt.Errorf("'bun' not found in PATH. Please install Bun to build the UI")
	}
	// Bound by the signal context, so Ctrl-C stops the build rather than
	// leaving bun running after this process has gone.
	cmd := exec.CommandContext(ctx, "bun", "run", "build")
	cmd.Dir = "ui"
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("error building UI: %w", err)
	}
	fmt.Println("UI build successful!")
	return nil
}

// setupEncryption installs the AES key used for process/task variables at rest.
//
// Resolution mirrors resolveJWTSecret: ENCRYPTION_KEY wins, then config.yaml.
// When the system is configured (config.yaml present) and neither supplies a
// key, startup fails rather than falling back to a default — the package
// previously shipped a hardcoded literal key, which meant unconfigured
// deployments encrypted business data with a value published in the repository.
//
// Pre-setup (no config.yaml) is allowed to start without a key: no process data
// exists yet, and the setup flow collects the key before anything is written.
// envAllowWeakSecrets lets an installation start with a secret that would
// otherwise be refused.
//
// It exists because refusing outright can be worse than the weakness. A weak
// ENCRYPTION_KEY has no safe remedy: rotating it does not re-encrypt anything,
// it just makes every stored variable unreadable. So an operator who discovers
// this on an upgrade needs a way to boot while they plan a re-encryption,
// rather than an outage and a key they cannot change.
const envAllowWeakSecrets = "METIS_ALLOW_WEAK_SECRETS"

// requireStrongSecret refuses a secret that would not survive an offline guess.
//
// Both secrets were previously accepted on the sole condition of being
// non-empty, and both fail silently when weak: a guessable JWT_SECRET is forged
// into an administrator's token, and a guessable ENCRYPTION_KEY turns a stolen
// backup back into plaintext. Nothing errors at the time — the system behaves
// exactly as though the secret were strong, which is why this has to be checked
// rather than trusted.
func requireStrongSecret(name, value string) error {
	err := secrets.Validate(name, value)
	if err == nil {
		return nil
	}

	if allowed, parseErr := strconv.ParseBool(envvar.Get(envAllowWeakSecrets)); parseErr == nil && allowed {
		// Every boot, not once: this is a standing condition, and a warning
		// printed the day it was set is a warning nobody who inherits the
		// system will ever see.
		log.Warn().Err(err).Str("setting", name).Str("override", envAllowWeakSecrets).
			Msg("Starting with a weak secret because the override is set. This is not a safe steady state.")
		return nil
	}

	return fmt.Errorf("%w\n\n"+
		"Refusing to start. A weak %s is not a degraded mode — it is indistinguishable from a strong one\n"+
		"until somebody guesses it offline, and then it is total.\n\n"+
		"If this is an existing installation, note that ENCRYPTION_KEY cannot simply be changed: rotating it\n"+
		"does not re-encrypt anything, it makes existing variables unreadable. To start anyway while you\n"+
		"plan a re-encryption, set %s=true", err, name, envAllowWeakSecrets)
}

func (a *App) setupEncryption() error {
	envKey := envvar.Get("ENCRYPTION_KEY")

	// A configured system's key is whatever its data was actually encrypted
	// with, so config.yaml wins over the environment.
	//
	// The usual precedence is the other way round, but here it produced a
	// confusing failure: a stale or unrelated ENCRYPTION_KEY silently replaced
	// the real one and the server died at startup with "cipher: message
	// authentication failed", which names the symptom and not the cause. The
	// environment variable is for deployments that keep no key in the file at
	// all; once one is in the file, disagreeing with it is a misconfiguration
	// rather than an override.
	if config.Exists(config.DefaultConfigPath) {
		cfg, err := config.Load(config.DefaultConfigPath)
		if err == nil && cfg.EncryptionKey != "" {
			if envKey != "" && envKey != cfg.EncryptionKey {
				log.Warn().Msg(
					"ENCRYPTION_KEY differs from the key in config.yaml; using the key from config.yaml, " +
						"because that is what the existing data was encrypted with. " +
						"To rotate the key, re-encrypt the data first.")
			}
			if err := requireStrongSecret("encryption_key in config.yaml", cfg.EncryptionKey); err != nil {
				return err
			}
			if err := crypto.Configure(cfg.EncryptionKey); err != nil {
				return fmt.Errorf("invalid encryption_key in config.yaml: %w", err)
			}
			log.Info().Msg("At-rest encryption key loaded from config.yaml")
			return nil
		}

		if envKey != "" {
			if err := requireStrongSecret("ENCRYPTION_KEY", envKey); err != nil {
				return err
			}
			if err := crypto.Configure(envKey); err != nil {
				return fmt.Errorf("invalid ENCRYPTION_KEY: %w", err)
			}
			log.Info().Msg("At-rest encryption key loaded from ENCRYPTION_KEY")
			return nil
		}

		return errors.New(
			"config.yaml is present but carries no encryption_key, and ENCRYPTION_KEY is not set; " +
				"process and task variables are encrypted at rest and cannot be read or written without it",
		)
	}

	if envKey != "" {
		// Checked here too, and this is the path that matters most: a fresh
		// installation is the one moment the key can still be chosen freely,
		// because nothing has been encrypted with it yet.
		if err := requireStrongSecret("ENCRYPTION_KEY", envKey); err != nil {
			return err
		}
		if err := crypto.Configure(envKey); err != nil {
			return fmt.Errorf("invalid ENCRYPTION_KEY: %w", err)
		}
		log.Info().Msg("At-rest encryption key loaded from ENCRYPTION_KEY")
		return nil
	}

	log.Warn().Msg("No ENCRYPTION_KEY set and no config.yaml found; running in pre-setup mode (no encrypted data can be written yet)")
	return nil
}

func (a *App) setupDatabase() error {
	dialector, err := a.resolveDialector()
	if err != nil {
		return err
	}
	db, err := gorm.Open(dialector, gorms.Config())
	if err != nil {
		return fmt.Errorf("failed to open db: %w", err)
	}
	serializeSQLitePool(db)
	a.db = db

	return a.migrate()
}

func (a *App) migrate() error {
	// Migrations and the data repairs behind them rewrite rows across every
	// tenant, which is exactly what they are for.
	ctx := entities.WithSystemContext(context.Background())

	result, err := migrations.Run(ctx, a.db, a.migrationList())
	if err != nil {
		return fmt.Errorf("failed to migrate db: %w", err)
	}
	log.Info().
		Ints("applied", result.Applied).
		Int("alreadyApplied", result.Skipped).
		Msg("Database migrations complete")

	a.reportSchemaDrift()
	return nil
}

// migrationList is the full ordered set: the schema migrations, plus the data
// repairs that need the repository layer.
//
// Both repairs used to run on every single boot. They are one-time upgrades —
// they fix rows written by older versions of the engine, and nothing writes
// those shapes any more — so every boot after the first scanned the whole table
// to find nothing. BackfillEngineBookkeeping was the worse of the two: it calls
// Process().List, loading every process instance ever created into memory, which
// on a mature database is a startup that keeps getting slower and never has
// anything to do.
func (a *App) migrationList() []migrations.Migration {
	// Each data migration builds its repository over the *transaction* the
	// runner hands it, not over a.db. The first version captured an outer
	// repository, which had two consequences: the backfills' writes never
	// actually joined the transaction their Transactional flag promised, and
	// once SQLite's pool was capped at one connection the repository's request
	// for a second one deadlocked against the migration's own transaction —
	// a hang, at boot, on the very first install.
	return append(migrations.Schema(models.MigrationModels()), []migrations.Migration{
		{
			Version:       2,
			Name:          "resolve templated message correlation keys",
			Transactional: true,
			// Message subscriptions written before correlation keys were
			// resolved per instance still hold the raw ${...} template, which no
			// inbound correlation value can match. Until they are repaired every
			// instance waiting on a message event hangs.
			Run: func(ctx context.Context, tx *gorm.DB) error {
				_, err := serviceimpl.BackfillMessageCorrelationKeys(ctx, repositories.NewRepository(tx))
				return err
			},
		},
		{
			Version:       3,
			Name:          "move engine bookkeeping out of business variables",
			Transactional: true,
			// Multi-instance progress and gateway join counts moved out of the
			// business variable namespace into their own columns. An instance
			// part-way through either would otherwise come back with nothing
			// recorded: restarting its iterations from zero, or forgetting the
			// branches that had already reached a waiting gateway.
			Run: func(ctx context.Context, tx *gorm.DB) error {
				_, err := serviceimpl.BackfillEngineBookkeeping(ctx, repositories.NewRepository(tx))
				return err
			},
		},
	}...)
}

// reportSchemaDrift warns when a model declares a column the database lacks.
//
// Adding a field to a model used to be enough, because AutoMigrate ran on every
// boot. It is not any more, and forgetting the migration fails at the first
// request that touches the column rather than at startup — so startup says so.
// A warning rather than a refusal: an operator restarting a service at 3am
// should not be blocked by a column the running code may never read.
func (a *App) reportSchemaDrift() {
	drift, err := migrations.DriftReport(a.db, models.MigrationModels())
	if err != nil {
		log.Warn().Err(err).Msg("Could not check the schema against the models")
		return
	}
	for _, item := range drift {
		log.Warn().Str("drift", item).Msg("Schema drift: a model change is missing its migration")
	}
}

func (a *App) setupService(ctx context.Context) error {
	a.repo = repositories.NewRepository(a.db)

	dispatcher := impl.NewEventDispatcher()
	dispatcher.Register(impl.NewAuditLogObserver(a.repo.Audit()))
	a.sse = impl.NewSSEObserver()
	dispatcher.Register(a.sse)

	// Register Webhook Observer if endpoints are provided
	webhookEndpoints := envvar.Get("WEBHOOK_ENDPOINTS")
	if webhookEndpoints != "" {
		endpointsList := strings.Split(webhookEndpoints, ",")
		dispatcher.Register(impl.NewWebhookObserver(endpointsList))
		log.Info().Int("count", len(endpointsList)).Msg("Registered Webhook Observer")
	}

	jwtSecret, err := a.resolveJWTSecret()
	if err != nil {
		return err
	}
	a.svc = services.NewServiceFacade(a.repo, dispatcher, a.sse, jwtSecret, func(targetDB *gorm.DB) {
		log.Info().Msg("Setup complete: hot-swapping database connection to target database")
		gorms.SetDBOverride(targetDB)

		// The built-in connectors were created during startup, which means they
		// went into the bootstrap database this call has just replaced. Without
		// seeding again, a freshly configured installation opens the connector
		// catalogue and finds it empty — no Slack, no email, no HTTP — with
		// nothing to indicate why.
		if err := a.svc.EnsureDefaultConnectors(ctx); err != nil {
			log.Error().Err(err).Msg("Failed to seed default connectors into the target database")
		}
	})

	dispatcher.Register(impl.NewNotificationObserver(a.svc))

	a.svc.StartWorkers(ctx)
	a.startSSEFanout(ctx)
	return nil
}

// startSSEFanout connects this replica's SSE observer to the shared bus, so a
// browser here sees events produced anywhere.
//
// Without it the client registry is per-process and correct only for a single
// replica: a browser connected to one process never learns about work done in
// another, and its lists stop updating with nothing in any log to say why.
//
// A failure here is not fatal. Local delivery works either way, so the cost of
// starting without the bus is the behaviour that shipped before it existed —
// which is worth logging loudly and not worth refusing to boot over.
func (a *App) startSSEFanout(ctx context.Context) {
	origin := replicaOrigin()
	fanout := impl.NewSSEFanout(a.repo.Broadcast(), a.sse, origin)
	if err := fanout.Start(ctx); err != nil {
		log.Error().Err(err).Str("replica", origin).
			Msg("The SSE bus did not start. Live updates will only reach browsers connected to this replica; every other browser will see stale lists until it refetches.")
		return
	}
	log.Info().Str("replica", origin).Msg("SSE fan-out started")
}

// replicaOrigin names this process on the shared bus.
//
// Only two properties matter: it is stable for the life of the process, and two
// replicas do not share one. A hostname gives both under every orchestrator
// that matters — Kubernetes gives each pod its own — and the PID disambiguates
// two processes on one host, which is how the tests run them. The random
// fallback keeps the guarantee when a hostname cannot be read; sharing an
// origin would make two replicas skip each other's events as their own, which
// is precisely the bug this exists to fix.
func replicaOrigin() string {
	host, err := os.Hostname()
	if err != nil || strings.TrimSpace(host) == "" {
		return fmt.Sprintf("replica-%s", uuid.NewString())
	}
	return fmt.Sprintf("%s-%d", host, os.Getpid())
}

func (a *App) setupAuth(ctx context.Context) {
	oidcIssuer := envvar.Get("OIDC_ISSUER")
	oidcClientID := envvar.Get("OIDC_CLIENT_ID")
	if oidcIssuer != "" && oidcClientID != "" {
		v, err := auth.NewTokenValidator(ctx, oidcIssuer, oidcClientID)
		if err != nil {
			log.Error().Err(err).Msg("failed to initialize OIDC validator")
		} else {
			a.validator = v
			log.Info().Str("issuer", redaction.RedactText(oidcIssuer)).Msg("OIDC Authentication enabled")
		}
	}
}

// readinessCheckers names the dependencies a replica needs before it should be
// sent traffic. Only the database qualifies today: without it no request can be
// served, whereas the broker being down degrades messaging rather than stopping
// the API.
//
// Before setup has run there is no database yet, and a replica in that state is
// still ready — serving the setup wizard is its whole job.
func (a *App) readinessCheckers() map[string]health.Checker {
	return map[string]health.Checker{
		"database": health.CheckerFunc(func(ctx context.Context) error {
			if a.db == nil {
				return nil
			}
			sqlDB, err := a.db.DB()
			if err != nil {
				return fmt.Errorf("resolve sql.DB: %w", err)
			}
			return sqlDB.PingContext(ctx)
		}),
	}
}

// BuildAPIHandler assembles the HTTP request path exactly as production serves
// it: the Go Kit handler inside the interceptor chain, with metrics, tracing
// and the health probes wrapped outside. runServers is its only production
// caller.
//
// It is exported for integration tests that must enter through the real front
// door — the strict-tenant-scope suite drives a process through this chain end
// to end, which is the coverage rbac_wiring_test.go cannot give by reading the
// wiring source. A nil validator selects the JWT strategy, as it does in
// production when OIDC is not configured.
func BuildAPIHandler(
	svc services.ServiceFacade,
	endpts endpoints.Endpoints,
	sse *impl.SSEObserver,
	validator *auth.TokenValidator,
	readiness map[string]health.Checker,
	db *gorm.DB,
) (http.Handler, *metrics.Collector) {
	httpHandler := https.NewHTTPHandler(svc, endpts, sse)

	f := interceptors.NewInterceptorFactory(svc)
	var strategy authinterceptor.SecurityStrategy
	if validator != nil {
		strategy = f.NewOIDCStrategy(validator)
	} else {
		strategy = f.NewJWTStrategy()
	}

	publicPaths := []string{
		"/api/v1/login",
		"/api/v1/setup/status",
		"/api/v1/setup",
		"/api/v1/setup/test-connection",
	}
	httpHandler = f.NewBackpressure(defaultHTTPMaxInFlightRequests, defaultHTTPMaxQueuedRequests).Wrap(
		f.NewRateLimit(defaultHTTPMaxRequestsPerLimit, time.Minute).Wrap(
			f.NewRequestSize(defaultHTTPMaxBodyBytes).Wrap(
				f.NewMandatoryHTTPAuth(strategy, publicPaths).Wrap(
					// Carries X-Organization-ID into the context. It only lets a
					// caller *choose* among the organizations they belong to;
					// the endpoint tenant resolver validates it against their
					// actual memberships.
					tenant.NewHTTPOrganizationSelector().Wrap(
						// Records go in the database rather than in this
						// process: a client retry that reaches another replica
						// must find the original answer, not an empty cache and
						// a second execution of the write. Before setup has run
						// there is no database yet, and the factory falls back
						// to the in-process store for that window.
						f.NewIdempotencyOver(db, defaultHTTPIdempotencyTTL).Wrap(httpHandler),
					),
				),
			),
		),
	)

	// Metrics wrap outside the limiters so that requests they reject with 429 or
	// 503 are still counted. Those spend a caller's error budget and are the
	// first sign of trouble; measuring only what got through would make an
	// overloaded service look perfectly healthy.
	metricsCollector := metrics.New()
	httpHandler = metricsCollector.Wrap(httpHandler)

	// Tracing wraps outside metrics so that a span covers the whole request,
	// including time spent queued behind the backpressure limiter — which is
	// exactly the time a caller complains about and the handler never sees.
	// otelhttp also reads any incoming traceparent, so a request arriving from
	// another service joins that trace instead of starting a new one.
	httpHandler = otelhttp.NewHandler(httpHandler, "metis.http")

	// Health wraps outside metrics, so probe traffic is served without being
	// counted. Probes run every few seconds per replica and would otherwise
	// dominate the request count and drag the latency percentiles down.
	//
	// Outside every interceptor too, deliberately: an orchestrator's probe
	// carries no credentials, so it must not meet the auth interceptor, and a
	// probe shed by the backpressure limiter would report a merely busy process
	// as an unhealthy one — getting it restarted or pulled from rotation at the
	// moment it was recovering.
	httpHandler = health.Wrap(httpHandler, readiness)

	return httpHandler, metricsCollector
}

func (a *App) runServers(ctx context.Context) error {
	endpts := endpoints.MakeEndpoints(a.svc)
	httpHandler, metricsCollector := BuildAPIHandler(a.svc, endpts, a.sse, a.validator, a.readinessCheckers(), a.db)

	grpcServer := grpcs.NewGRPCServer(endpts)

	g, ctx := errgroup.WithContext(ctx)

	if metricsEnabled() {
		metricsAddress := resolveAddress(envMetricsAddress, defaultMetricsAddress)
		g.Go(func() error {
			log.Info().Str("addr", metricsAddress).Msg("metrics server listening")
			mux := http.NewServeMux()
			mux.Handle("/metrics", metricsCollector.Handler())
			server := newHTTPServer(metricsAddress, mux)

			go func() {
				<-ctx.Done()
				// WithoutCancel rather than Background: this runs because ctx
				// was cancelled, so deriving from it would hand Shutdown an
				// already-expired deadline. Inheriting the values keeps the
				// trace context attached to the shutdown instead of orphaning
				// it, which context.Background() would have done.
				shutdownCtx, cancel := context.WithTimeoutCause(
					context.WithoutCancel(ctx),
					httpShutdownTimeout,
					errors.New("metrics server shutdown timed out"),
				)
				defer cancel()

				if err := server.Shutdown(shutdownCtx); err != nil && !errors.Is(err, http.ErrServerClosed) {
					log.Error().Err(err).Msg("metrics server shutdown failed")
				}
			}()

			if err := server.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
				return fmt.Errorf("metrics server crashed: %w", err)
			}
			return nil
		})
	}

	if profilingEnabled() {
		pprofAddress := resolvePprofAddress()
		g.Go(func() error {
			log.Info().Str("addr", pprofAddress).Msg("pprof server listening")
			server := newHTTPServer(pprofAddress, newPprofHandler())

			go func() {
				<-ctx.Done()
				shutdownCtx, cancel := context.WithTimeoutCause(
					context.Background(),
					httpShutdownTimeout,
					errors.New("pprof server shutdown timed out"),
				)
				defer cancel()

				//nolint:contextcheck // shutdown is what ctx being cancelled triggers; inheriting it would cancel the shutdown itself
				if err := server.Shutdown(shutdownCtx); err != nil && !errors.Is(err, http.ErrServerClosed) {
					log.Error().Err(err).Msg("pprof server shutdown failed")
				}
			}()

			if err := server.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
				return fmt.Errorf("pprof server crashed: %w", err)
			}
			return nil
		})
	}

	// HTTP Server
	httpAddress := resolveAddress(envHTTPAddress, defaultHTTPAddress)
	g.Go(func() error {
		log.Info().Str("addr", httpAddress).Msg("HTTP server listening")
		server := newHTTPServer(httpAddress, httpHandler)
		go func() {
			<-ctx.Done()
			shutdownCtx, cancel := context.WithTimeoutCause(
				context.Background(),
				httpShutdownTimeout,
				errors.New("http server shutdown timed out"),
			)
			defer cancel()

			//nolint:contextcheck // shutdown is what ctx being cancelled triggers; inheriting it would cancel the shutdown itself
			if err := server.Shutdown(shutdownCtx); err != nil && !errors.Is(err, http.ErrServerClosed) {
				log.Error().Err(err).Msg("HTTP server shutdown failed")
			}
		}()
		return server.ListenAndServe()
	})

	// gRPC Server
	grpcAddress := resolveAddress(envGRPCAddress, defaultGRPCAddress)
	g.Go(func() error {
		// ListenConfig rather than net.Listen so the listener is bound under the
		// server's context and shutdown can interrupt a slow bind.
		var lc net.ListenConfig
		lis, err := lc.Listen(ctx, "tcp", grpcAddress)
		if err != nil {
			return err
		}
		baseServer := grpc.NewServer()
		a.registerGRPCServices(baseServer, grpcServer)

		log.Info().Str("addr", grpcAddress).Msg("gRPC server listening")

		go func() {
			<-ctx.Done()
			baseServer.GracefulStop()
		}()
		return baseServer.Serve(lis)
	})

	err := g.Wait()
	if err != nil && !errors.Is(err, http.ErrServerClosed) {
		return fmt.Errorf("server crashed: %w", err)
	}
	return nil
}

func (a *App) registerGRPCServices(baseServer *grpc.Server, grpcServer *grpcs.Server) {
	pbservices.RegisterOrganizationServiceServer(baseServer, grpcServer)
	pbservices.RegisterProjectServiceServer(baseServer, grpcServer)
	pbservices.RegisterProcessServiceServer(baseServer, grpcServer)
	pbservices.RegisterTaskServiceServer(baseServer, grpcServer)
	pbservices.RegisterDefinitionServiceServer(baseServer, grpcServer)
	pbservices.RegisterStatsServiceServer(baseServer, grpcServer)
	pbservices.RegisterExternalTaskServiceServer(baseServer, grpcServer)
	pbservices.RegisterSignalServiceServer(baseServer, grpcServer)
}

// resolveJWTSecret determines the JWT signing secret.
//
// Priority:
//  1. JWT_SECRET environment variable (always preferred).
//  2. If config.yaml exists (system is configured) and JWT_SECRET is absent,
//     the application refuses to start – a random secret would silently
//     invalidate all sessions on every restart.
//  3. Pre-setup state (no config.yaml): a random ephemeral secret is used
//     because no real users exist yet.
func (a *App) resolveJWTSecret() (string, error) {
	if secret := envvar.Get("JWT_SECRET"); secret != "" {
		if err := requireStrongSecret("JWT_SECRET", secret); err != nil {
			return "", err
		}
		return secret, nil
	}

	if config.Exists(config.DefaultConfigPath) {
		cfg, err := config.Load(config.DefaultConfigPath)
		if err == nil && cfg.JWTSecret != "" {
			if secretErr := requireStrongSecret("jwt_secret in config.yaml", cfg.JWTSecret); secretErr != nil {
				return "", secretErr
			}
			return cfg.JWTSecret, nil
		}

		return "", fmt.Errorf(
			"JWT_SECRET environment variable or config jwt_secret is required when config.yaml is present; " +
				"set it to a stable secret to avoid invalidating user sessions on restart",
		)
	}

	// Pre-setup: no persistent users exist yet; ephemeral secret is acceptable.
	log.Warn().Msg("No JWT_SECRET set and no config.yaml found; using a random ephemeral secret (pre-setup mode only)")
	return uuid.NewString(), nil
}

// resolveDialector determines the GORM dialector based on config.yaml or environment variables.
func (a *App) resolveDialector() (gorm.Dialector, error) {
	// Priority 1: Load from config.yaml if it exists
	if config.Exists(config.DefaultConfigPath) {
		cfg, err := config.Load(config.DefaultConfigPath)
		if err != nil {
			log.Warn().Err(err).Msg("Failed to load config.yaml, falling back to environment")
		} else {
			return a.dialectorFromConfig(cfg)
		}
	}

	// Priority 2: Fall back to environment variables
	dsn := envvar.Get("DATABASE_URL")
	if dsn != "" {
		log.Info().Msg("Using PostgreSQL database from DATABASE_URL...")
		return postgres.Open(dsn), nil
	}

	// Priority 3: Default to SQLite
	path := config.DefaultSQLitePath()
	log.Info().Str("file", path).Msg("Using SQLite database...")
	return sqlite.Open(sqliteDSNWithBusyTimeout(path)), nil
}

func (a *App) dialectorFromConfig(cfg *config.Config) (gorm.Dialector, error) {
	// Same precedence as setupEncryption: the key stored alongside the data
	// wins, because it is the one the data was encrypted with.
	encKey := cfg.EncryptionKey
	if encKey == "" {
		encKey = envvar.Get("ENCRYPTION_KEY")
	}

	dsn, err := cfg.DecryptConnectionString(encKey)
	if err != nil {
		return nil, fmt.Errorf(
			"could not decrypt the database connection string in %s — this almost always means the "+
				"encryption key does not match the one used when the system was set up: %w",
			config.DefaultConfigPath, err)
	}

	switch cfg.Database.Driver {
	case config.DriverPostgres:
		log.Info().Msg("Using PostgreSQL database from config...")
		return postgres.Open(dsn), nil
	case config.DriverMySQL:
		log.Info().Msg("Using MySQL database from config...")
		return mysql.Open(dsn), nil
	case config.DriverSQLServer:
		log.Info().Msg("Using SQL Server database from config...")
		return sqlserver.Open(dsn), nil
	default:
		log.Info().Str("path", redaction.RedactText(dsn)).Msg("Using SQLite database from config...")
		return sqlite.Open(sqliteDSNWithBusyTimeout(dsn)), nil
	}
}
