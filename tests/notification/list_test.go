package notification_test

import "testing"

// A person in two organizations sees, in each, that organization's
// notifications — however many they have in the other.
//
// The list read the person's newest thousand notifications across every
// organization and then dropped the ones the caller's organization may not
// see. Somebody with a busy inbox elsewhere had their thousand filled by it,
// and was shown nothing at all here.
func TestAPersonsListInOneOrganizationIsNotCrowdedOutByAnother(t *testing.T) {
	w := newWorld(t)
	const here = 5
	w.seedInbox(t, "alice", w.projectID, here, 0)
	// Newer than every one of hers here, and more than the list's thousand.
	_, elsewhereProject := w.elsewhere(t)
	w.seedInbox(t, "alice", elsewhereProject, inboxSize, 0)

	got, err := w.svc.ListByUser(w.ctx, "alice")
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(got) != here {
		t.Fatalf("alice's list in this organization holds %d notifications; want her %d here", len(got), here)
	}
	for _, n := range got {
		if n.Project == nil || n.Project.ID != w.projectID {
			t.Fatalf("notification %s belongs to another organization's project", n.ID)
		}
	}
}
