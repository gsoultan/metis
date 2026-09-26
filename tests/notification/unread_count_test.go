package notification_test

import (
	"context"
	"testing"
)

// The bell's number is every unread notification a person has.
//
// It was counted in the browser, among what the list endpoint had sent: the
// newest thousand. Somebody with more than that was told of none of the unread
// ones older than those — here, all twenty. And the list was cut at a thousand
// before the organization filter was applied, so the same person's inbox in a
// second organization could come back empty however much was waiting in it.
func TestTheBellCountsEveryUnreadNotification(t *testing.T) {
	w := newWorld(t)
	w.seedInbox(t, "alice", w.projectID, inboxSize, oldestUnread)

	// Neither somebody else's unread notifications nor alice's in another
	// organization are hers to count here.
	w.seedInbox(t, "bob", w.projectID, 5, 5)
	elsewhere, elsewhereProject := w.elsewhere(t)
	w.seedInbox(t, "alice", elsewhereProject, 7, 7)

	assertUnread(t, w, w.ctx, "alice", oldestUnread)
	assertUnread(t, w, w.ctx, "bob", 5)
	assertUnread(t, w, elsewhere, "alice", 7)
}

// Reading one takes it off the count, and marking them all read empties it —
// in this organization, and not in the other one.
func TestReadingNotificationsTakesThemOffTheCount(t *testing.T) {
	w := newWorld(t)
	w.seedInbox(t, "alice", w.projectID, inboxSize, oldestUnread)
	elsewhere, elsewhereProject := w.elsewhere(t)
	w.seedInbox(t, "alice", elsewhereProject, 7, 7)

	if err := w.svc.MarkAsRead(w.ctx, w.oneUnread(t, "alice", w.projectID)); err != nil {
		t.Fatalf("mark one read: %v", err)
	}
	assertUnread(t, w, w.ctx, "alice", oldestUnread-1)

	if err := w.svc.MarkAllAsRead(w.ctx, "alice"); err != nil {
		t.Fatalf("mark all read: %v", err)
	}
	assertUnread(t, w, w.ctx, "alice", 0)
	assertUnread(t, w, elsewhere, "alice", 7)
}

func assertUnread(t *testing.T, w world, ctx context.Context, userID string, want int64) {
	t.Helper()
	got, err := w.svc.CountUnreadByUser(ctx, userID)
	if err != nil {
		t.Fatalf("count %s's unread notifications: %v", userID, err)
	}
	if got != want {
		t.Errorf("%s has %d unread notifications here, and the count says %d", userID, want, got)
	}
}
