package postgres_test

import (
	"testing"

	"github.com/gsoultan/metis/tests/testutils"
)

// Identifier columns are PostgreSQL's own uuid, not text: a database created
// from the model and one converted from an earlier release both have them, and
// a change here would be an ALTER TABLE on every existing deployment.
//
// This asserts the column types an existing database already has.
func TestUUIDColumnsStayNativeOnPostgres(t *testing.T) {
	held := testutils.Held(t, testutils.SetupPostgresDB(t, 2))

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
			column := held.Table(tc.table).Column(tc.column)
			if column == nil {
				t.Fatalf("column %s.%s does not exist", tc.table, tc.column)
			}
			dataType := column.Type.Name
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
	held := testutils.Held(t, testutils.SetupPostgresDB(t, 2))

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
			column := held.Table(tc.table).Column(tc.column)
			if column == nil {
				t.Fatalf("column %s.%s does not exist", tc.table, tc.column)
			}
			length := column.Type.Size
			if length != tc.maxSize {
				t.Errorf("%s.%s has length %d, expected %d — the upgrade note for this column is wrong",
					tc.table, tc.column, length, tc.maxSize)
			}
		})
	}
}
