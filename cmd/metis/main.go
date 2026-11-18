package main

import (
	"context"
	"os"
	"os/signal"
	"syscall"

	"github.com/gsoultan/metis/engine"
	"github.com/rs/zerolog/log"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if err := engine.New().Run(ctx, os.Args[1:]); err != nil {
		stop()
		log.Fatal().Err(err).Msg("Application failed")
	}
}
