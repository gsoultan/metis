package testutils

import (
	"fmt"
	"slices"
	"testing"

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
	one, err := pool.Acquire(ctx)
	if err != nil {
		t.Fatalf("borrow a connection: %v", err)
	}
	held, err := pgschema.Introspect(ctx, one.Conn(), db.Namespace(pool))
	one.Release()
	if err != nil {
		t.Fatalf("list the tables: %v", err)
	}
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
