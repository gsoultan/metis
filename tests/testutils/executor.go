package testutils

import (
	"context"
	"testing"

	"github.com/gsoultan/metis/server/repositories/db"
	"github.com/gsoultan/storm/runtime"
	"github.com/gsoultan/storm/runtime/pgxdrv"
)

// Executor runs a generated store's statements on conn's main database, each
// on its own, outside any transaction: how a test seeds rows and reads back
// what the code under test wrote.
func Executor(conn *db.Conn) runtime.Executor {
	return pgxdrv.Pool{P: conn.Main()}
}

// Begin opens a transaction on conn's main database for a test to hold: to
// keep a row locked while the code under test meets it. Rolled back when the
// test ends, unless the test has finished it already.
func Begin(t *testing.T, conn *db.Conn) runtime.Tx {
	t.Helper()
	tx, err := pgxdrv.Pool{P: conn.Main()}.StartTx(t.Context())
	if err != nil {
		t.Fatalf("begin a transaction: %v", err)
	}
	t.Cleanup(func() { _ = tx.Rollback(context.Background()) })
	return tx
}
