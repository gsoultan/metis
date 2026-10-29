package db

import (
	"context"
	"fmt"
	"strings"

	"github.com/gsoultan/storm/migrate"
	"github.com/gsoultan/storm/schema"
	pgschema "github.com/gsoultan/storm/schema/pg"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Namespace is the schema a pool's connections work in: the first entry of
// the search_path its connection string sets, or public. The test harnesses
// give each run its own, and two of them must not see each other's tables.
func Namespace(pool *pgxpool.Pool) string {
	path := pool.Config().ConnConfig.RuntimeParams["search_path"]
	first, _, _ := strings.Cut(path, ",")
	if first = strings.Trim(strings.TrimSpace(first), `"`); first != "" {
		return first
	}
	return "public"
}

// ReportDrift compares the live schema against the model and returns the
// statements that would reconcile them, without running any of them.
//
// The migrations decide the shape of a table, and the model is a second
// description of it. Where the two disagree nothing fails until a query runs,
// and the symptom arrives much later and somewhere else: a unique key that is
// scoped to the live rows in the model and to every row in the database looks
// identical until the first time somebody deletes a row and cannot recreate it.
//
// So the drift is named at boot rather than fixed. Destructive steps are marked,
// because a report that reads like a to-do list should say which items would
// lose data.
//
// Reads only the tables in want.
func ReportDrift(ctx context.Context, pool *pgxpool.Pool, want *schema.Schema) ([]string, error) {
	if pool == nil || want == nil {
		return nil, nil
	}
	conn, err := pool.Acquire(ctx)
	if err != nil {
		return nil, fmt.Errorf("could not borrow a connection to check the schema: %w", err)
	}
	defer conn.Release()

	live, err := pgschema.Introspect(ctx, conn.Conn(), Namespace(pool))
	if err != nil {
		return nil, fmt.Errorf("could not read the live schema: %w", err)
	}

	// Narrowed to the tables the model knows about: a table it does not
	// describe is not drift.
	modelled := map[string]struct{}{}
	for _, table := range want.Tables {
		modelled[table.Name] = struct{}{}
	}
	narrowed := &schema.Schema{Enums: live.Enums}
	for _, table := range live.Tables {
		if _, ok := modelled[table.Name]; ok {
			narrowed.Tables = append(narrowed.Tables, table)
		}
	}

	var drift []string
	for _, change := range migrate.Diff(narrowed, want).Changes {
		line := change.SQL
		if change.Destructive {
			line = "would lose data: " + line
		}
		drift = append(drift, line)
	}
	return drift, nil
}
