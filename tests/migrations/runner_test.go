// Package migrations_test proves the schema migration runner.
//
// This is the upgrade path for every existing deployment, so the cases that
// matter are the ones about a database that already has data: a step must run
// once and never again, a failure must not be recorded as success, two
// replicas starting together must not both create the schema, and a database
// the runner does not recognise must be refused with nothing changed.
package migrations_test

import (
	"context"
	"errors"
	"slices"
	"strings"
	"sync"
	"testing"

	"github.com/google/uuid"
	"github.com/gsoultan/metis/server/repositories/db"
	"github.com/gsoultan/metis/server/repositories/migrations"
	"github.com/gsoultan/metis/server/repositories/model"
	"github.com/gsoultan/metis/server/repositories/store/organization"
	"github.com/gsoultan/metis/server/repositories/store/schemamigration"
	"github.com/gsoultan/metis/tests/testutils"
	"github.com/gsoultan/storm"
	"github.com/gsoultan/storm/migrate"
	"github.com/gsoultan/storm/runtime"
	"github.com/gsoultan/storm/runtime/pgxdrv"
	"github.com/gsoultan/storm/schema"
	pgschema "github.com/gsoultan/storm/schema/pg"
	"github.com/jackc/pgx/v5/pgxpool"
)

// An empty database is created from the model and recorded as in its shape,
// with nothing a comparison against the model finds.
func TestAnEmptyDatabaseIsCreatedFromTheModel(t *testing.T) {
	pool := testutils.EmptySchema(t)
	ctx := t.Context()

	result, err := migrations.Run(ctx, pool, nil)
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if !result.Created || len(result.Applied) != 0 {
		t.Fatalf("result %+v; want the database created and nothing else applied", result)
	}
	if got := versions(t, pool); !slices.Equal(got, []int64{migrations.ConvertedVersion}) {
		t.Fatalf("recorded %v; want only %d", got, migrations.ConvertedVersion)
	}

	want, err := storm.Build(model.All()...)
	if err != nil {
		t.Fatalf("build the model: %v", err)
	}
	if drift := driftFrom(t, pool, want); len(drift) != 0 {
		t.Errorf("a database created from the model differs from it:\n%s", strings.Join(drift, "\n"))
	}

	again, err := migrations.Run(ctx, pool, nil)
	if err != nil {
		t.Fatalf("run again: %v", err)
	}
	if again.Created {
		t.Fatal("a second run created the database again")
	}
}

// Two replicas booting against one empty database create it once.
func TestReplicasStartingTogetherCreateTheDatabaseOnce(t *testing.T) {
	pool := testutils.EmptySchema(t)
	ctx := t.Context()

	const replicas = 4
	var wg sync.WaitGroup
	errs := make([]error, replicas)
	for i := range replicas {
		wg.Go(func() {
			_, errs[i] = migrations.Run(ctx, pool, nil)
		})
	}
	wg.Wait()
	for i, err := range errs {
		if err != nil {
			t.Errorf("replica %d: %v", i, err)
		}
	}
	if got := versions(t, pool); !slices.Equal(got, []int64{migrations.ConvertedVersion}) {
		t.Fatalf("recorded %v; want the creation once", got)
	}
}

// A step runs once, is recorded with its version, and is skipped from then
// on — and the rows the database had are still there after it.
func TestAStepRunsOnceAndRowsSurviveIt(t *testing.T) {
	pool := testutils.EmptySchema(t)
	ctx := t.Context()
	if _, err := migrations.Run(ctx, pool, nil); err != nil {
		t.Fatalf("create: %v", err)
	}
	kept := insertOrganization(t, pool, "already here")

	var runs int
	step := migrations.Migration{Version: migrations.ConvertedVersion + 1, Name: "count", Run: func(context.Context, runtime.Executor) error {
		runs++
		return nil
	}}
	for range 2 {
		if _, err := migrations.Run(ctx, pool, []migrations.Migration{step}); err != nil {
			t.Fatalf("run: %v", err)
		}
	}
	if runs != 1 {
		t.Fatalf("the step ran %d times; want once", runs)
	}
	if got := versions(t, pool); !slices.Equal(got, []int64{migrations.ConvertedVersion, step.Version}) {
		t.Fatalf("recorded %v", got)
	}
	if n, err := organization.New().Where(organization.ID.Eq(kept)).Count(ctx, pgxdrv.Pool{P: pool}); err != nil || n != 1 {
		t.Fatalf("the organization written before the step: %d rows (%v), want 1", n, err)
	}
}

