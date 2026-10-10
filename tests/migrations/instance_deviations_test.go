package migrations_test

import (
	"strings"
	"testing"

	stormdb "github.com/gsoultan/metis/server/repositories/db"
	"github.com/gsoultan/metis/server/repositories/migrations"
	"github.com/gsoultan/metis/server/repositories/model"
	"github.com/gsoultan/metis/server/repositories/models"
	"github.com/gsoultan/metis/tests/testutils"
	"github.com/gsoultan/storm"
	"gorm.io/gorm"
)

// The ledger reaches a database two ways: an upgrading installation runs
// migration 33, whose DDL is frozen text, and a fresh one or a test schema gets
// the table from the storm model at boot. Two descriptions of one table drift
// unless something compares them, and drift here is a compliance record whose
// shape nobody can vouch for.
func TestMigration33CreatesTheLedgerAsTheModelDescribesIt(t *testing.T) {
	db := testutils.SetupTestDB(t)
	ctx := t.Context()
	schema := migrations.Schema(models.MigrationModels())
	migrate := func() {
		t.Helper()
		if _, err := migrations.Run(ctx, db, schema); err != nil {
			t.Fatalf("run the migrations: %v", err)
		}
	}
	migrate()

	// The database as an upgrading installation has it: no ledger, and
	// migrations 33 and 34 not yet run. Both, because 33 alone no longer
	// builds the model's table: 34 adds a column to it and changes its indexes.
	if err := db.WithContext(ctx).Exec(`DROP TABLE IF EXISTS instance_deviations`).Error; err != nil {
		t.Fatalf("remove the ledger: %v", err)
	}
	if err := db.WithContext(ctx).Exec(`DELETE FROM schema_migrations WHERE version IN (?, ?)`,
		migrations.InstanceDeviationsMigration, migrations.DeviationRequestsMigration).Error; err != nil {
		t.Fatalf("forget that migrations 33 and 34 ran: %v", err)
	}
	migrate()
	migrate() // a second run finds nothing to do

	want, err := storm.Build(model.All()...)
	if err != nil {
		t.Fatalf("build the model: %v", err)
	}
	drift, err := stormdb.ReportDrift(ctx, testutils.StormConn(db).Main(), want)
	if err != nil {
		t.Fatalf("report drift: %v", err)
	}
	for _, line := range drift {
		if strings.Contains(line, "instance_deviations") {
			t.Errorf("the table migrations 33 and 34 leave differs from the model: %s", line)
		}
	}
	for _, column := range []string{"visit_key", "request_id", "approved_by", "approved_by_id", "decided_at", "audit_entry_id", "run_id"} {
		if !ledgerHasColumn(t, db, "instance_deviations", column) {
			t.Errorf("migration 33 left out instance_deviations.%s, which 3a-2 and 3b write without a migration of their own", column)
		}
	}
}

// A hand-over holds the task's row and inserts; a completion holds the instance
// and then waits for the task's row. A foreign key from the ledger to either
// row makes the hand-over's insert wait for a lock the completion holds while
// the completion waits for the hand-over: a deadlock, answered as a 500. The
// rule is the one task.go states for audit_logs and notifications.
func TestTheLedgerHasNoForeignKeyToTheRowsAHandOverAndACompletionLock(t *testing.T) {
	db := testutils.SetupTestDB(t)
	ctx := t.Context()
	assertNoLockingForeignKey := func(how string) {
		t.Helper()
		var referenced []string
		if err := db.WithContext(ctx).Raw(`
			SELECT r.relname FROM pg_constraint c
			JOIN pg_class t ON t.oid = c.conrelid
			JOIN pg_class r ON r.oid = c.confrelid
			JOIN pg_namespace n ON n.oid = t.relnamespace
			WHERE c.contype = 'f' AND n.nspname = current_schema() AND t.relname = 'instance_deviations'`).
			Scan(&referenced).Error; err != nil {
			t.Fatalf("read the ledger's foreign keys: %v", err)
		}
		for _, table := range referenced {
			if table == "process_instances" || table == "tasks" {
				t.Errorf("the ledger built %s references %s; a hand-over racing a completion would deadlock", how, table)
			}
		}
	}
	assertNoLockingForeignKey("from the model")

	if err := db.WithContext(ctx).Exec(`DROP TABLE instance_deviations`).Error; err != nil {
		t.Fatalf("remove the ledger: %v", err)
	}
	// SetupTestDB builds the schema without the numbered migrations, so this
	// first run includes 33 and creates the table from its frozen DDL.
	if _, err := migrations.Run(ctx, db, migrations.Schema(models.MigrationModels())); err != nil {
		t.Fatalf("run the migrations: %v", err)
	}
	assertNoLockingForeignKey("by migration 33")
}

func ledgerHasColumn(t *testing.T, db *gorm.DB, table, column string) bool {
	t.Helper()
	var n int64
	if err := db.Raw(`SELECT count(*) FROM information_schema.columns
		WHERE table_schema = current_schema() AND table_name = ? AND column_name = ?`, table, column).
		Scan(&n).Error; err != nil {
		t.Fatalf("read %s.%s: %v", table, column, err)
	}
	return n == 1
}
