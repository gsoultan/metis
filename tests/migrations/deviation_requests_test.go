package migrations_test

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/gsoultan/metis/server/domains/entities"
	observersimpl "github.com/gsoultan/metis/server/domains/observers/impl"
	"github.com/gsoultan/metis/server/domains/services"
	"github.com/gsoultan/metis/server/repositories"
	stormdb "github.com/gsoultan/metis/server/repositories/db"
	"github.com/gsoultan/metis/server/repositories/migrations"
	"github.com/gsoultan/metis/server/repositories/model"
	"github.com/gsoultan/metis/server/repositories/models"
	"github.com/gsoultan/metis/tests/testutils"
	"github.com/gsoultan/storm"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

// ledgerBefore34 is one database as slice 3a left it: every migration up to 33
// run, a project to hang ledger rows on, and nothing of migration 34.
type ledgerBefore34 struct {
	db       *gorm.DB
	schema   []migrations.Migration
	project  uuid.UUID
	instance uuid.UUID
}

func newLedgerBefore34(t *testing.T) ledgerBefore34 {
	t.Helper()
	db := testutils.SetupTestDB(t)
	l := ledgerBefore34{db: db, schema: migrations.Schema(models.MigrationModels()), instance: uuid.Must(uuid.NewV7())}
	l.migrate(t)
	// What the application does next at boot. The baseline's AutoMigrate has
	// just taken the defaults off the timestamps storm leaves to the database,
	// and nothing storm writes could be inserted without them.
	want, err := storm.Build(model.All()...)
	if err != nil {
		t.Fatalf("build the model: %v", err)
	}
	if _, err := stormdb.EnsureColumnDefaults(t.Context(), testutils.StormConn(db).Main(), want); err != nil {
		t.Fatalf("restore the storm column defaults: %v", err)
	}

	svc := services.NewServiceFacade(repositories.NewRepository(testutils.StormConn(db)),
		observersimpl.NewEventDispatcher(), observersimpl.NewSSEObserver(), "m34-test", nil, nil, nil)
	org, err := svc.CreateOrganization(context.Background(), "M34 Org", "")
	if err != nil {
		t.Fatalf("create organization: %v", err)
	}
	project, err := svc.CreateProject(entities.WithTenantContext(context.Background(),
		entities.TenantContext{TenantID: org.ID.String()}), org.ID, "M34", "")
	if err != nil {
		t.Fatalf("create project: %v", err)
	}
	l.project = project.ID

	// The test schema is built from the current model, so what 34 adds is taken
	// back off and what it removes is put back.
	for _, stmt := range []string{
		`DROP TABLE IF EXISTS deviation_requests CASCADE`,
		`DROP INDEX IF EXISTS ux_instance_deviations_live_visit`,
		`DROP INDEX IF EXISTS ix_instance_deviations_visit`,
		`DROP INDEX IF EXISTS ix_instance_deviations_request_id`,
		`ALTER TABLE instance_deviations DROP COLUMN IF EXISTS live_visit_key`,
		`CREATE UNIQUE INDEX IF NOT EXISTS ux_instance_deviations_visit ON instance_deviations (instance_id, visit_key)`,
	} {
		l.exec(t, stmt)
	}
	l.forget(t)
	return l
}

func (l ledgerBefore34) exec(t *testing.T, stmt string, args ...any) {
	t.Helper()
	if err := l.db.WithContext(t.Context()).Exec(stmt, args...).Error; err != nil {
		t.Fatalf("%s: %v", stmt, err)
	}
}

// quiet is the same database without GORM's statement log, for a statement the
// test expects to fail or to wait: GORM prints every error and every slow
// statement, and a failure that was asked for is not something to read past.
func (l ledgerBefore34) quiet() *gorm.DB {
	return l.db.Session(&gorm.Session{Logger: logger.Discard})
}

func (l ledgerBefore34) forget(t *testing.T) {
	t.Helper()
	l.exec(t, `DELETE FROM schema_migrations WHERE version = ?`, migrations.DeviationRequestsMigration)
}

