package migrations_test

import (
	"fmt"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/gsoultan/metis/server/repositories/migrations"
	"github.com/gsoultan/metis/server/repositories/models"
	"github.com/gsoultan/metis/tests/testutils"
	"gorm.io/gorm"
)

// The indexes the bell reads through, and the columns each one must lead with.
var notificationIndexes = map[string]string{
	"ix_notifications_user_unread": "(user_id, is_read, project_id)",
	"ix_notifications_user_newest": "(user_id, created_at DESC, id DESC)",
}

// unreadCount is the statement the store sends for a person's unread count,
// with is_read bound as a parameter the way the store binds it.
const unreadCount = `SELECT count(*) FROM "notifications"
	WHERE "deleted_at" IS NULL AND ("user_id" = $1 AND ("project_id" = ANY($2) OR "project_id" IS NULL) AND "is_read" = $3)`

// newestPage is the statement the store sends for one page of a person's
// notifications.
const newestPage = `SELECT "id" FROM "notifications"
	WHERE "deleted_at" IS NULL AND ("user_id" = $1 AND ("project_id" = ANY($2) OR "project_id" IS NULL))
	ORDER BY "created_at" DESC, "id" DESC LIMIT $3 OFFSET $4`

func migrateAll(t *testing.T, db *gorm.DB) {
	t.Helper()
	if _, err := migrations.Run(t.Context(), db, migrations.Schema(models.MigrationModels())); err != nil {
		t.Fatalf("run the migrations: %v", err)
	}
}

// Migration 29 is only reachable on an installation that already has the
// table, with notifications in it. This takes the indexes away and forgets the
// migration ran, the way an upgrading installation arrives, and runs it again.
func TestMigration29IndexesANotificationTableThatAlreadyHasRows(t *testing.T) {
	db := testutils.SetupTestDB(t)
	migrateAll(t, db)
	assertNotificationIndexes(t, db)

	for name := range notificationIndexes {
		if err := db.WithContext(t.Context()).Exec(`DROP INDEX IF EXISTS ` + name).Error; err != nil {
			t.Fatalf("drop %s: %v", name, err)
		}
	}
	if err := db.WithContext(t.Context()).Exec(
		`DELETE FROM schema_migrations WHERE version = ?`, migrations.NotificationIndexesMigration).Error; err != nil {
		t.Fatalf("forget the migration ran: %v", err)
	}
	seedNotifications(t, db, "alice", 30, 5)

	migrateAll(t, db)
	migrateAll(t, db) // and a second run finds nothing left to do

	assertNotificationIndexes(t, db)
	var kept int64
	if err := db.WithContext(t.Context()).Raw(
		`SELECT count(*) FROM notifications WHERE user_id = 'alice'`).Scan(&kept).Error; err != nil {
		t.Fatalf("count the notifications: %v", err)
	}
	if kept != 30 {
		t.Fatalf("%d of the 30 notifications written before the migration are left", kept)
	}
}

// The bell's statements are answered from the indexes, and still are once the
// statement is prepared and planned for any parameters at all.
//
// A prepared statement that has run a few times may be given a generic plan,
// which cannot see the value bound to is_read. An index built only over the
// unread rows would then be no use to the count it was built for, silently, on
// exactly the busy connections that run the count most. So is_read is a key
// column rather than the index's condition. The condition that is there, that
// the row is not deleted, is written into every statement rather than bound,
// so a generic plan can still prove it.
func TestTheBellIsAnsweredFromTheIndexesEvenWithAGenericPlan(t *testing.T) {
	db := testutils.SetupTestDB(t)
	migrateAll(t, db)
	seedNotifications(t, db, "alice", 400, 20)
	seedNotifications(t, db, "bob", 400, 20)
	if err := db.WithContext(t.Context()).Exec(`ANALYZE notifications`).Error; err != nil {
		t.Fatalf("analyze: %v", err)
	}

	plans := explainGeneric(t, db, map[string]string{
		"unread": unreadCount,
		"page":   newestPage,
	}, map[string]string{
		"unread": `'alice', '{}'::uuid[], false`,
		"page":   `'alice', '{}'::uuid[], 20, 40`,
	})
	if !strings.Contains(plans["unread"], "ix_notifications_user_unread") {
		t.Errorf("the unread count is not answered from ix_notifications_user_unread:\n%s", plans["unread"])
	}
	if !strings.Contains(plans["page"], "ix_notifications_user_newest") || strings.Contains(plans["page"], "Sort") {
		t.Errorf("a page is not read in order from ix_notifications_user_newest:\n%s", plans["page"])
	}
}

