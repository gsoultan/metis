package models

import (
	"testing"

	"github.com/glebarez/sqlite"
	"gorm.io/driver/mysql"
	"gorm.io/driver/postgres"
	"gorm.io/driver/sqlserver"
	"gorm.io/gorm"
)

// The column type each supported dialect gets for an identifier.
//
// This is the decision every table's primary key depends on, and getting it
// wrong is not subtle: AutoMigrate fails on the first table and the server
// cannot start at all. MySQL was in exactly that state — offered in the config
// and the setup wizard, and unable to create a single table — because every
// test ran on SQLite.
//
// SQL Server was in the same state and could not be found the same way: there
// is no arm64 image, so tests/mssql cannot run on a developer machine. Asking
// the type directly needs only the dialector's name, so the decision is checked
// here even where the server cannot be started.
func TestEachDialectGetsAColumnTypeItHas(t *testing.T) {
	for _, want := range []struct {
		dialect gorm.Dialector
		column  string
		why     string
	}{
		{postgres.New(postgres.Config{}), "uuid", "PostgreSQL has a native uuid type, and changing this would ALTER TABLE every existing deployment on its next boot"},
		{sqlite.Open(":memory:"), "uuid", "SQLite accepts any type name, and existing databases were created with this one"},
		{mysql.New(mysql.Config{}), "char(36)", "MySQL has no uuid type"},
		{sqlserver.New(sqlserver.Config{}), "char(36)", "SQL Server has uniqueidentifier, not uuid — and uniqueidentifier stores the first three groups little-endian, which Value/Scan do not do"},
	} {
		db := &gorm.DB{Config: &gorm.Config{Dialector: want.dialect}}
		if got := NilUUID.GormDBDataType(db, nil); got != want.column {
			t.Errorf("%s gets %q, want %q — %s", want.dialect.Name(), got, want.column, want.why)
		}
	}
}

// With no database to ask, there is still a type to name.
func TestTheColumnTypeSurvivesHavingNoDialector(t *testing.T) {
	if got := NilUUID.GormDBDataType(nil, nil); got == "" {
		t.Error("a nil db gave no column type at all")
	}
	if got := NilUUID.GormDBDataType(&gorm.DB{Config: &gorm.Config{}}, nil); got == "" {
		t.Error("a db with no dialector gave no column type at all")
	}
}
