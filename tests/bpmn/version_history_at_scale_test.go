package bpmn_test

import (
	"testing"
)

// versionsOfOneKey is more versions than one read of the generated store
// returns: storm starts every query with a limit of 1000.
const versionsOfOneKey = 1001

// The version history lists every version of a process, and the versions page
// marks the live version of every process.
//
// Both read through queries the store caps at a thousand rows, newest first.
// A process deployed on every change passes a thousand versions: its history
// lost the oldest, the first one ever deployed. And the live marks were read
// from the project's whole release timeline, so a process promoted once, long
// ago, fell behind a thousand newer promotions of another: the page no longer
// knew it had been promoted and showed its highest version as live, while new
// instances started on the promoted one.
func TestTheVersionHistoryAndTheLiveMarksCoverEveryVersion(t *testing.T) {
	h := newEngineHarness(t, "Busy Versions Project")
	ctx := h.Ctx()
	if err := h.db.WithContext(ctx).Exec(`
		INSERT INTO process_definitions (id, created_at, updated_at, project_id, key, name, version, nodes, flows)
		SELECT gen_random_uuid(), now(), now(), ?, 'expense-approval', 'Expense approval', n, '[]', '[]'
		  FROM generate_series(1, ?) AS n`, h.projID, versionsOfOneKey).Error; err != nil {
		t.Fatalf("seed %d versions: %v", versionsOfOneKey, err)
	}
	versions, err := h.svc.ListDefinitionVersions(ctx, h.projID, "expense-approval")
	if err != nil {
		t.Fatalf("version history: %v", err)
	}
	if len(versions) != versionsOfOneKey {
		t.Errorf("the version history lists %d of the process's %d versions", len(versions), versionsOfOneKey)
	}

	// A quiet process, promoted back to its first version a year ago, and a
	// thousand promotions of the busy one since.
	if err := h.db.WithContext(ctx).Exec(`
		INSERT INTO process_definitions (id, created_at, updated_at, project_id, key, name, version, nodes, flows)
		SELECT gen_random_uuid(), now(), now(), ?, 'quiet', 'Quiet', n, '[]', '[]' FROM generate_series(1, 2) AS n`,
		h.projID).Error; err != nil {
		t.Fatalf("seed the quiet process: %v", err)
	}
	if err := h.db.WithContext(ctx).Exec(`
		INSERT INTO process_definition_releases (id, created_at, updated_at, project_id, process_key, version, activate_at)
		VALUES (gen_random_uuid(), now(), now(), ?, 'quiet', 1, now() - interval '1 year')`, h.projID).Error; err != nil {
		t.Fatalf("promote the quiet process: %v", err)
	}
	if err := h.db.WithContext(ctx).Exec(`
		INSERT INTO process_definition_releases (id, created_at, updated_at, project_id, process_key, version, activate_at)
		SELECT gen_random_uuid(), now(), now(), ?, 'expense-approval', n, now() - n * interval '1 minute'
		  FROM generate_series(1, ?) AS n`, h.projID, versionsOfOneKey-1).Error; err != nil {
		t.Fatalf("seed the busy process's promotions: %v", err)
	}
	live, err := h.svc.ListLiveVersions(ctx, h.projID)
	if err != nil {
		t.Fatalf("live versions: %v", err)
	}
	if got, marked := live["quiet"]; got != 1 {
		t.Fatalf("the versions page has the quiet process's live version as %d (marked: %v), so it shows the highest as live; new instances start on version 1",
			got, marked)
	}
}