// explainGeneric prepares each statement on one connection, forced to a
// generic plan and away from scanning the whole table, and returns the plan
// of executing it with the given arguments.
func explainGeneric(t *testing.T, db *gorm.DB, statements, arguments map[string]string) map[string]string {
	t.Helper()
	plans := map[string]string{}
	err := db.WithContext(t.Context()).Connection(func(conn *gorm.DB) error {
		for _, setting := range []string{`SET plan_cache_mode = force_generic_plan`, `SET enable_seqscan = off`} {
			if err := conn.Exec(setting).Error; err != nil {
				return fmt.Errorf("%s: %w", setting, err)
			}
		}
		for name, statement := range statements {
			if err := conn.Exec(`PREPARE ` + name + ` AS ` + statement).Error; err != nil {
				return fmt.Errorf("prepare %s: %w", name, err)
			}
			var lines []string
			if err := conn.Raw(`EXPLAIN EXECUTE ` + name + `(` + arguments[name] + `)`).Scan(&lines).Error; err != nil {
				return fmt.Errorf("explain %s: %w", name, err)
			}
			if err := conn.Exec(`DEALLOCATE ` + name).Error; err != nil {
				return fmt.Errorf("deallocate %s: %w", name, err)
			}
			plans[name] = strings.Join(lines, "\n")
		}
		return nil
	})
	if err != nil {
		t.Fatalf("explain the bell's statements: %v", err)
	}
	return plans
}

// assertNotificationIndexes checks each index is there, usable, over the
// columns it is for, and over the notifications that are not deleted.
func assertNotificationIndexes(t *testing.T, db *gorm.DB) {
	t.Helper()
	for name, columns := range notificationIndexes {
		var index struct {
			Definition string
			Valid      bool
		}
		if err := db.WithContext(t.Context()).Raw(`
			SELECT pg_get_indexdef(i.indexrelid) AS definition, i.indisvalid AS valid
			  FROM pg_class c
			  JOIN pg_index i ON i.indexrelid = c.oid
			 WHERE c.relname = ?
			   AND c.relnamespace = current_schema()::regnamespace`, name).Scan(&index).Error; err != nil {
			t.Fatalf("read %s: %v", name, err)
		}
		if !index.Valid {
			t.Errorf("%s is missing or INVALID: %q", name, index.Definition)
			continue
		}
		if !strings.Contains(index.Definition, "notifications USING btree "+columns) {
			t.Errorf("%s is not over notifications %s: %s", name, columns, index.Definition)
		}
		if !strings.Contains(index.Definition, "WHERE (deleted_at IS NULL)") {
			t.Errorf("%s holds deleted notifications too: %s", name, index.Definition)
		}
	}
}

// seedNotifications addresses count notifications to one person, the oldest
// unread of them unread, in one statement.
func seedNotifications(t *testing.T, db *gorm.DB, userID string, count, unread int) {
	t.Helper()
	if err := db.WithContext(t.Context()).Exec(`
		INSERT INTO notifications (id, created_at, updated_at, user_id, type, title, message, is_read, project_id)
		SELECT gen_random_uuid(), now() - n * interval '1 second', now(), ?, 'TaskAssignment', 'Approve ' || n,
		       'Request ' || n, n <= ?, ?
		  FROM generate_series(1, ?) AS n`,
		userID, count-unread, uuid.New(), count).Error; err != nil {
		t.Fatalf("seed %d notifications for %s: %v", count, userID, err)
	}
}
