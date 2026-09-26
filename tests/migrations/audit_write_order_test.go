package migrations_test

import (
	"context"
	"database/sql"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	stormdb "github.com/gsoultan/metis/server/repositories/db"
	"github.com/gsoultan/metis/server/repositories/migrations"
	"github.com/gsoultan/metis/server/repositories/model"
	"github.com/gsoultan/metis/server/repositories/models"
	"github.com/gsoultan/metis/tests/testutils"
	"github.com/gsoultan/storm"
	"gorm.io/gorm"
)

// auditSequence is the sequence behind audit_logs.seq, named as PostgreSQL
// names the one behind a serial column.
const auditSequence = "audit_logs_seq_seq"

// An installation upgrading has an audit table whose entries record no order
// but their transaction's created_at, which every entry one transaction writes
// shares. This puts audit_logs back that way — no seq column, no sequence —
// with one transaction's entries already in it, and runs the migration at it.
func TestMigration28GivesEveryNewAuditEntryThePlaceItWasWrittenIn(t *testing.T) {
	db := testutils.SetupTestDB(t)
	ctx := t.Context()
	migrate := func() {
		t.Helper()
		if _, err := migrations.Run(ctx, db, migrations.Schema(models.MigrationModels())); err != nil {
			t.Fatalf("run the migrations: %v", err)
		}
	}
	rewind := func() {
		t.Helper()
		if err := db.WithContext(ctx).Exec(`DELETE FROM schema_migrations WHERE version = ?`,
			migrations.AuditWriteOrderMigration).Error; err != nil {
			t.Fatalf("rewind the migration record: %v", err)
		}
	}
	migrate()
	// A fresh install has the column and its sequence from the start.
	assertAuditEntriesAreNumberedAsWritten(t, db)

	for _, stmt := range []string{
		`ALTER TABLE audit_logs DROP COLUMN IF EXISTS seq`,
		`DROP SEQUENCE IF EXISTS ` + auditSequence,
	} {
		if err := db.WithContext(ctx).Exec(stmt).Error; err != nil {
			t.Fatalf("restore the old schema (%s): %v", stmt, err)
		}
	}
	rewind()
	project, instance := uuid.New(), uuid.New()
	existing := seedOneTransactionAsTheOldReleaseDid(t, db, project, instance, 3)

	migrate()
	migrate() // and a second run finds nothing left to do

	// Nothing recorded the order the existing entries were written in, so they
	// are not given one: an invented position is a falsified trail. They keep
	// the order they had.
	for _, id := range existing {
		if seq := seqOf(t, db, id); seq.Valid {
			t.Errorf("an entry written before the upgrade was given position %d", seq.Int64)
		}
	}

	// Every entry from here on is numbered by the database as it is written,
	// by whichever writer — this insert names no position at all.
	first := insertAuditEntry(t, db, project, instance)
	second := insertAuditEntry(t, db, project, instance)
	firstSeq, secondSeq := seqOf(t, db, first), seqOf(t, db, second)
	if !firstSeq.Valid || !secondSeq.Valid || firstSeq.Int64 >= secondSeq.Int64 {
		t.Fatalf("two entries written one after the other are numbered %v and %v, want increasing",
			firstSeq, secondSeq)
	}
	assertAuditEntriesAreNumberedAsWritten(t, db)

	// Run again after a failure part-way: it finishes the job and does not
	// start the numbering over.
	rewind()
	migrate()
	third := insertAuditEntry(t, db, project, instance)
	if thirdSeq := seqOf(t, db, third); !thirdSeq.Valid || thirdSeq.Int64 <= secondSeq.Int64 {
		t.Errorf("after running again the next entry is numbered %v, want after %d", thirdSeq, secondSeq.Int64)
	}
	for _, id := range existing {
		if seq := seqOf(t, db, id); seq.Valid {
			t.Errorf("running again gave an entry written before the upgrade position %d", seq.Int64)
		}
	}

	// And the column is what the storm model describes. A sequence the column
	// did not own would read back as a default naming it, and the boot drift
	// report would propose dropping it. (The table's other columns have drift
	// of their own, from before this migration; see tests/drift.)
	want, err := storm.Build(model.All()...)
	if err != nil {
		t.Fatalf("build the model: %v", err)
	}
	drift, err := stormdb.ReportDrift(ctx, testutils.StormConn(db).Main(), want)
	if err != nil {
		t.Fatalf("report drift: %v", err)
	}
	for _, line := range drift {
		if strings.Contains(line, `"audit_logs"`) && strings.Contains(line, `"seq"`) {
			t.Errorf("audit_logs.seq differs from the model: %s", line)
		}
	}
}

