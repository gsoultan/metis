package main

import (
	"github.com/gsoultan/metis/internal/app"
	"github.com/rs/zerolog/log"
)

func main() {
	a := app.New()
	if err := a.Run(); err != nil {
		log.Fatal().Err(err).Msg("Application failed")
	}
}
