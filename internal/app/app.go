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

	"github.com/google/uuid"
	pbservices "github.com/gsoultan/gobpm/api/proto/services"
	"github.com/gsoultan/gobpm/internal/pkg/auth"
	"github.com/gsoultan/gobpm/internal/pkg/config"
	"github.com/gsoultan/gobpm/internal/pkg/crypto"
	"github.com/gsoultan/gobpm/internal/pkg/logger"
	"github.com/gsoultan/gobpm/internal/pkg/redaction"
	"github.com/gsoultan/gobpm/server/domains/observers/impl"
	"github.com/gsoultan/gobpm/server/domains/services"
	serviceimpl "github.com/gsoultan/gobpm/server/domains/services/impl"
	"github.com/gsoultan/gobpm/server/endpoints"
	"github.com/gsoultan/gobpm/server/interceptors"
	authinterceptor "github.com/gsoultan/gobpm/server/interceptors/auth"
	"github.com/gsoultan/gobpm/server/interceptors/tenant"
	"github.com/gsoultan/gobpm/server/repositories"
	gorms "github.com/gsoultan/gobpm/server/repositories/gorms"
	models "github.com/gsoultan/gobpm/server/repositories/models"
	"github.com/gsoultan/gobpm/server/transports/grpcs"
	https "github.com/gsoultan/gobpm/server/transports/https"

	"github.com/glebarez/sqlite"
	"github.com/rs/zerolog/log"
	"golang.org/x/sync/errgroup"
	"google.golang.org/grpc"
	"gorm.io/driver/mysql"
	"gorm.io/driver/postgres"
	"gorm.io/driver/sqlserver"
	"gorm.io/gorm"
)

type App struct {
	config     *config.Config
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
	envPprofEnabled                      = "GOBPM_PPROF_ENABLED"
	envPprofAddress                      = "GOBPM_PPROF_ADDRESS"

	defaultHTTPAddress = ":8080"
	defaultGRPCAddress = ":8081"
	envHTTPAddress     = "GOBPM_HTTP_ADDRESS"
	envGRPCAddress     = "GOBPM_GRPC_ADDRESS"

	// envResetPassword supplies the new password for --reset-password, so that
	// a chosen one need not be typed where it will be recorded.
	envResetPassword = "GOBPM_NEW_PASSWORD"
)

// resolveAddress returns the listen address from env, falling back to a
// default. Both were previously hardcoded, so two instances could not run side
// by side and a container could not be told which port to publish.
func resolveAddress(envVar, fallback string) string {
	if v := strings.TrimSpace(os.Getenv(envVar)); v != "" {
		return v
	}
	return fallback
}

func profilingEnabled() bool {
	enabledValue, exists := os.LookupEnv(envPprofEnabled)
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
	address := strings.TrimSpace(os.Getenv(envPprofAddress))
	if address == "" {
		return defaultPprofAddress
	}
	return address
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
		return a.handleBuildUI()
	}

	// 0. Initialize Logger
	logger.Init()

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
	password, generated := os.Getenv(envResetPassword), false
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