// A long read of the audit trail — an export, a report, an anti-wraparound
// vacuum — holds a lock the migration's ALTER TABLE has to wait for, and
// PostgreSQL queues every later writer of the table behind a waiting ALTER.
// Every step of every running process writes audit entries, so during a
// rolling upgrade one slow reader would stop the engine on the replicas still
// serving for as long as the read ran. The migration gives up after a bounded
// wait instead, says why, and runs when it is started again.
func TestMigration28GivesUpRatherThanHoldEveryAuditWriteBehindALongRead(t *testing.T) {
	db := testutils.SetupTestDB(t)
	ctx := t.Context()
	schema := migrations.Schema(models.MigrationModels())
	if _, err := migrations.Run(ctx, db, schema); err != nil {
		t.Fatalf("run the migrations: %v", err)
	}
	for _, stmt := range []string{
		`ALTER TABLE audit_logs DROP COLUMN IF EXISTS seq`,
		`DROP SEQUENCE IF EXISTS ` + auditSequence,
	} {
		if err := db.WithContext(ctx).Exec(stmt).Error; err != nil {
			t.Fatalf("restore the old schema (%s): %v", stmt, err)
		}
	}
	if err := db.WithContext(ctx).Exec(`DELETE FROM schema_migrations WHERE version = ?`,
		migrations.AuditWriteOrderMigration).Error; err != nil {
		t.Fatalf("rewind the migration record: %v", err)
	}

	pool, err := db.DB()
	if err != nil {
		t.Fatalf("open the pool: %v", err)
	}
	reader, err := pool.BeginTx(ctx, nil)
	if err != nil {
		t.Fatalf("begin the long read: %v", err)
	}
	defer func() { _ = reader.Rollback() }()
	// Holds ACCESS SHARE on audit_logs until the transaction ends.
	if _, err := reader.ExecContext(ctx, `SELECT count(*) FROM audit_logs`); err != nil {
		t.Fatalf("read the trail: %v", err)
	}

	finished := make(chan error, 1)
	go func() {
		_, err := migrations.Run(context.Background(), db, schema)
		finished <- err
	}()
	const patience = 20 * time.Second
	select {
	case err := <-finished:
		if err == nil {
			t.Fatal("the migration altered audit_logs while a reader held it")
		}
		if !strings.Contains(err.Error(), "audit_logs") {
			t.Errorf("the migration failed without saying what it waited for: %v", err)
		}
	case <-time.After(patience):
		_ = reader.Rollback()
		<-finished
		t.Fatalf("the migration was still waiting for audit_logs after %s, and every audit write queues behind it", patience)
	}

	// Once the reader is done, starting again finishes the job.
	if err := reader.Rollback(); err != nil {
		t.Fatalf("end the long read: %v", err)
	}
	if _, err := migrations.Run(ctx, db, schema); err != nil {
		t.Fatalf("run the migrations again once the read ended: %v", err)
	}
	assertAuditEntriesAreNumberedAsWritten(t, db)
}

// assertAuditEntriesAreNumberedAsWritten checks audit_logs.seq takes its value
// from a sequence the column owns, and stays nullable for the entries written
// before it existed.
func assertAuditEntriesAreNumberedAsWritten(t *testing.T, db *gorm.DB) {
	t.Helper()
	var column struct {
		Default  sql.NullString
		Nullable string
		Owned    sql.NullString
	}
	if err := db.WithContext(t.Context()).Raw(`
		SELECT column_default AS "default", is_nullable AS nullable,
		       pg_get_serial_sequence('audit_logs', 'seq') AS owned
		  FROM information_schema.columns
		 WHERE table_schema = current_schema() AND table_name = 'audit_logs' AND column_name = 'seq'`).
		Scan(&column).Error; err != nil {
		t.Fatalf("read audit_logs.seq: %v", err)
	}
	if !column.Default.Valid {
		t.Fatal("audit_logs.seq is missing or has no default, so an entry is written with no place in the trail")
	}
	if !strings.HasPrefix(column.Default.String, "nextval(") || !strings.Contains(column.Default.String, auditSequence) {
		t.Fatalf("audit_logs.seq defaults to %s, want the next value of %s", column.Default.String, auditSequence)
	}
	if !column.Owned.Valid {
		t.Fatalf("%s is not owned by audit_logs.seq", auditSequence)
	}
	if column.Nullable != "YES" {
		t.Fatal("audit_logs.seq is NOT NULL, which an upgrade could only reach by inventing positions for existing entries")
	}
}

// seedOneTransactionAsTheOldReleaseDid writes n entries in one statement, so
// they share its created_at, with only the columns the release before the
// migration knew about.
func seedOneTransactionAsTheOldReleaseDid(t *testing.T, db *gorm.DB, project, instance uuid.UUID, n int) []uuid.UUID {
	t.Helper()
	var ids []uuid.UUID
	if err := db.WithContext(t.Context()).Raw(`
		INSERT INTO audit_logs (id, created_at, updated_at, project_id, instance_id, type, node_id, message)
		SELECT gen_random_uuid(), now(), now(), ?, ?, 'NodeReached', 'step-' || n, 'A step was reached'
		  FROM generate_series(1, ?) AS n
		RETURNING id`, project, instance, n).Scan(&ids).Error; err != nil {
		t.Fatalf("seed the entries an upgrading installation already has: %v", err)
	}
	if len(ids) != n {
		t.Fatalf("seeded %d entries, want %d", len(ids), n)
	}
	return ids
}

// insertAuditEntry writes one entry the way every writer does: without a
// position, leaving it to the database.
func insertAuditEntry(t *testing.T, db *gorm.DB, project, instance uuid.UUID) uuid.UUID {
	t.Helper()
	id := uuid.New()
	if err := db.WithContext(t.Context()).Exec(`
		INSERT INTO audit_logs (id, created_at, updated_at, project_id, instance_id, type, message)
		VALUES (?, now(), now(), ?, ?, 'NodeReached', 'A step was reached')`,
		id, project, instance).Error; err != nil {
		t.Fatalf("write an audit entry: %v", err)
	}
	return id
}

func seqOf(t *testing.T, db *gorm.DB, id uuid.UUID) sql.NullInt64 {
	t.Helper()
	var seq sql.NullInt64
	if err := db.WithContext(t.Context()).Raw(`SELECT seq FROM audit_logs WHERE id = ?`, id).
		Row().Scan(&seq); err != nil {
		t.Fatalf("read the position of %s: %v", id, err)
	}
	return seq
}
