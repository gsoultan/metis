package selfupgrade

import (
	"errors"
	"io"
	"os"

	"github.com/rs/zerolog/log"
)

// Cleanup that fails changes no outcome of an upgrade, but it is said, so a
// host left with a stray file or an open handle has a line that names it.

// closing closes c and says so when that fails.
func closing(c io.Closer, what string) {
	if err := c.Close(); err != nil {
		log.Debug().Err(err).Str("what", what).Msg("upgrade: could not close")
	}
}

// removing removes a file that may already be gone.
func removing(path string) {
	if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
		log.Warn().Err(err).Str("path", path).Msg("upgrade: could not remove a file it no longer needs")
	}
}

// sharing gives the service's group read access to what the page shows. A
// failure leaves the upgrade itself untouched; the page just cannot show it.
func sharing(path string, gid int) {
	if gid <= 0 {
		return
	}
	if err := os.Chown(path, -1, gid); err != nil {
		log.Warn().Err(err).Str("path", path).Msg("upgrade: the Upgrades page will not be able to read this")
	}
}
