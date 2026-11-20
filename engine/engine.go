// Package engine is the metis engine as a Go library: the BPMN and DMN
// runtime, its jobs, timers and schedules, releases, permission checks and
// the database it keeps them in.
//
// A program embeds it to run the engine in its own process. metis-server does,
// adding the web app and sign-in around it; cmd/metis is the smallest such
// program.
//
//	e := engine.New()
//	if err := e.Run(ctx, os.Args[1:]); err != nil { ... }
//
// The engine reads its configuration from the environment and config.yaml,
// exactly as the metis command does; see the README.
package engine

import (
	"context"
	"errors"
	"flag"

	"github.com/gsoultan/metis/internal/app"
)

// Engine is one metis engine. Run it once.
type Engine struct {
	app *app.App
}

// New returns an engine that has not started.
func New(options ...Option) *Engine {
	e := &Engine{app: app.New()}
	for _, option := range options {
		option(e)
	}
	return e
}

// Run starts the engine and blocks until ctx is done or it fails. args is the
// command line without the program name, as for the metis command: empty to
// serve, or a maintenance flag such as --reset-password, which does its work
// and returns. A cancelled ctx is a clean stop, not an error. A help flag
// returns nil after the usage is printed.
func (e *Engine) Run(ctx context.Context, args []string) error {
	err := e.app.RunContext(ctx, args)
	if errors.Is(err, flag.ErrHelp) {
		return nil
	}
	return err
}

// Version is the engine's build, as its liveness probe reports it.
func Version() string {
	return app.Version()
}

// Role is what an engine started here would do of its work, as METIS_ROLE or
// else server.role in config.yaml says: "all" (the default), "web" (serve the
// API, run no jobs, timers or deliveries) or "worker". An unknown value is an
// error, as it is when the engine starts. Reading it changes nothing.
func Role() (string, error) {
	role, err := app.ConfiguredRole()
	return string(role), err
}