// A step that fails leaves nothing behind: not its writes, not a record that
// it ran, and not any step after it.
func TestAFailingStepIsRolledBackAndStopsTheRun(t *testing.T) {
	pool := testutils.EmptySchema(t)
	ctx := t.Context()
	if _, err := migrations.Run(ctx, pool, nil); err != nil {
		t.Fatalf("create: %v", err)
	}

	written := uuid.Must(uuid.NewV7())
	broken := errors.New("broken step")
	var laterRan bool
	steps := []migrations.Migration{
		{Version: migrations.ConvertedVersion + 1, Name: "writes then fails", Run: func(ctx context.Context, ex runtime.Executor) error {
			org := organization.Create()
			org.SetID(written)
			org.SetName("half done")
			if _, err := org.Insert(ctx, ex); err != nil {
				return err
			}
			return broken
		}},
		{Version: migrations.ConvertedVersion + 2, Name: "after", Run: func(context.Context, runtime.Executor) error {
			laterRan = true
			return nil
		}},
	}
	if _, err := migrations.Run(ctx, pool, steps); !errors.Is(err, broken) {
		t.Fatalf("run: %v; want the step's error", err)
	}
	if laterRan {
		t.Error("a step after the failed one ran")
	}
	if got := versions(t, pool); !slices.Equal(got, []int64{migrations.ConvertedVersion}) {
		t.Errorf("recorded %v; want only the creation", got)
	}
	if n, err := organization.New().Where(organization.ID.Eq(written)).Count(ctx, pgxdrv.Pool{P: pool}); err != nil || n != 0 {
		t.Errorf("the failed step's write: %d rows (%v), want none", n, err)
	}
}

// A database with only some of GORM's migrations cannot be brought forward
// from here, and is told how to get there.
func TestADatabaseOlderThanGORMsLastVersionIsRefused(t *testing.T) {
	pool := testutils.EmptySchema(t)
	ctx := t.Context()
	if _, err := migrations.Run(ctx, pool, nil); err != nil {
		t.Fatalf("create: %v", err)
	}
	ex := pgxdrv.Pool{P: pool}
	if _, err := schemamigration.New().Where(schemamigration.Version.Eq(migrations.ConvertedVersion)).DeleteAll(ctx, ex); err != nil {
		t.Fatalf("forget the creation: %v", err)
	}
	old := schemamigration.Create()
	old.SetVersion(30)
	old.SetName("an older release")
	if _, err := old.Insert(ctx, ex); err != nil {
		t.Fatalf("record version 30: %v", err)
	}
	before := tables(t, pool)

	var ran bool
	step := migrations.Migration{Version: migrations.ConvertedVersion + 1, Name: "x", Run: func(context.Context, runtime.Executor) error {
		ran = true
		return nil
	}}
	_, err := migrations.Run(ctx, pool, []migrations.Migration{step})
	if !errors.Is(err, migrations.ErrBeforeBaseline) {
		t.Fatalf("run: %v; want ErrBeforeBaseline", err)
	}
	if !strings.Contains(err.Error(), "version 30") {
		t.Errorf("the refusal does not say where the database is: %v", err)
	}
	if ran {
		t.Error("a step ran against a database the runner refused")
	}
	if got := versions(t, pool); !slices.Equal(got, []int64{30}) {
		t.Errorf("recorded %v; want it left at 30", got)
	}
	if after := tables(t, pool); !slices.Equal(before, after) {
		t.Errorf("the tables changed: %v, then %v", before, after)
	}
}

// The product's tables and no record of how they got there is not a database
// this product made, and creating the model's tables over it would be guessing.
func TestTablesWithoutAMigrationRecordAreRefused(t *testing.T) {
	pool := testutils.EmptySchema(t)
	ctx := t.Context()
	want, err := migrations.Target()
	if err != nil {
		t.Fatalf("model: %v", err)
	}
	one := &schema.Schema{}
	for _, table := range want.Tables {
		if table.Name == organization.Table {
			one.Tables = append(one.Tables, table)
		}
	}
	if _, err := migrate.AutoPool(ctx, pool, one, migrate.AutoOptions{Schema: db.Namespace(pool)}); err != nil {
		t.Fatalf("create a stray table: %v", err)
	}

	if _, err := migrations.Run(ctx, pool, nil); !errors.Is(err, migrations.ErrUnknownDatabase) {
		t.Fatalf("run: %v; want ErrUnknownDatabase", err)
	}
	if got := tables(t, pool); !slices.Equal(got, []string{organization.Table}) {
		t.Fatalf("tables %v; want the stray one alone", got)
	}
}

