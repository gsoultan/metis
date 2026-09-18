package migrations_test

import (
	"fmt"
	"sort"
	"strings"
	"testing"

	"github.com/gsoultan/metis/server/repositories/migrations"
	"github.com/gsoultan/metis/server/repositories/model"
	"github.com/gsoultan/metis/server/repositories/models"
	"github.com/gsoultan/metis/tests/testutils"
	"github.com/gsoultan/storm"
	"gorm.io/gorm"
)

// fixedWidth are the column types whose generated reader indexes a fixed number
// of bytes, so a NULL panics rather than decoding as a zero value.
var fixedWidth = map[string]bool{
	"smallint": true, "integer": true, "bigint": true,
	"real": true, "double precision": true,
	"timestamp with time zone": true, "timestamp without time zone": true,
}

// TestMigration22RepairsEveryColumnTheReaderNeeds checks the migration's list
// against the model the reader is compiled from.
//
// A fresh install gets these constraints from the model tags, so migration 22
// finds nothing to do and would pass this test having done nothing. So the
// constraints are taken back off first: that is the schema an upgrading
// installation actually has, and it is the only one on which the migration does
// any work.
//
// What this catches is the migration's list falling behind the model — a new
// non-nullable field added with a tag, so fresh installs are fine, and left out
// of the migration, so every existing installation keeps the column that
// panics. That failure is invisible until someone upgrades.
func TestMigration22RepairsEveryColumnTheReaderNeeds(t *testing.T) {
	db := testutils.SetupTestDB(t)
	ctx := t.Context()

	if _, err := migrations.Run(ctx, db, migrations.Schema(models.MigrationModels())); err != nil {
		t.Fatalf("run the migrations: %v", err)
	}

	needed := columnsTheReaderNeeds(t, db)
	if len(needed) == 0 {
		t.Fatal("no non-nullable fixed-width columns found; this test is looking in the wrong place")
	}

	// Back to the pre-22 shape.
	for _, c := range needed {
		if err := db.WithContext(ctx).Exec(fmt.Sprintf(
			`ALTER TABLE %s ALTER COLUMN %s DROP NOT NULL`, c.table, c.column)).Error; err != nil {
			t.Fatalf("drop not null on %s.%s: %v", c.table, c.column, err)
		}
	}
	if err := db.WithContext(ctx).Exec(
		`DELETE FROM schema_migrations WHERE version IN (21, 22)`).Error; err != nil {
		t.Fatalf("rewind the migration record: %v", err)
	}

	if _, err := migrations.Run(ctx, db, migrations.Schema(models.MigrationModels())); err != nil {
		t.Fatalf("re-run the migrations over the old schema: %v", err)
	}

	var missed []string
	for _, c := range needed {
		var nullable string
		if err := db.WithContext(ctx).Raw(`
			SELECT is_nullable FROM information_schema.columns
			WHERE table_schema = current_schema() AND table_name = ? AND column_name = ?`,
			c.table, c.column).Scan(&nullable).Error; err != nil {
			t.Fatalf("read %s.%s: %v", c.table, c.column, err)
		}
		if nullable != "NO" {
			missed = append(missed, c.table+"."+c.column)
		}
	}
	if len(missed) > 0 {
		sort.Strings(missed)
		t.Fatalf("migration 22 left %d column(s) nullable that the reader decodes as non-null.\n"+
			"A fresh install is fine — the model tag covers it — and every upgraded\n"+
			"installation keeps a column that panics on read. Add them to migration 22\n"+
			"or, if this list has grown, to a new migration:\n\n  %s",
			len(missed), strings.Join(missed, "\n  "))
	}
}

// Running it again must do nothing and take no locks worth mentioning: a
// migration that is not idempotent is one that cannot be retried after a
// failure part-way through.
func TestMigration22IsIdempotent(t *testing.T) {
	db := testutils.SetupTestDB(t)
	ctx := t.Context()

	if _, err := migrations.Run(ctx, db, migrations.Schema(models.MigrationModels())); err != nil {
		t.Fatalf("run the migrations: %v", err)
	}
	if err := db.WithContext(ctx).Exec(
		`DELETE FROM schema_migrations WHERE version IN (21, 22)`).Error; err != nil {
		t.Fatalf("rewind the migration record: %v", err)
	}
	if _, err := migrations.Run(ctx, db, migrations.Schema(models.MigrationModels())); err != nil {
		t.Fatalf("re-running 21 and 22 over a schema that already has them failed: %v", err)
	}
}

