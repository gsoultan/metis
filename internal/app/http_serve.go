package app

import (
	"context"
	"errors"
	"fmt"
	"net/http"

	"github.com/rs/zerolog/log"
)

// serveUntilShutdown runs serve until ctx ends, then shuts the server down and
// returns only once that shutdown has finished.
//
// The waiting is the point. Serve and ListenAndServe return
// http.ErrServerClosed the moment Shutdown begins, not when it ends, so a
// caller that returned on that error was gone while requests were still being
// answered: the errgroup finished, background work was stopped and the process
// could exit under a request that had been promised the drain time — a write
// committed whose answer never reached the client, who then retried it.
//
// The shutdown context keeps ctx's values but not its cancellation: this runs
// because ctx was cancelled, and inheriting that would hand Shutdown an
// already-expired deadline.
func serveUntilShutdown(ctx context.Context, name string, server *http.Server, serve func() error) error {
	shutdownDone := make(chan struct{})
	go func() {
		defer close(shutdownDone)
		<-ctx.Done()
		shutdownCtx, cancel := context.WithTimeoutCause(
			context.WithoutCancel(ctx),
			httpShutdownTimeout,
			fmt.Errorf("%s shutdown timed out", name),
		)
		defer cancel()
		if err := server.Shutdown(shutdownCtx); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Error().Err(err).Str("server", name).Msg("Server shutdown failed; requests still in flight were cut off")
		}
	}()

	err := serve()
	if errors.Is(err, http.ErrServerClosed) {
		<-shutdownDone
		return nil
	}
	return err
}
