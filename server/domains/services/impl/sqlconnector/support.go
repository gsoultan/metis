package sqlconnector

import (
	"database/sql"
	"errors"

	"github.com/rs/zerolog/log"
)

// applicationName is what a lookup's connections call themselves, so the
// database's own administrator can see whose they are.
const applicationName = "metis-lookup"

// errUnreadableConnection stands in for a driver's parse error, which may quote
// the connection string it could not read — password and all.
var errUnreadableConnection = errors.New("the lookup's connection string could not be read; check it on the connector's settings")

// rollback ends a lookup's transaction. It is never committed: a lookup has
// nothing to keep, and on SQL Server, which cannot begin a read-only
// transaction, rolling back is what undoes a write that got past everything
// else.
func rollback(tx *sql.Tx) {
	if err := tx.Rollback(); err != nil && !errors.Is(err, sql.ErrTxDone) {
		log.Debug().Err(err).Msg("Could not roll back a lookup's transaction; the connection is discarded")
	}
}
