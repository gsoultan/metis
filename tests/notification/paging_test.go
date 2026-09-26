package notification_test

import (
	"testing"

	"github.com/google/uuid"
	"github.com/gsoultan/metis/server/repositories/contracts"
)

// pageSize is what the bell asks for: a screenful, not a history.
const pageSize = 20

// Paging walks every one of a person's notifications exactly once, newest
// first, and reaches the oldest.
//
// The bell was sent the newest thousand in one response, so fifty of these
// were never shown, and the twenty unread ones among them least of all. Paged,
// the order has to be total: one transaction's notifications share a
// created_at, and among ties LIMIT and OFFSET may return a different order for
// each page, showing some rows twice and others never.
func TestPagingWalksEveryNotificationExactlyOnce(t *testing.T) {
	w := newWorld(t)
	w.seedInbox(t, "alice", w.projectID, inboxSize, oldestUnread)
	// Neither somebody else's notifications nor alice's in another
	// organization belong in this walk.
	w.seedInbox(t, "bob", w.projectID, 30, 0)
	_, elsewhereProject := w.elsewhere(t)
	w.seedInbox(t, "alice", elsewhereProject, 30, 0)

	seen := map[uuid.UUID]int{}
	unread := 0
	var totals []int64
	lastPage := (inboxSize + pageSize - 1) / pageSize
	for page := 1; page <= lastPage+1; page++ {
		got, err := w.svc.ListByUserPaged(w.ctx, "alice", contracts.Pagination{Page: page, PageSize: pageSize})
		if err != nil {
			t.Fatalf("page %d: %v", page, err)
		}
		totals = append(totals, got.Total)
		for i, n := range got.Items {
			seen[n.ID]++
			if !n.IsRead {
				unread++
			}
			if i > 0 && n.CreatedAt.After(got.Items[i-1].CreatedAt) {
				t.Fatalf("page %d is not newest first: %v comes after %v", page, n.CreatedAt, got.Items[i-1].CreatedAt)
			}
		}
		if !got.HasMore() {
			break
		}
	}

	repeated := 0
	for _, times := range seen {
		if times > 1 {
			repeated++
		}
	}
	if len(seen) != inboxSize || repeated > 0 {
		t.Errorf("walking every page showed %d of alice's %d notifications, %d of them more than once",
			len(seen), inboxSize, repeated)
	}
	if unread != oldestUnread {
		t.Errorf("walking every page reached %d of alice's %d unread notifications", unread, oldestUnread)
	}
	if len(totals) != lastPage {
		t.Errorf("paging took %d pages of %d; %d notifications make %d", len(totals), pageSize, inboxSize, lastPage)
	}
	for page, total := range totals {
		if total != inboxSize {
			t.Errorf("page %d says alice has %d notifications; she has %d", page+1, total, inboxSize)
			break
		}
	}
}
