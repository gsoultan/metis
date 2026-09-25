package sqlconnector

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"
)

// probeTimeout bounds the check a new pool makes before its first lookup.
const probeTimeout = 10 * time.Second

var errOwnDatabase = errors.New("the connection's login can see Metis's own tables; " +
	"a lookup must read another database, as a login that cannot")

// refuseOwnDatabase refuses a connection whose login can see Metis's own
// tables.
//
// A lookup's query is written by whoever designs the process. Pointed at the
// database Metis runs on, it would read every project's instances, every
// connection's settings, every account — whatever that login is allowed. So
// before a pool is used, it asks the database what its login can see, rather
// than comparing addresses: an alias, a pooler, a second address for the same
// server, or a port forward would each defeat a comparison, and none of them
// changes what the login can read. A login that cannot see these tables cannot
// query them; one that can, is refused.
//
// On MySQL, INFORMATION_SCHEMA covers every database on the server the login
// may read, which is right: it could read those too. SQL Server's covers only
// the current database, so a login that has been given access to Metis's
// database on the same server is not caught here — see the package note.
func refuseOwnDatabase(ctx context.Context, db *sql.DB) error {
	ctx, cancel := context.WithTimeout(ctx, probeTimeout)
	defer cancel()
	var visible int
	if err := db.QueryRowContext(ctx, ownTablesProbe).Scan(&visible); err != nil {
		// Failing closed: a database that cannot answer this is not one to
		// hand a process author's query to.
		return fmt.Errorf("could not confirm the lookup's database is not Metis's own, so it was not used: %w", err)
	}
	if visible >= ownTableCount {
		return errOwnDatabase
	}
	return nil
}
