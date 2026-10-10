package testutils

import (
	"fmt"
	"slices"
	"testing"

	"gorm.io/gorm"
)

// EveryRow is what a test's database holds: for each table of its schema, how
// many rows and a hash of all of them. A test's schema is its own, so two
// readings that are equal mean nothing was written between them — to any
// table, by any path.
func EveryRow(t *testing.T, db *gorm.DB) map[string]string {
	t.Helper()
	var tables []string
	if err := db.Raw(`SELECT table_name FROM information_schema.tables
		 WHERE table_schema = current_schema() AND table_type = 'BASE TABLE'`).Scan(&tables).Error; err != nil {
		t.Fatalf("list the tables: %v", err)
	}
	if len(tables) == 0 {
		t.Fatal("the schema lists no tables, so comparing them would prove nothing")
	}
	held := make(map[string]string, len(tables))
	for _, table := range tables {
		var rows string
		query := fmt.Sprintf(`SELECT count(*)::text || ' rows ' || coalesce(md5(string_agg(r::text, '|' ORDER BY r::text)), '')
			 FROM %q r`, table)
		if err := db.Raw(query).Scan(&rows).Error; err != nil {
			t.Fatalf("read %s: %v", table, err)
		}
		held[table] = rows
	}
	return held
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
