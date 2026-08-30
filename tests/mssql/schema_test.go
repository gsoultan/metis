package mssql_test

import (
	"testing"

	"github.com/gsoultan/metis/server/repositories/models"
	"github.com/gsoultan/metis/tests/testutils"
)

// The whole schema, created on SQL Server.
//
// SQL Server is offered in the config and the setup wizard, and until now
// nothing had ever created a table on it. The models declared `uuid` columns,
// which PostgreSQL and SQLite accept and SQL Server does not, so AutoMigrate
// would have failed on the first table and the server could not have started.
//
// models.UUID now answers char(36) for SQL Server as it does for MySQL — the
// unit test in server/repositories/models covers that decision without needing
// a server. This is the part that needs one: that every model in the migration
// set actually becomes a table.
func TestTheWholeSchemaMigratesOnSQLServer(t *testing.T) {
	db := testutils.SetupSQLServerDB(t, 4)

	for _, model := range models.MigrationModels() {
		if !db.Migrator().HasTable(model) {
			t.Errorf("%T is in the migration set but no table was created for it", model)
		}
	}
}

// An identifier column has to hold the canonical hyphenated form, because that
// is what Value writes. uniqueidentifier would not: it stores the first three
// groups little-endian, so the bytes read back are not the bytes written unless
// every reader agrees on the swap.
func TestIdentifierColumnsHoldTheFormValueWrites(t *testing.T) {
	db := testutils.SetupSQLServerDB(t, 2)

	for _, column := range []struct{ table, name string }{
		{"process_instances", "id"},
		{"process_instances", "project_id"},
		{"tasks", "id"},
		{"jobs", "id"},
		{"external_tasks", "project_id"},
		{"event_subscriptions", "instance_id"},
	} {
		var dataType string
		err := db.Raw(`SELECT DATA_TYPE FROM INFORMATION_SCHEMA.COLUMNS
		               WHERE TABLE_NAME = ? AND COLUMN_NAME = ?`, column.table, column.name).Scan(&dataType).Error
		if err != nil {
			t.Fatalf("reading %s.%s: %v", column.table, column.name, err)
		}
		if dataType != "char" {
			t.Errorf("%s.%s is %q, want char — uniqueidentifier reorders the bytes Value wrote",
				column.table, column.name, dataType)
		}
	}
}