func (l ledgerBefore34) migrate(t *testing.T) {
	t.Helper()
	if _, err := migrations.Run(t.Context(), l.db, l.schema); err != nil {
		t.Fatalf("run the migrations: %v", err)
	}
}

// insert writes a ledger row the way a release before migration 34 did: every
// column 33 made NOT NULL, and no live key. visit is a string, or nil for a row
// that belongs to no visit (a hand-over, an edit).
func (l ledgerBefore34) insert(t *testing.T, status string, visit any) error {
	t.Helper()
	return l.db.WithContext(t.Context()).Exec(`INSERT INTO instance_deviations
		(project_id, instance_id, kind, scope, origin, status, actor, before, after, details, run_id, visit_key)
		VALUES (?, ?, 'hold', 'instance', 'in_place', ?, 'ana', '{}', '{}', '{}', ?, ?)`,
		l.project, l.instance, status, uuid.Must(uuid.NewV7()), visit).Error
}

func (l ledgerBefore34) mustInsert(t *testing.T, status string, visit any) {
	t.Helper()
	if err := l.insert(t, status, visit); err != nil {
		t.Fatalf("write a %s row for visit %v: %v", status, visit, err)
	}
}

// liveKey is the live key of the one row of a visit, "" when it has none.
func (l ledgerBefore34) liveKey(t *testing.T, visit string) string {
	t.Helper()
	var live string
	if err := l.db.WithContext(t.Context()).Raw(
		`SELECT COALESCE(live_visit_key, '') FROM instance_deviations WHERE visit_key = ?`, visit).
		Row().Scan(&live); err != nil {
		t.Fatalf("read the live key of %s: %v", visit, err)
	}
	return live
}

func (l ledgerBefore34) count(t *testing.T, query string, args ...any) int64 {
	t.Helper()
	var n int64
	if err := l.db.WithContext(t.Context()).Raw(query, args...).Row().Scan(&n); err != nil {
		t.Fatalf("%s: %v", query, err)
	}
	return n
}

// indexState answers whether an index of this schema exists and whether the
// planner may use it. A concurrent build that failed leaves one that exists and
// is not valid.
func (l ledgerBefore34) indexState(t *testing.T, name string) (exists, valid bool) {
	t.Helper()
	var states []bool
	if err := l.db.WithContext(t.Context()).Raw(`
		SELECT i.indisvalid FROM pg_class c JOIN pg_index i ON i.indexrelid = c.oid
		 WHERE c.relname = ? AND c.relnamespace = current_schema()::regnamespace`, name).
		Scan(&states).Error; err != nil {
		t.Fatalf("look for %s: %v", name, err)
	}
	return len(states) == 1, len(states) == 1 && states[0]
}

// requestForeignKey answers whether the ledger's reference to its request
// exists in this schema and whether every row has been checked against it.
func (l ledgerBefore34) requestForeignKey(t *testing.T) (exists, validated bool) {
	t.Helper()
	var states []bool
	if err := l.db.WithContext(t.Context()).Raw(`
		SELECT c.convalidated FROM pg_constraint c
		 WHERE c.conname = 'fk_instance_deviations_request_id'
		   AND c.connamespace = current_schema()::regnamespace`).Scan(&states).Error; err != nil {
		t.Fatalf("look for the request foreign key: %v", err)
	}
	return len(states) == 1, len(states) == 1 && states[0]
}

