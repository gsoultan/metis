package testutils

import (
	"fmt"
	"slices"
	"testing"

	"github.com/gsoultan/storm/schema"

	"github.com/gsoultan/metis/server/repositories/db"
	pgschema "github.com/gsoultan/storm/schema/pg"
)

// EveryRow is what a test's database holds: for each table of its schema, how
// many rows and a hash of all of them. A test's schema is its own, so two
// readings that are equal mean nothing was written between them — to any
// table, by any path.
//
// The one read here no model can say: a fingerprint of rows of every table,
// whatever its columns.
func EveryRow(t *testing.T, conn *db.Conn) map[string]string {
	t.Helper()
	ctx := t.Context()
	pool := conn.Main()
	held := Held(t, conn)
	if len(held.Tables) == 0 {
		t.Fatal("the schema lists no tables, so comparing them would prove nothing")
	}
	rows := make(map[string]string, len(held.Tables))
	for _, table := range held.Tables {
		var fingerprint string
		query := fmt.Sprintf(`SELECT count(*)::text || ' rows ' || coalesce(md5(string_agg(r::text, '|' ORDER BY r::text)), '')
			 FROM %q r`, table.Name)
		if err := pool.QueryRow(ctx, query).Scan(&fingerprint); err != nil {
			t.Fatalf("read %s: %v", table.Name, err)
		}
		rows[table.Name] = fingerprint
	}
	return rows
}

// TablesThatDiffer names the tables two readings of EveryRow disagree on, in
// order, each with what it held and holds.
func TablesThatDiffer(before, after map[string]string) []string {
	var changed []string
	for table, rows := range after {
		if before[table] != rows {
			changed = append(changed, fmt.Sprintf("%s (%s, was %s)", table, rows, before[table]))
		}
	}
	slices.Sort(changed)
	return changed
}

// Held is the schema conn's main database holds, read from the catalog: what a
// test asks when the point is the table itself, not a row in it.
func Held(t *testing.T, conn *db.Conn) *schema.Schema {
	t.Helper()
	pool := conn.Main()
	one, err := pool.Acquire(t.Context())
	if err != nil {
		t.Fatalf("borrow a connection: %v", err)
	}
	defer one.Release()
	held, err := pgschema.Introspect(t.Context(), one.Conn(), db.Namespace(pool))
	if err != nil {
		t.Fatalf("read the schema: %v", err)
	}
	return held
}

// RowsIn counts every row of table, marked deleted or not: what a test of a
// purge asks, which no store read can — a store reads the live rows unless
// its model lets call sites widen to the marked ones, and none widens only so
// a test can look.
func RowsIn(t *testing.T, conn *db.Conn, table string) int64 {
	t.Helper()
	var n int64
	if err := conn.Main().QueryRow(t.Context(), fmt.Sprintf(`SELECT count(*) FROM %q`, table)).Scan(&n); err != nil {
		t.Fatalf("count %s: %v", table, err)
	}
	return n
}
