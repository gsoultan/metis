package migrations_test

import (
	_ "embed"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/gsoultan/metis/server/repositories/db"
	"github.com/gsoultan/metis/server/repositories/migrations"
	"github.com/gsoultan/metis/server/repositories/store/auditentry"
	"github.com/gsoultan/metis/server/repositories/store/installationsetting"
	"github.com/gsoultan/metis/server/repositories/store/organization"
	"github.com/gsoultan/metis/server/repositories/store/processdefinition"
	"github.com/gsoultan/metis/server/repositories/store/task"
	"github.com/gsoultan/metis/tests/testutils"
	"github.com/gsoultan/storm/migrate"
	"github.com/gsoultan/storm/runtime/pgxdrv"
	"github.com/gsoultan/storm/schema"
	"github.com/jackc/pgx/v5/pgxpool"
)

// gormV49 is the schema the last GORM release left every database in, read
// back from one it had migrated: the tables a conversion starts from.
//
//go:embed testdata/gorm_v49.json
var gormV49 []byte

// gormDatabase is a database as the last GORM release left it: its tables,
// and a record of versions 1 to 49.
//
// The rows it is given are written as that release wrote them, in SQL: the
// model has no way to describe a table it does not have, and these are the
// rows the conversion has to carry across.
func gormDatabase(t *testing.T, rows ...string) *pgxpool.Pool {
	t.Helper()
	var shape schema.Schema
	if err := json.Unmarshal(gormV49, &shape); err != nil {
		t.Fatalf("read the GORM schema: %v", err)
	}
	pool := testutils.EmptySchema(t)
	ctx := t.Context()
	if _, err := migrate.AutoPool(ctx, pool, &shape, migrate.AutoOptions{Schema: db.Namespace(pool)}); err != nil {
		t.Fatalf("create GORM's tables: %v", err)
	}
	// Migration 49, the last GORM release's, made the server settings by hand
	// rather than from GORM's models, so its table is not in the snapshot.
	rows = append(installationSettingsV49[:len(installationSettingsV49):len(installationSettingsV49)], rows...)
	rows = append(rows, `INSERT INTO schema_migrations (version, name, applied_at, duration_ms)
		SELECT v, 'gorm ' || v, now(), 1 FROM generate_series(1, 49) AS v`)
	for _, row := range rows {
		if _, err := pool.Exec(ctx, row); err != nil {
			t.Fatalf("write as GORM did: %v\n%s", err, row)
		}
	}
	return pool
}

// installationSettingsV49 is the table migration 49 made, as it made it.
var installationSettingsV49 = []string{
	`CREATE TABLE installation_settings (
    id uuid DEFAULT gen_random_uuid() NOT NULL,
    created_at timestamptz DEFAULT now() NOT NULL,
    updated_at timestamptz DEFAULT now() NOT NULL,
    name varchar(100) NOT NULL,
    value text,
    sealed boolean NOT NULL,
    reason text NOT NULL,
    set_by varchar(255) NOT NULL,
    set_by_id uuid NOT NULL,
    PRIMARY KEY (id)
)`,
	`CREATE INDEX ix_installation_settings_name ON installation_settings (name, created_at, id)`,
}

const (
	gormOrg      = "0190a3c1-0000-7000-8000-000000000001"
	gormProject  = "0190a3c1-0000-7000-8000-000000000002"
	gormDef      = "0190a3c1-0000-7000-8000-000000000003"
	gormInstance = "0190a3c1-0000-7000-8000-000000000004"
	gormTask     = "0190a3c1-0000-7000-8000-000000000005"
	gormAudit    = "0190a3c1-0000-7000-8000-000000000006"
	gormSetting  = "0190a3c1-0000-7000-8000-000000000007"
)

// The rows GORM left: NULL where it wrote nothing, and JSON as text, the empty
// string included, which GORM wrote for an empty list.
var gormRows = []string{
	`INSERT INTO installation_settings (id, name, value, sealed, reason, set_by, set_by_id)
		VALUES ('` + gormSetting + `', 'NOTIFICATION_SMTP_HOST', 'smtp.example.com', false, 'moved', 'ada', gen_random_uuid())`,
	`INSERT INTO organizations (id, created_at, updated_at, name) VALUES ('` + gormOrg + `', now(), now(), NULL)`,
	`INSERT INTO projects (id, created_at, updated_at, organization_id, name) VALUES ('` + gormProject + `', now(), now(), '` + gormOrg + `', 'P')`,
	`INSERT INTO process_definitions (id, created_at, updated_at, project_id, key, name, version, nodes, flows)
		VALUES ('` + gormDef + `', now(), now(), '` + gormProject + `', 'k', NULL, 1, '[{"id":"start"}]', '')`,
	`INSERT INTO process_instances (id, created_at, updated_at, project_id, definition_id, status, tokens)
		VALUES ('` + gormInstance + `', now(), now(), '` + gormProject + `', '` + gormDef + `', 'active', NULL)`,
	`INSERT INTO tasks (id, created_at, updated_at, project_id, instance_id, node_id, name, type, status, candidate_users, priority)
		VALUES ('` + gormTask + `', now(), now(), '` + gormProject + `', '` + gormInstance + `', 'n', 'Approve', 'user', 'unclaimed', '["ada"]', 50)`,
	`INSERT INTO audit_logs (id, created_at, updated_at, project_id, instance_id, type, message)
		VALUES ('` + gormAudit + `', now(), now(), '` + gormProject + `', '` + gormInstance + `', 'started', 'm')`,
}