// assertAfter34 checks everything migration 34 leaves behind, however many
// attempts it took: both tables as the model describes them, the new indexes
// usable, 33's unique index gone, the foreign key checked, and the run recorded.
func (l ledgerBefore34) assertAfter34(t *testing.T) {
	t.Helper()
	want, err := storm.Build(model.All()...)
	if err != nil {
		t.Fatalf("build the model: %v", err)
	}
	drift, err := stormdb.ReportDrift(t.Context(), testutils.StormConn(l.db).Main(), want)
	if err != nil {
		t.Fatalf("report drift: %v", err)
	}
	for _, line := range drift {
		if strings.Contains(line, "deviation_requests") || strings.Contains(line, "instance_deviations") {
			t.Errorf("migration 34 left a table that differs from the model: %s", line)
		}
	}
	for _, name := range []string{
		"ux_instance_deviations_live_visit", "ix_instance_deviations_visit", "ix_instance_deviations_request_id",
		"ux_deviation_requests_live_key", "ix_deviation_requests_queue", "ix_deviation_requests_sweep",
	} {
		if _, valid := l.indexState(t, name); !valid {
			t.Errorf("%s is missing or not valid after migration 34", name)
		}
	}
	if exists, _ := l.indexState(t, "ux_instance_deviations_visit"); exists {
		t.Error("migration 33's unique visit index is still there; a waive somebody rejected could never be asked for again")
	}
	if exists, validated := l.requestForeignKey(t); !exists || !validated {
		t.Errorf("the ledger's reference to its request: exists %v, validated %v; want both", exists, validated)
	}
	if n := l.count(t, `SELECT count(*) FROM schema_migrations WHERE version = ?`, migrations.DeviationRequestsMigration); n != 1 {
		t.Errorf("migration 34 is recorded %d times; want once", n)
	}
}

// An installation upgrading from 3a has the ledger with 33's plain unique
// visit index, which would forbid asking again for a waive somebody rejected.
// Migration 34 adds the requests, moves uniqueness onto a key that is set only
// while a row is live, and must leave a table the model agrees with.
func TestMigration34AddsTheRequestsAndFreesAVisitOnceDecided(t *testing.T) {
	l := newLedgerBefore34(t)
	l.mustInsert(t, "applied", "dv1-held-in-3a")

	l.migrate(t)
	l.migrate(t) // a second run finds nothing to do

	if live := l.liveKey(t, "dv1-held-in-3a"); live != "dv1-held-in-3a" {
		t.Fatalf("a 3a row's live key is %q; a live row must keep holding its visit", live)
	}
	l.assertAfter34(t)

	lockedRefs := l.count(t, `SELECT count(*) FROM pg_constraint c JOIN pg_class t ON t.oid = c.conrelid JOIN pg_class r ON r.oid = c.confrelid
		WHERE c.contype = 'f' AND c.connamespace = current_schema()::regnamespace
		  AND t.relname = 'deviation_requests' AND r.relname IN ('process_instances', 'tasks')`)
	if lockedRefs != 0 {
		t.Fatal("deviation_requests references a row a completion locks; a request racing a completion could deadlock")
	}

	// One live row per visit; a decided one frees it.
	l.mustInsert(t, "pending_approval", "dv1-asked")
	l.exec(t, `UPDATE instance_deviations SET live_visit_key = visit_key WHERE visit_key = 'dv1-asked'`)
	if err := l.insert(t, "pending_approval", "dv1-asked"); err != nil {
		t.Fatalf("a second row without a live key was refused: %v", err)
	}
	if err := l.quiet().WithContext(t.Context()).Exec(`UPDATE instance_deviations SET live_visit_key = visit_key
		WHERE visit_key = 'dv1-asked' AND live_visit_key IS NULL`).Error; err == nil {
		t.Fatal("two live rows for one visit were accepted")
	}
	l.exec(t, `UPDATE instance_deviations SET status = 'rejected', live_visit_key = NULL WHERE live_visit_key = 'dv1-asked'`)
	l.exec(t, `UPDATE instance_deviations SET live_visit_key = visit_key WHERE visit_key = 'dv1-asked' AND status = 'pending_approval'`)
}

