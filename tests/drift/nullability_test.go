package drift_test

import (
	"context"
	"sort"
	"strings"
	"testing"

	"github.com/gsoultan/metis/server/repositories/db"
	"github.com/gsoultan/metis/server/repositories/model"
	"github.com/gsoultan/metis/tests/testutils"
	"github.com/gsoultan/storm"
)

// fixedWidthDecoders are the column types whose generated reader indexes a
// fixed number of bytes out of the wire buffer.
//
// storm decodes an int8 with binary.BigEndian.Uint64 and a timestamptz with the
// same, so a NULL — which arrives as a zero-length buffer — is not a zero
// value, it is an index out of range. Bool reads b[0] behind a length check,
// UUID copies into a fixed array, and text, bytea and json all tolerate an
// empty buffer. Those degrade; these panic.
var fixedWidthDecoders = map[string]bool{
	"smallint": true, "integer": true, "bigint": true,
	"real": true, "double precision": true,
	"timestamp with time zone": true, "timestamp without time zone": true,
}

// TestAColumnReadAsNonNullIsDeclaredNonNull closes the class of defect that
// took down the task inbox.
//
// The two layers describe the same table twice: the storm model says whether a
// field is nullable (a pointer is, a value is not) and the generated reader is
// compiled from that, while GORM's migrations create the column. AutoMigrate
// does not carry non-nullability over on its own, so a column could permit a
// value its own reader cannot decode — and nothing fails until a row actually
// carries a NULL, which normally happens first on an upgrade, when AutoMigrate
// adds a column to a table that already has rows in it.
//
// That is how `GET /api/v1/tasks` came to panic with `index out of range [7]
// with length 0` on tasks.priority. Seventy-two other columns had the same
// shape at the time. This is the check that says so before a user finds out.
//
// The sibling test above deliberately ignores type differences, because text
// accepts everything varchar and jsonb do. Nullability is not like that: it is
// the difference between a row that reads and a request that dies.
func TestAColumnReadAsNonNullIsDeclaredNonNull(t *testing.T) {
	_, conn := testutils.SetupTestStore(t)

	want, err := storm.Build(model.All()...)
	if err != nil {
		t.Fatalf("build the model: %v", err)
	}

	ctx := context.Background()
	var divergent []string
	for _, table := range want.Tables {
		for _, column := range table.Columns {
			if !column.NotNull {
				continue
			}
			isNullable, dataType, err := columnFacts(ctx, conn, table.Name, column.Name)
			if err != nil {
				t.Fatalf("read %s.%s: %v", table.Name, column.Name, err)
			}
			if isNullable == "YES" && fixedWidthDecoders[dataType] {
				divergent = append(divergent, table.Name+"."+column.Name+" ("+dataType+")")
			}
		}
	}

	if len(divergent) > 0 {
		sort.Strings(divergent)
		t.Fatalf("%d column(s) the reader decodes as non-null but the table allows NULL in.\n"+
			"A single NULL row makes every read of that table panic, and the request is\n"+
			"answered with a 500 rather than data. Add `gorm:\"not null\"` to the model\n"+
			"field and a migration for tables that already exist — see migration 21.\n\n  %s",
			len(divergent), strings.Join(divergent, "\n  "))
	}
}

// columnFacts asks the database what it actually created, rather than what the
// migration meant to create.
func columnFacts(ctx context.Context, conn *db.Conn, table, column string) (isNullable, dataType string, err error) {
	rows, err := conn.Main().Query(ctx, `
		SELECT is_nullable, data_type FROM information_schema.columns
		WHERE table_schema = current_schema() AND table_name = $1 AND column_name = $2`,
		table, column)
	if err != nil {
		return "", "", err
	}
	defer rows.Close()
	for rows.Next() {
		if err := rows.Scan(&isNullable, &dataType); err != nil {
			return "", "", err
		}
	}
	return isNullable, dataType, rows.Err()
}
