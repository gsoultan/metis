package impl

import (
	"errors"
	"fmt"
	"runtime/debug"

	"github.com/rs/zerolog/log"

	"github.com/gsoultan/metis/internal/pkg/redaction"
)

// errPanicked marks work that panicked rather than returned an error.
//
// The job workers and the inbound message workers run user-authored
// definitions and decisions in goroutines of their own, and a panic in a
// goroutine nothing recovers ends the process: every replica that picked the
// same job or message up again went down the same way. Turned into an error,
// it fails that one piece of work through the path any failure takes.
var errPanicked = errors.New("panicked")

// runRecovered runs fn and returns a panic inside it as an error wrapping
// errPanicked, logged with its stack. what names the work for the log.
func runRecovered(what string, fn func() error) (err error) {
	defer func() {
		recovered := recover()
		if recovered == nil {
			return
		}
		// Redacted: a panic raised over a row or a connection string carries
		// whatever was in it, and this line ends up in a log aggregator.
		text := redaction.RedactText(fmt.Sprint(recovered))
		log.Error().
			Str("work", what).
			Str("panic", text).
			Bytes("stack", debug.Stack()).
			Msg("Work panicked; failing it instead of the process")
		err = fmt.Errorf("%w: %s: %s", errPanicked, what, text)
	}()
	return fn()
}
