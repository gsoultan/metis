package app

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"sync"

	"github.com/rs/zerolog/log"

	"github.com/gsoultan/metis/internal/pkg/config"
	"github.com/gsoultan/metis/internal/pkg/envvar"
	"github.com/gsoultan/metis/internal/pkg/health"
	serviceimpl "github.com/gsoultan/metis/server/domains/services/impl"
	setupendpoints "github.com/gsoultan/metis/server/endpoints/setup"
	"github.com/gsoultan/metis/server/transports/https"
)

// errNotSetUp is what readiness reports while only the wizard is served: the
// process is alive, and it is not ready for anybody's traffic.
var errNotSetUp = errors.New("this server is not set up; open /setup")

// awaitsWizard reports whether nothing names a database: no config.yaml and
// no DATABASE_URL. Such a server serves the setup wizard and nothing else
// until the wizard has chosen one.
func awaitsWizard() bool {
	return !config.Exists(config.DefaultConfigPath) && envvar.Get("DATABASE_URL") == ""
}

// useConfigFile installs the server settings config.yaml carries, so every
// reader of METIS_HTTP_ADDRESS and METIS_PUBLIC_URL sees them under the
// environment.
//
// A file that cannot be read installs nothing and is not an error here:
// resolveDSN reads the same file a moment later and refuses to start with a
// message that says which file and why.
func useConfigFile() {
	if !config.Exists(config.DefaultConfigPath) {
		return
	}
	cfg, err := config.Load(config.DefaultConfigPath)
	if err != nil {
		return
	}
	values := cfg.Server.Values()
	envvar.UseFile(values)
	for name, value := range values {
		if _, overridden := envvar.FromEnvironment(name); overridden {
			log.Info().Str("setting", name).Msg("config.yaml sets this, and the environment sets it too; the environment wins")
			continue
		}
		log.Info().Str("setting", name).Str("value", value).Msg("Read from config.yaml")
	}
}

// serveWizardOnly serves the setup wizard alone, on the HTTP address, until
// setup has written config.yaml or the process is told to stop.
//
// This is what a fresh package install does: it has no database yet, and the
// wizard is where one is chosen. Serving the whole product here would be the
// failure resolveDSN refuses — a server that looks up and healthy with
// nothing behind it — so it serves only the wizard, behind the setup token,
// and reports itself not ready the whole time.
//
// It returns nil once setup is done, for the caller to start on what was
// written; and nil if ctx ends first, with ctx.Err() saying so.
func serveWizardOnly(ctx context.Context) error {
	done := make(chan struct{})
	finish := sync.OnceFunc(func() { close(done) })
	svc := serviceimpl.NewSetupService(nil, envvar.Get(serviceimpl.EnvSetupToken), serviceimpl.WizardOnly(finish))

	handler := health.Wrap(
		https.NewSetupOnlyHandler(setupendpoints.MakeEndpoints(svc)),
		map[string]health.Checker{"setup": health.CheckerFunc(func(context.Context) error { return errNotSetUp })},
	)
	address := resolveAddress(envHTTPAddress, defaultHTTPAddress)
	server := newHTTPServer(address, handler)

	log.Warn().Str("addr", address).Msg("No database is configured, so only the setup wizard is served. Open /setup in a browser to choose one")
	svc.AnnounceSetupToken(ctx)

	// Stops on either: setup done, or the process told to stop. Shutdown
	// waits for the request that finished setup, so its answer reaches the
	// browser before the listener goes.
	serveCtx, stop := context.WithCancel(ctx)
	defer stop()
	go func() {
		select {
		case <-done:
			stop()
		case <-serveCtx.Done():
		}
	}()

	if err := serveUntilShutdown(serveCtx, "setup wizard", server, server.ListenAndServe); err != nil && !errors.Is(err, http.ErrServerClosed) {
		return fmt.Errorf("setup wizard server crashed: %w", err)
	}
	return nil
}