// The live key is what holds a visit, so the upgrade gives one to exactly the
// rows that hold theirs — applied, or waiting for approval — and to no other:
// a key on a decided row would go on refusing a new request for its visit, and
// a row that belongs to no visit has nothing to hold. More rows than one batch,
// because a backfill that stops after its first batch passes every small test.
func TestMigration34GivesALiveKeyToEveryRowThatHoldsItsVisitAndToNoOther(t *testing.T) {
	l := newLedgerBefore34(t)
	const manyVisits = 5003 // one batch is 5000
	l.exec(t, `INSERT INTO instance_deviations
		(project_id, instance_id, kind, scope, origin, status, actor, before, after, details, run_id, visit_key)
		SELECT ?, ?, 'hold', 'instance', 'in_place', 'applied', 'ana', '{}'::jsonb, '{}'::jsonb, '{}'::jsonb, gen_random_uuid(), 'dv1-many-' || n
		  FROM generate_series(1, ?) AS n`, l.project, l.instance, manyVisits)
	l.mustInsert(t, "pending_approval", "dv1-waiting")
	for _, decided := range []string{"rejected", "expired", "stale"} {
		l.mustInsert(t, decided, "dv1-"+decided)
	}
	l.mustInsert(t, "applied", nil) // a hand-over: no visit

	l.migrate(t)

	held := l.count(t, `SELECT count(*) FROM instance_deviations WHERE live_visit_key IS NOT NULL`)
	if held != manyVisits+1 {
		t.Errorf("%d rows hold a visit after the upgrade; want %d: the %d applied and the one waiting", held, manyVisits+1, manyVisits)
	}
	if wrong := l.count(t, `SELECT count(*) FROM instance_deviations
		WHERE live_visit_key IS NOT NULL AND live_visit_key IS DISTINCT FROM visit_key`); wrong != 0 {
		t.Errorf("%d rows hold a key that is not their own visit's", wrong)
	}
	if live := l.liveKey(t, "dv1-waiting"); live != "dv1-waiting" {
		t.Errorf("a row waiting for approval has the live key %q; want its visit key", live)
	}
	for _, decided := range []string{"rejected", "expired", "stale"} {
		if live := l.liveKey(t, "dv1-"+decided); live != "" {
			t.Errorf("a %s row was given the live key %q; it would refuse a new request for its visit for good", decided, live)
		}
	}
	l.assertAfter34(t)
}

// Adding the column needs the ledger to itself for as long as the catalogue
// takes, and while the ALTER TABLE waits for a reader, PostgreSQL queues every
// later reader and writer of the ledger behind it — every hand-over, while a
// canary runs the release's migrations beside the stable pods. The migration
// gives up after a bounded wait instead, says what it waited for, and finishes
// when it is started again.
func TestMigration34GivesUpRatherThanHoldTheLedgerBehindALongReadAndFinishesLater(t *testing.T) {
	l := newLedgerBefore34(t)
	ctx := t.Context()
	l.mustInsert(t, "applied", "dv1-held-in-3a")

	pool, err := l.db.DB()
	if err != nil {
		t.Fatalf("open the pool: %v", err)
	}
	reader, err := pool.BeginTx(ctx, nil)
	if err != nil {
		t.Fatalf("begin the long read: %v", err)
	}
	defer func() { _ = reader.Rollback() }()
	// Holds ACCESS SHARE on the ledger until the transaction ends.
	if _, err := reader.ExecContext(ctx, `SELECT count(*) FROM instance_deviations`); err != nil {
		t.Fatalf("read the ledger: %v", err)
	}

	finished := make(chan error, 1)
	go func() {
		_, err := migrations.Run(context.Background(), l.quiet(), l.schema)
		finished <- err
	}()
	const patience = 20 * time.Second
	select {
	case err := <-finished:
		if err == nil {
			t.Fatal("the migration altered the ledger while a reader held it")
		}
		if !strings.Contains(err.Error(), "instance_deviations was held") {
			t.Errorf("the migration failed without saying what it waited for: %v", err)
		}
	case <-time.After(patience):
		_ = reader.Rollback()
		<-finished
		t.Fatalf("the migration was still waiting for the ledger after %s, and every hand-over queues behind it", patience)
	}
	if n := l.count(t, `SELECT count(*) FROM schema_migrations WHERE version = ?`, migrations.DeviationRequestsMigration); n != 0 {
		t.Fatal("a migration that gave up is recorded as run; it would never finish")
	}
	// It stopped exactly between its two transactions: the requests table is
	// committed, and the ledger has neither its column nor its reference. The
	// second start has to pass over the first half and do the second.
	if n := l.count(t, `SELECT count(*) FROM information_schema.tables
		WHERE table_schema = current_schema() AND table_name = 'deviation_requests'`); n != 1 {
		t.Fatalf("the requests table exists %d times after the first transaction; this test no longer stops between the two", n)
	}
	if ledgerHasColumn(t, l.db, "instance_deviations", "live_visit_key") {
		t.Fatal("the ledger was altered while a reader held it")
	}
	if exists, _ := l.requestForeignKey(t); exists {
		t.Fatal("the ledger was given its reference while a reader held it")
	}

	if err := reader.Rollback(); err != nil {
		t.Fatalf("end the long read: %v", err)
	}
	l.migrate(t)
	if live := l.liveKey(t, "dv1-held-in-3a"); live != "dv1-held-in-3a" {
		t.Errorf("after the second start a 3a row's live key is %q; want its visit key", live)
	}
	l.assertAfter34(t)
}