type columnRef struct{ table, column string }

// columnsTheReaderNeeds is the model's own answer: a storm field that is not a
// pointer is not nullable, and the generated reader is compiled from that.
func columnsTheReaderNeeds(t *testing.T, db *gorm.DB) []columnRef {
	t.Helper()
	want, err := storm.Build(model.All()...)
	if err != nil {
		t.Fatalf("build the model: %v", err)
	}

	var needed []columnRef
	for _, table := range want.Tables {
		for _, column := range table.Columns {
			if !column.NotNull {
				continue
			}
			var dataType string
			if err := db.Raw(`
				SELECT data_type FROM information_schema.columns
				WHERE table_schema = current_schema() AND table_name = ? AND column_name = ?`,
				table.Name, column.Name).Scan(&dataType).Error; err != nil {
				t.Fatalf("read %s.%s: %v", table.Name, column.Name, err)
			}
			if !fixedWidth[dataType] {
				continue
			}
			// A primary key is NOT NULL because it is a primary key, and
			// PostgreSQL will not let that be dropped. It was never part of
			// the problem and cannot be part of the rehearsal.
			var inPrimaryKey int64
			if err := db.Raw(`
				SELECT count(*) FROM information_schema.key_column_usage k
				JOIN information_schema.table_constraints c
				  ON c.constraint_name = k.constraint_name
				 AND c.table_schema = k.table_schema
				WHERE c.constraint_type = 'PRIMARY KEY'
				  AND k.table_schema = current_schema()
				  AND k.table_name = ? AND k.column_name = ?`,
				table.Name, column.Name).Scan(&inPrimaryKey).Error; err != nil {
				t.Fatalf("read the primary key of %s: %v", table.Name, err)
			}
			if inPrimaryKey == 0 {
				needed = append(needed, columnRef{table.Name, column.Name})
			}
		}
	}
	return needed
}

// TestMigration22WillNotInventATimestamp.
//
// A counter gets a zero: no retries yet, no attempts yet, version zero — all
// true statements about a row that does not say. A timestamp has no equivalent.
// created_at is part of the record of when something happened, and in a system
// whose whole job is to be able to answer "who approved this, and when", a
// migration that quietly writes now() over the gap has destroyed the answer and
// left something that looks like one.
//
// So it stops, and names the table, the column and the count, and leaves the
// decision with the person who can actually make it.
func TestMigration22WillNotInventATimestamp(t *testing.T) {
	db := testutils.SetupTestDB(t)
	ctx := t.Context()

	if _, err := migrations.Run(ctx, db, migrations.Schema(models.MigrationModels())); err != nil {
		t.Fatalf("run the migrations: %v", err)
	}

	// A row that says nothing about when it was received. Every constraint on
	// the table comes off first, because the point is to produce the row an
	// older schema allowed, not one this one does.
	for _, stmt := range []string{
		`ALTER TABLE webhook_deliveries ALTER COLUMN received_at DROP NOT NULL`,
		`ALTER TABLE webhook_deliveries ALTER COLUMN created_at DROP NOT NULL`,
		`ALTER TABLE webhook_deliveries ALTER COLUMN updated_at DROP NOT NULL`,
		`INSERT INTO webhook_deliveries (id) VALUES (gen_random_uuid())`,
		`DELETE FROM schema_migrations WHERE version = 22`,
	} {
		if err := db.WithContext(ctx).Exec(stmt).Error; err != nil {
			t.Fatalf("set up the old schema (%s): %v", stmt, err)
		}
	}

	_, err := migrations.Run(ctx, db, migrations.Schema(models.MigrationModels()))
	if err == nil {
		t.Fatal("the migration filled in a timestamp it could not know; it must refuse instead")
	}
	for _, want := range []string{"webhook_deliveries", "received_at"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("the refusal does not name %q, so nobody can act on it: %v", want, err)
		}
	}

	// And having refused, it must not have half-applied: the column is still
	// nullable, so the operator can fix the rows and run it again.
	var nullable string
	if err := db.WithContext(ctx).Raw(`
		SELECT is_nullable FROM information_schema.columns
		WHERE table_schema = current_schema()
		  AND table_name = 'webhook_deliveries' AND column_name = 'received_at'`).
		Scan(&nullable).Error; err != nil {
		t.Fatalf("read the column: %v", err)
	}
	if nullable != "YES" {
		t.Error("the migration refused and applied the constraint anyway")
	}
}