// A database the last GORM release left is brought to the model's shape, once:
// nothing the model and it disagree about is left, the record says so, and
// every row reads as it did — a NULL as what GORM read it as, and JSON held as
// text as the same JSON.
func TestAGORMDatabaseIsConvertedToTheModel(t *testing.T) {
	pool := gormDatabase(t, gormRows...)
	ctx := t.Context()

	result, err := migrations.Run(ctx, pool, nil)
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if !result.Converted || result.Created {
		t.Fatalf("result %+v; want the database converted", result)
	}
	want, err := migrations.Target()
	if err != nil {
		t.Fatal(err)
	}
	if drift := driftFrom(t, pool, want); len(drift) != 0 {
		t.Errorf("a converted database still differs from the model:\n%s", strings.Join(drift, "\n"))
	}
	if got := versions(t, pool); len(got) != 50 || got[49] != migrations.ConvertedVersion {
		t.Fatalf("recorded %v; want 1 to 49 and the conversion", got)
	}
	if !contains(tables(t, pool), "schema_migration_locks") {
		t.Error("GORM's own table, which the model does not declare, was dropped")
	}

	ex := pgxdrv.Pool{P: pool}
	org, _, err := organization.New().Where(organization.ID.Eq(uuid.MustParse(gormOrg))).One(ctx, ex)
	if err != nil || org.Name != "" {
		t.Errorf("the organization GORM wrote no name for reads %q (%v), want the empty name it always read", org.Name, err)
	}
	def, _, err := processdefinition.New().Where(processdefinition.ID.Eq(uuid.MustParse(gormDef))).One(ctx, ex)
	if err != nil {
		t.Fatalf("read the definition: %v", err)
	}
	if string(def.Nodes) != `[{"id": "start"}]` || string(def.Flows) != "null" || def.Name != "" {
		t.Errorf("the definition reads nodes %s, flows %s and name %q; want the nodes it held, null and an empty name",
			def.Nodes, def.Flows, def.Name)
	}
	tk, _, err := task.New().Where(task.ID.Eq(uuid.MustParse(gormTask))).One(ctx, ex)
	if err != nil || string(tk.CandidateUsers) != `["ada"]` || string(tk.CandidateGroups) != "null" {
		t.Errorf("the task reads candidates %s and %s (%v); want [\"ada\"] and null", tk.CandidateUsers, tk.CandidateGroups, err)
	}
	entry, _, err := auditentry.New().Where(auditentry.ID.Eq(uuid.MustParse(gormAudit))).One(ctx, ex)
	if err != nil {
		t.Fatalf("read the audit entry: %v", err)
	}
	if string(entry.Data) != "null" {
		t.Errorf("the audit entry's data reads %s, want null", entry.Data)
	}

	setting, _, err := installationsetting.New().Where(installationsetting.ID.Eq(uuid.MustParse(gormSetting))).One(ctx, ex)
	if err != nil || setting.Name != "NOTIFICATION_SMTP_HOST" || setting.Value.V != "smtp.example.com" {
		t.Errorf("the setting migration 49's table held reads %q = %q (%v), want it carried across", setting.Name, setting.Value.V, err)
	}

	again, err := migrations.Run(ctx, pool, nil)
	if err != nil || again.Converted {
		t.Fatalf("a second run: %+v, %v; want nothing to do", again, err)
	}
}

// A row whose reference GORM left NULL has nothing it can be given: the
// conversion fails on it and changes nothing at all, so the operator can mend
// the row and start again.
func TestAConversionThatMeetsANullReferenceChangesNothing(t *testing.T) {
	orphan := `INSERT INTO tasks (id, created_at, updated_at, project_id, instance_id, node_id, name, type, status, priority)
		VALUES (gen_random_uuid(), now(), now(), '` + gormProject + `', NULL, 'n', 'Orphan', 'user', 'unclaimed', 50)`
	pool := gormDatabase(t, append(append([]string{}, gormRows...), orphan)...)
	ctx := t.Context()
	want, err := migrations.Target()
	if err != nil {
		t.Fatal(err)
	}
	before := driftFrom(t, pool, want)

	_, err = migrations.Run(ctx, pool, nil)
	if err == nil || !strings.Contains(err.Error(), "instance_id") {
		t.Fatalf("run: %v; want the conversion refused, naming the column", err)
	}
	if got := versions(t, pool); len(got) != 49 {
		t.Fatalf("recorded %d versions; want GORM's 49 alone", len(got))
	}
	if after := driftFrom(t, pool, want); strings.Join(after, "\n") != strings.Join(before, "\n") {
		t.Fatal("a refused conversion changed the tables")
	}
	var unknown *migrate.DestructiveError
	if errors.As(err, &unknown) {
		t.Fatalf("the conversion was refused as a plan rather than failing on the row: %v", err)
	}
}

func contains(xs []string, x string) bool {
	for _, v := range xs {
		if v == x {
			return true
		}
	}
	return false
}