// A run that stops while it builds the new unique index must leave 33's in
// place: until the new one is valid, the old one is the only thing in the
// database that refuses a second live row for a visit. Started again, it clears
// the wreck of the failed build and finishes.
//
// The build is made to fail by two live rows of one instance under one key.
// Nothing in the product writes that; it stands for whatever stops a concurrent
// build — a deadlock, a cancelled statement, a full disk — all of which leave
// the same index behind, present and not valid.
func TestMigration34KeepsTheOldVisitIndexUntilTheNewOneIsBuiltAndFinishesWhenStartedAgain(t *testing.T) {
	l := newLedgerBefore34(t)
	l.mustInsert(t, "applied", "dv1-first")
	l.mustInsert(t, "applied", "dv1-second")
	// An earlier attempt got as far as the column, and one row holds the other's key.
	l.exec(t, `ALTER TABLE instance_deviations ADD COLUMN live_visit_key varchar(64)`)
	l.exec(t, `UPDATE instance_deviations SET live_visit_key = 'dv1-second' WHERE visit_key = 'dv1-first'`)

	_, err := migrations.Run(t.Context(), l.quiet(), l.schema)
	if err == nil {
		t.Fatal("the migration built a unique index over two rows holding one key")
	}
	if !strings.Contains(err.Error(), "ux_instance_deviations_live_visit") {
		t.Errorf("the migration failed without naming the index it could not build: %v", err)
	}
	if exists, valid := l.indexState(t, "ux_instance_deviations_visit"); !exists || !valid {
		t.Fatalf("migration 33's unique visit index after the failed build: exists %v, valid %v; "+
			"it was dropped before anything replaced it", exists, valid)
	}
	if exists, valid := l.indexState(t, "ux_instance_deviations_live_visit"); !exists || valid {
		t.Fatalf("the new index after the failed build: exists %v, valid %v; want the wreck a failed concurrent build leaves, "+
			"or this test is not exercising a restart over one", exists, valid)
	}
	if _, validated := l.requestForeignKey(t); validated {
		t.Error("the request foreign key was validated by a run that never got that far")
	}
	if n := l.count(t, `SELECT count(*) FROM schema_migrations WHERE version = ?`, migrations.DeviationRequestsMigration); n != 0 {
		t.Fatal("a migration that stopped part-way is recorded as run; it would never finish")
	}

	l.exec(t, `UPDATE instance_deviations SET live_visit_key = visit_key WHERE visit_key = 'dv1-first'`)
	l.migrate(t)
	for _, visit := range []string{"dv1-first", "dv1-second"} {
		if live := l.liveKey(t, visit); live != visit {
			t.Errorf("after the second start the row of %s has the live key %q", visit, live)
		}
	}
	l.assertAfter34(t)
}

