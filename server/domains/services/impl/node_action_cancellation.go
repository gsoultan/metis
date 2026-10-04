package impl

import "github.com/gsoultan/metis/server/repositories/models"

// cancellation is what cancelling an instance did: the row as it was written,
// and what was taken with it.
type cancellation struct {
	// instance is the row as the cancel wrote it.
	instance models.ProcessInstanceModel
	// withdrawn is the tasks it took out of people's inboxes, as they were
	// before, in listing order (newest first).
	withdrawn []models.TaskModel
	// parkedWithdrawn is how many pieces of work parked for outside workers it
	// took off the list they fetch from.
	parkedWithdrawn int
}
