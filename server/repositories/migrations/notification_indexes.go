package migrations

import (
	"context"

	"gorm.io/gorm"
)

// NotificationIndexesMigration is the version the notification centre's
// indexes are recorded under, named once so the test that rewinds it and the
// list that runs it agree on the row they mean.
const NotificationIndexesMigration = 29

// notificationIndexes is migration 29: an index for each of the two questions
// the notification centre asks about the signed-in person.
//
// The bell polls how many of their notifications are unread, every minute for
// everybody signed in, and opening it reads a page of them newest first. Over
// the single-column index on user_id each of those visits every notification
// the person has ever been sent — the count to look at is_read, the page to
// sort — so both slow down for the people who have the most.
//
//   - (user_id, is_read, project_id) counts: the unread ones are one range of
//     it, and the project the organization filter reads is in the index too,
//     so the count need not visit the table at all.
//   - (user_id, created_at DESC, id DESC) is a page: already in the order the
//     list asks for, so a page is a walk of its offset and its length instead
//     of a sort of everything.
//
// Both hold only the notifications that are not deleted, which every read of
// this table asks for with deleted_at IS NULL written into the statement. That
// is also why is_read is a column of the first rather than its condition: the
// store sends is_read as a parameter, and once a prepared statement is given a
// generic plan nothing can prove that parameter false, so an index over the
// unread rows alone would go unused by the very statement it is for, and say
// nothing.
//
// Built CONCURRENTLY, as 20: a notification is stored in the transaction that
// assigns the task it is about, so a plain build's lock on the table would hold
// up assigning work for as long as the build took.
func notificationIndexes() Migration {
	return Migration{
		Version: NotificationIndexesMigration,
		Name:    "count and page a person's notifications from an index",
		Run: func(ctx context.Context, db *gorm.DB) error {
			indexes := []struct{ name, columns string }{
				{"ix_notifications_user_unread", "user_id, is_read, project_id"},
				{"ix_notifications_user_newest", "user_id, created_at DESC, id DESC"},
			}
			for _, index := range indexes {
				if err := createPartialIndexConcurrently(ctx, db,
					"notifications", index.name, index.columns, "deleted_at IS NULL"); err != nil {
					return err
				}
			}
			return nil
		},
	}
}