// A database that holds somebody else's tables and none of the product's is
// empty as far as the product goes: created, with the other tables left alone.
func TestAnotherOwnersTablesAreLeftAlone(t *testing.T) {
	pool := testutils.EmptySchema(t)
	ctx := t.Context()
	theirs := &schema.Schema{Tables: []*schema.Table{{
		Name: "written_here",
		Columns: []*schema.Column{
			{Name: "id", Type: schema.Type{Name: "uuid"}, NotNull: true},
			{Name: "note", Type: schema.Type{Name: "text"}},
		},
		PrimaryKey: []string{"id"},
	}}}
	if _, err := migrate.AutoPool(ctx, pool, theirs, migrate.AutoOptions{Schema: db.Namespace(pool)}); err != nil {
		t.Fatalf("create their table: %v", err)
	}

	result, err := migrations.Run(ctx, pool, nil)
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if !result.Created {
		t.Fatal("the database was not created")
	}
	if !slices.Contains(tables(t, pool), "written_here") {
		t.Fatal("their table is gone")
	}
}

// Two steps with one version, or a step numbered into the history the
// conversion replaced, would mask one another for good. Refused before anything runs.
func TestStepsThatCannotBeTrackedAreRefused(t *testing.T) {
	pool := testutils.EmptySchema(t)
	noop := func(context.Context, runtime.Executor) error { return nil }
	for name, steps := range map[string][]migrations.Migration{
		"a shared version": {
			{Version: migrations.ConvertedVersion + 1, Name: "a", Run: noop},
			{Version: migrations.ConvertedVersion + 1, Name: "b", Run: noop},
		},
		"at the conversion":     {{Version: migrations.ConvertedVersion, Name: "a", Run: noop}},
		"before the conversion": {{Version: 12, Name: "a", Run: noop}},
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := migrations.Run(t.Context(), pool, steps); err == nil {
				t.Fatal("run accepted it")
			}
		})
	}
	if got := tables(t, pool); len(got) != 0 {
		t.Fatalf("a refused run created %v", got)
	}
}

// The migration steps this release ships are themselves trackable.
func TestTheShippedStepsAreTrackable(t *testing.T) {
	pool := testutils.EmptySchema(t)
	if _, err := migrations.Run(t.Context(), pool, migrations.Steps()); err != nil {
		t.Fatalf("run the shipped steps on an empty database: %v", err)
	}
}

func versions(t *testing.T, pool *pgxpool.Pool) []int64 {
	t.Helper()
	rows, err := schemamigration.New().Order(schemamigration.Version.Asc()).All(t.Context(), pgxdrv.Pool{P: pool}, nil)
	if err != nil {
		t.Fatalf("read the record: %v", err)
	}
	out := make([]int64, 0, len(rows))
	for _, row := range rows {
		out = append(out, row.Version)
	}
	return out
}

func tables(t *testing.T, pool *pgxpool.Pool) []string {
	t.Helper()
	conn, err := pool.Acquire(t.Context())
	if err != nil {
		t.Fatalf("borrow a connection: %v", err)
	}
	defer conn.Release()
	live, err := pgschema.Introspect(t.Context(), conn.Conn(), db.Namespace(pool))
	if err != nil {
		t.Fatalf("read the schema: %v", err)
	}
	names := make([]string, 0, len(live.Tables))
	for _, table := range live.Tables {
		names = append(names, table.Name)
	}
	slices.Sort(names)
	return names
}

func insertOrganization(t *testing.T, pool *pgxpool.Pool, name string) uuid.UUID {
	t.Helper()
	id := uuid.Must(uuid.NewV7())
	org := organization.Create()
	org.SetID(id)
	org.SetName(name)
	if _, err := org.Insert(t.Context(), pgxdrv.Pool{P: pool}); err != nil {
		t.Fatalf("write an organization: %v", err)
	}
	return id
}

func driftFrom(t *testing.T, pool *pgxpool.Pool, want *schema.Schema) []string {
	t.Helper()
	drift, err := db.ReportDrift(t.Context(), pool, want)
	if err != nil {
		t.Fatalf("compare: %v", err)
	}
	return drift
}

// A database made before the archive is given the table by step 51, in the
// shape the model declares; one created with it already is left as it is.
func TestTheArchiveStepBringsAnOlderDatabaseToTheModel(t *testing.T) {
	pool := testutils.EmptySchema(t)
	ctx := t.Context()
	if _, err := migrations.Run(ctx, pool, nil); err != nil {
		t.Fatalf("create: %v", err)
	}
	if _, err := pool.Exec(ctx, `DROP TABLE archived_instances`); err != nil {
		t.Fatalf("make the database one from before the archive: %v", err)
	}
	result, err := migrations.Run(ctx, pool, migrations.Steps())
	if err != nil {
		t.Fatalf("run the shipped steps: %v", err)
	}
	if !slices.Contains(result.Applied, 51) {
		t.Fatalf("result %+v; want step 51 applied", result)
	}
	want, err := storm.Build(model.All()...)
	if err != nil {
		t.Fatalf("build the model: %v", err)
	}
	if drift := driftFrom(t, pool, want); len(drift) != 0 {
		t.Errorf("after step 51 the database differs from the model:\n%s", strings.Join(drift, "\n"))
	}
}
