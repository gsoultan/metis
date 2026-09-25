package db

import (
	"testing"
)

// The main pool carries the ported repositories — most of what the engine
// runs — and was sized by pgx's default: the larger of 4 and the machine's CPU
// count. METIS_DB_MAX_OPEN_CONNS, documented as the pool size, reached only
// GORM's pool, so on a 64-core node each replica could open 64 connections
// nobody had configured.
func TestTheMainPoolIsSizedByTheDocumentedSetting(t *testing.T) {
	// Nothing listens on port 1; a pgx pool does not connect until it is used.
	const dsn = "postgres://metis@127.0.0.1:1/metis?sslmode=disable"

	for _, tc := range []struct {
		name, env, dsn string
		want           int32
	}{
		{name: "the default", dsn: dsn, want: 25},
		{name: "the setting", env: "7", dsn: dsn, want: 7},
		{name: "a DSN that says so itself", env: "7", dsn: dsn + "&pool_max_conns=3", want: 3},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv("METIS_DB_MAX_OPEN_CONNS", tc.env)
			conn, err := Open(t.Context(), tc.dsn)
			if err != nil {
				t.Fatalf("open: %v", err)
			}
			defer conn.Close()
			if got := conn.Main().Config().MaxConns; got != tc.want {
				t.Fatalf("the main pool holds up to %d connections, want %d", got, tc.want)
			}
		})
	}
}

// storm's scanners read binary. Under a text query mode — a common setting
// behind PgBouncer — a boolean decodes inverted with no error, so such a pool
// has to be refused at start rather than served from.
func TestAPoolInATextQueryModeIsRefused(t *testing.T) {
	for _, mode := range []string{"simple_protocol", "exec"} {
		pool, err := NewPool(t.Context(), "postgres://metis@127.0.0.1:1/metis?sslmode=disable&default_query_exec_mode="+mode)
		if err == nil {
			pool.Close()
			t.Fatalf("a pool in %s mode was opened", mode)
		}
	}
}
