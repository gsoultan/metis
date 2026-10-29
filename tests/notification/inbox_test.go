// Package notification_test holds a person's notification centre to what they
// actually have: every notification counted, every one reachable by paging,
// however many there are.
package notification_test

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/gsoultan/metis/server/repositories/db"
	"github.com/gsoultan/metis/server/repositories/store/notification"
	"github.com/gsoultan/storm/runtime"

	"github.com/google/uuid"
	"github.com/gsoultan/metis/server/domains/services/contracts"
	"github.com/gsoultan/metis/server/domains/services/impl"
	"github.com/gsoultan/metis/server/repositories"
	"github.com/gsoultan/metis/tests/testutils"
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
	db        *db.Conn
	repo      repositories.Repository
	svc       contracts.NotificationService
	ctx       context.Context
	projectID uuid.UUID
}

func newWorld(t *testing.T) world {
	t.Helper()
	db := testutils.SetupTestDB(t)
	repo := repositories.NewRepository(db)
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
func seedInbox(t *testing.T, db *db.Conn, userID string, projectID uuid.UUID, count, unread int) {
	t.Helper()
	first := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	rows := make([]notification.Row, count)
	for i := range rows {
		n := i + 1
		at := first.Add(time.Duration((n-1)/sameMoment) * time.Minute)
		rows[i] = notification.Row{
			ID: uuid.New(), CreatedAt: at, UpdatedAt: at, UserID: userID, Type: "TaskAssignment",
			Title: fmt.Sprintf("Approve request %d", n), Message: fmt.Sprintf("Request %d is waiting for you", n),
			IsRead: n > unread, ProjectID: runtime.Null[[16]byte]{V: projectID, Valid: true},
		}
	}
	if _, err := notification.InsertAll(t.Context(), testutils.Executor(db), rows); err != nil {
		t.Fatalf("seed %d notifications for %s: %v", count, userID, err)
	}
}

// oneUnread returns the id of one of a person's unread notifications in a
// project.
func (w world) oneUnread(t *testing.T, userID string, projectID uuid.UUID) uuid.UUID {
	t.Helper()
	row, found, err := notification.New().UserIDEq(userID).ProjectIDEq(projectID).IsReadEq(false).
		Order(notification.CreatedAt.Asc(), notification.ID.Asc()).One(t.Context(), testutils.Executor(w.db))
	if err != nil || !found {
		t.Fatalf("%s has no unread notification: found %v, %v", userID, found, err)
	}
	return row.ID
}
