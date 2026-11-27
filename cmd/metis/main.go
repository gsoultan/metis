package main

import (
	"context"
	"errors"
	"os"
	"os/signal"
	"syscall"

	"github.com/gsoultan/metis/engine"
	"github.com/rs/zerolog/log"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	err := engine.New().Run(ctx, os.Args[1:])
	stop()
	if errors.Is(err, engine.ErrRestart) {
		err = engine.Restart()
	}
	if err != nil {
		log.Fatal().Err(err).Msg("Application failed")
	}
}
