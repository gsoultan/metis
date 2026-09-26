// Package notification_test holds a person's notification centre to what they
// actually have: every notification counted, every one reachable by paging,
// however many there are.
package notification_test

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/gsoultan/metis/server/domains/services/contracts"
	"github.com/gsoultan/metis/server/domains/services/impl"
	"github.com/gsoultan/metis/server/repositories"
	"github.com/gsoultan/metis/tests/testutils"
	"gorm.io/gorm"
)

// The inbox the bell reads, seeded in one statement.
//
// inboxSize is past the store's default thousand rows, which is what the list
// endpoint returned and the bell counted among. Rows are written in groups of
// sameMoment that share a created_at, the way one transaction's notifications
// do. The oldestUnread oldest are unread and the rest read, so the newest
// thousand — whole groups, ending on a group boundary — hold no unread one at
// all.
const (
	inboxSize    = 1050
	sameMoment   = 50
	oldestUnread = 20
)

// world is one organization with a project, acted in by ctx, and the
// notification service over it.
type world struct {
	db        *gorm.DB
	repo      repositories.Repository
	svc       contracts.NotificationService
	ctx       context.Context
	projectID uuid.UUID
}

func newWorld(t *testing.T) world {
	t.Helper()
	db := testutils.SetupTestDB(t)
	repo := repositories.NewRepository(testutils.StormConn(db))
	ctx, _, projectID := testutils.ScopedProject(t, repo)
	return world{db: db, repo: repo, svc: impl.NewNotificationService(repo.Notification()), ctx: ctx, projectID: projectID}
}

// elsewhere is another organization with a project of its own, and a context
// acting in it.
func (w world) elsewhere(t *testing.T) (context.Context, uuid.UUID) {
	t.Helper()
	ctx, _, projectID := testutils.ScopedProject(t, w.repo)
	return ctx, projectID
}

func (w world) seedInbox(t *testing.T, userID string, projectID uuid.UUID, count, unread int) {
	t.Helper()
	seedInbox(t, w.db, userID, projectID, count, unread)
}

// seedInbox addresses count notifications to one person in one project.
//
// Numbered from the oldest: row n shares its created_at with the rest of its
// group of sameMoment, and rows 1 to unread are unread. Ids are random, so the
// order among rows that share a created_at is up to the database unless a
// query settles it.
func seedInbox(t *testing.T, db *gorm.DB, userID string, projectID uuid.UUID, count, unread int) {
	t.Helper()
	if err := db.WithContext(t.Context()).Exec(`
		INSERT INTO notifications (id, created_at, updated_at, user_id, type, title, message, is_read, project_id)
		SELECT gen_random_uuid(), at, at, ?, 'TaskAssignment', 'Approve request ' || n,
		       'Request ' || n || ' is waiting for you', n > ?, ?
		  FROM generate_series(1, ?) AS n,
		       LATERAL (SELECT timestamptz '2026-01-01 00:00:00+00' + ((n - 1) / ?) * interval '1 minute' AS at) AS moment`,
		userID, unread, projectID, count, sameMoment).Error; err != nil {
		t.Fatalf("seed %d notifications for %s: %v", count, userID, err)
	}
}

// oneUnread returns the id of one of a person's unread notifications in a
// project.
func (w world) oneUnread(t *testing.T, userID string, projectID uuid.UUID) uuid.UUID {
	t.Helper()
	var id string
	if err := w.db.WithContext(t.Context()).Raw(`
		SELECT id::text FROM notifications
		 WHERE user_id = ? AND project_id = ? AND is_read = false
		 ORDER BY created_at, id LIMIT 1`,
		userID, projectID).Scan(&id).Error; err != nil {
		t.Fatalf("find an unread notification for %s: %v", userID, err)
	}
	parsed, err := uuid.Parse(id)
	if err != nil {
		t.Fatalf("%s has no unread notification: %v", userID, err)
	}
	return parsed
}