func (a *App) handleBuildUI() error {
	fmt.Println("Building UI...")
	if _, err := exec.LookPath("bun"); err != nil {
		return fmt.Errorf("'bun' not found in PATH. Please install Bun to build the UI")
	}
	cmd := exec.Command("bun", "run", "build")
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
func (a *App) setupEncryption() error {
	envKey := os.Getenv("ENCRYPTION_KEY")

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
			if err := crypto.Configure(cfg.EncryptionKey); err != nil {
				return fmt.Errorf("invalid encryption_key in config.yaml: %w", err)
			}
			log.Info().Msg("At-rest encryption key loaded from config.yaml")
			return nil
		}

		if envKey != "" {
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
	db, err := gorm.Open(dialector, &gorm.Config{})
	if err != nil {
		return fmt.Errorf("failed to open db: %w", err)
	}
	a.db = db

	return a.migrate()
}

func (a *App) migrate() error {
	if err := a.db.AutoMigrate(models.MigrationModels()...); err != nil {
		return fmt.Errorf("failed to migrate db: %w", err)
	}

	// Schema alone is not enough here: message subscriptions written before
	// correlation keys were resolved per instance still hold the raw ${...}
	// template, which no inbound correlation value can match. Repair them before
	// serving traffic, or every instance already waiting on a message event
	// hangs.
	repo := repositories.NewRepository(a.db)
	if _, err := serviceimpl.BackfillMessageCorrelationKeys(context.Background(), repo); err != nil {
		return fmt.Errorf("failed to backfill message correlation keys: %w", err)
	}

	// Multi-instance bookkeeping moved out of the business variable namespace
	// into its own column. An instance already part-way through a node that runs
	// once per item would otherwise come back with no progress recorded and
	// restart its iterations from zero.
	if _, err := serviceimpl.BackfillMultiInstanceState(context.Background(), repo); err != nil {
		return fmt.Errorf("failed to backfill multi-instance state: %w", err)
	}
	return nil
}

func (a *App) setupService(ctx context.Context) error {
	a.repo = repositories.NewRepository(a.db)

	dispatcher := impl.NewEventDispatcher()
	dispatcher.Register(impl.NewAuditLogObserver(a.repo.Audit()))
	a.sse = impl.NewSSEObserver()
	dispatcher.Register(a.sse)

	// Register Webhook Observer if endpoints are provided
	webhookEndpoints := os.Getenv("WEBHOOK_ENDPOINTS")
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
	return nil
}

func (a *App) setupAuth(ctx context.Context) {
	oidcIssuer := os.Getenv("OIDC_ISSUER")
	oidcClientID := os.Getenv("OIDC_CLIENT_ID")
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

func (a *App) runServers(ctx context.Context) error {
	endpts := endpoints.MakeEndpoints(a.svc)
	httpHandler := https.NewHTTPHandler(a.svc, endpts, a.sse)

	f := interceptors.NewInterceptorFactory(a.svc)
	var strategy authinterceptor.SecurityStrategy
	if a.validator != nil {
		strategy = f.NewOIDCStrategy(a.validator)
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
						f.NewIdempotency(defaultHTTPIdempotencyTTL).Wrap(httpHandler),
					),
				),
			),
		),
	)

	grpcServer := grpcs.NewGRPCServer(endpts)

	g, ctx := errgroup.WithContext(ctx)

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

			if err := server.Shutdown(shutdownCtx); err != nil && !errors.Is(err, http.ErrServerClosed) {
				log.Error().Err(err).Msg("HTTP server shutdown failed")
			}
		}()
		return server.ListenAndServe()
	})

	// gRPC Server
	grpcAddress := resolveAddress(envGRPCAddress, defaultGRPCAddress)
	g.Go(func() error {
		lis, err := net.Listen("tcp", grpcAddress)
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

func (a *App) registerGRPCServices(baseServer *grpc.Server, grpcServer any) {
	pbservices.RegisterOrganizationServiceServer(baseServer, grpcServer.(pbservices.OrganizationServiceServer))
	pbservices.RegisterProjectServiceServer(baseServer, grpcServer.(pbservices.ProjectServiceServer))
	pbservices.RegisterProcessServiceServer(baseServer, grpcServer.(pbservices.ProcessServiceServer))
	pbservices.RegisterTaskServiceServer(baseServer, grpcServer.(pbservices.TaskServiceServer))
	pbservices.RegisterDefinitionServiceServer(baseServer, grpcServer.(pbservices.DefinitionServiceServer))
	pbservices.RegisterStatsServiceServer(baseServer, grpcServer.(pbservices.StatsServiceServer))
	pbservices.RegisterExternalTaskServiceServer(baseServer, grpcServer.(pbservices.ExternalTaskServiceServer))
	pbservices.RegisterSignalServiceServer(baseServer, grpcServer.(pbservices.SignalServiceServer))
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
	if secret := os.Getenv("JWT_SECRET"); secret != "" {
		return secret, nil
	}

	if config.Exists(config.DefaultConfigPath) {
		cfg, err := config.Load(config.DefaultConfigPath)
		if err == nil && cfg.JWTSecret != "" {
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
	dsn := os.Getenv("DATABASE_URL")
	if dsn != "" {
		log.Info().Msg("Using PostgreSQL database from DATABASE_URL...")
		return postgres.Open(dsn), nil
	}

	// Priority 3: Default to SQLite
	log.Info().Msg("Using SQLite database (gobpm.db)...")
	return sqlite.Open("gobpm.db"), nil
}

func (a *App) dialectorFromConfig(cfg *config.Config) (gorm.Dialector, error) {
	// Same precedence as setupEncryption: the key stored alongside the data
	// wins, because it is the one the data was encrypted with.
	encKey := cfg.EncryptionKey
	if encKey == "" {
		encKey = os.Getenv("ENCRYPTION_KEY")
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
		return sqlite.Open(dsn), nil
	}
}
