package postgres_test

import (
	"testing"

	"github.com/gsoultan/metis/tests/testutils"
)

// The dialect-aware UUID type must not change PostgreSQL's schema.
//
// Identifier columns used to be pinned with `gorm:"type:uuid"`, which MySQL has
// no equivalent for. Moving that decision into models.UUID lets MySQL get
// char(36) — but only if PostgreSQL still gets exactly `uuid`. If it did not,
// every existing deployment would meet an ALTER TABLE on its next boot.
//
// This asserts the column types an existing database already has.
func TestUUIDColumnsStayNativeOnPostgres(t *testing.T) {
	db := testutils.SetupPostgresDB(t, 2)

	cases := []struct {
		table  string
		column string
	}{
		{"process_instances", "id"},
		{"process_instances", "project_id"},
		{"process_instances", "definition_id"},
		{"process_instances", "parent_instance_id"},
		{"event_subscriptions", "id"},
		{"event_subscriptions", "project_id"},
		{"event_subscriptions", "instance_id"},
		{"tasks", "id"},
		{"jobs", "id"},
		{"projects", "organization_id"},
	}

	for _, tc := range cases {
		t.Run(tc.table+"."+tc.column, func(t *testing.T) {
			var dataType string
			err := db.Raw(`
				SELECT data_type
				FROM information_schema.columns
				WHERE table_schema = current_schema()
				  AND table_name = ?
				  AND column_name = ?`, tc.table, tc.column).Scan(&dataType).Error
			if err != nil {
				t.Fatalf("read column type: %v", err)
			}
			if dataType == "" {
				t.Fatalf("column %s.%s does not exist", tc.table, tc.column)
			}
			if dataType != "uuid" {
				t.Errorf("%s.%s is %q, expected \"uuid\" — existing deployments would be altered on upgrade",
					tc.table, tc.column, dataType)
			}
		})
	}
}

// The sized identifier columns are the one deliberate schema change: MySQL
// cannot index an unbounded TEXT column, so those became varchar. This records
// which columns are affected, because an existing PostgreSQL database will be
// altered from text to varchar on upgrade and a value longer than the size would
// fail that migration.
func TestSizedColumnsAreRecordedForMigration(t *testing.T) {
	db := testutils.SetupPostgresDB(t, 2)

	cases := []struct {
		table   string
		column  string
		maxSize int
	}{
		{"event_subscriptions", "event_name", 255},
		{"event_subscriptions", "correlation_key", 512},
		{"process_definitions", "key", 255},
		{"projects", "name", 255},
		{"users", "username", 255},
	}

	for _, tc := range cases {
		t.Run(tc.table+"."+tc.column, func(t *testing.T) {
			var length int
			err := db.Raw(`
				SELECT COALESCE(character_maximum_length, 0)
				FROM information_schema.columns
				WHERE table_schema = current_schema()
				  AND table_name = ?
				  AND column_name = ?`, tc.table, tc.column).Scan(&length).Error
			if err != nil {
				t.Fatalf("read column length: %v", err)
			}
			if length != tc.maxSize {
				t.Errorf("%s.%s has length %d, expected %d — the upgrade note for this column is wrong",
					tc.table, tc.column, length, tc.maxSize)
			}
		})
	}
}