// No release before this one wrote request_id, so the reference migration 34
// adds holds for every row an installation has. A row that names a request
// nobody made was written by hand, and the upgrade does not decide what a
// compliance record should have said: it stops, says which column of which
// table, leaves the row as it found it, and finishes once somebody has
// corrected it.
func TestMigration34StopsAtALedgerRowThatNamesARequestNobodyMadeAndLeavesItAlone(t *testing.T) {
	l := newLedgerBefore34(t)
	l.mustInsert(t, "applied", "dv1-hand-edited")
	l.exec(t, `UPDATE instance_deviations SET request_id = gen_random_uuid() WHERE visit_key = 'dv1-hand-edited'`)

	_, err := migrations.Run(t.Context(), l.quiet(), l.schema)
	if err == nil {
		t.Fatal("the migration validated a reference to a request that does not exist")
	}
	for _, want := range []string{"instance_deviations", "request_id", "deviation_requests", "started again"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("the refusal does not say %q, so nobody can act on it: %v", want, err)
		}
	}
	if n := l.count(t, `SELECT count(*) FROM instance_deviations WHERE visit_key = 'dv1-hand-edited' AND request_id IS NOT NULL`); n != 1 {
		t.Error("the migration cleared or removed the row it could not vouch for")
	}
	if n := l.count(t, `SELECT count(*) FROM schema_migrations WHERE version = ?`, migrations.DeviationRequestsMigration); n != 0 {
		t.Fatal("a migration that stopped is recorded as run; the reference would never be validated")
	}

	l.exec(t, `UPDATE instance_deviations SET request_id = NULL WHERE visit_key = 'dv1-hand-edited'`)
	l.migrate(t)
	l.assertAfter34(t)
}

// The backfill picks its rows and then updates them, and a row can be decided
// between the two: the update waits for the row, and when it gets it the row is
// no longer one that holds its visit. Root cause of the bug this pins: with the
// predicates only on the statement that picks, PostgreSQL re-checks just the id
// against the row as it now is, and a rejected row is given a key that refuses
// every later request for its visit.
func TestMigration34DoesNotGiveALiveKeyToARowDecidedWhileItRan(t *testing.T) {
	l := newLedgerBefore34(t)
	ctx := t.Context()
	l.migrate(t)
	// The column is in place; only the backfill runs. A row as a 3a pod still
	// serving beside the upgrade writes it: live, with no key.
	l.mustInsert(t, "pending_approval", "dv1-decided-meanwhile")

	pool, err := l.db.DB()
	if err != nil {
		t.Fatalf("open the pool: %v", err)
	}
	decision, err := pool.BeginTx(ctx, nil)
	if err != nil {
		t.Fatalf("begin the decision: %v", err)
	}
	defer func() { _ = decision.Rollback() }()
	if _, err := decision.ExecContext(ctx,
		`UPDATE instance_deviations SET status = 'rejected' WHERE visit_key = 'dv1-decided-meanwhile'`); err != nil {
		t.Fatalf("reject the row: %v", err)
	}

	finished := make(chan error, 1)
	go func() { finished <- migrations.FillLiveVisitKeys(context.Background(), l.quiet()) }()
	time.Sleep(500 * time.Millisecond) // the backfill has picked the row and waits on its lock
	if err := decision.Commit(); err != nil {
		t.Fatalf("commit the decision: %v", err)
	}
	select {
	case err := <-finished:
		if err != nil {
			t.Fatalf("the backfill failed: %v", err)
		}
	case <-time.After(20 * time.Second):
		t.Fatal("the backfill did not finish after the decision committed")
	}
	if live := l.liveKey(t, "dv1-decided-meanwhile"); live != "" {
		t.Fatalf("a row rejected while the backfill ran was given the live key %q; its visit could never be asked for again", live)
	}
}
