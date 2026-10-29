package bpmn_test

import (
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/gsoultan/metis/server/repositories/store/processdefinitionrelease"
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
	h.seedDefinitions(t, "expense-approval", "Expense approval", versionsOfOneKey, time.Now())
	versions, err := h.svc.ListDefinitionVersions(ctx, h.projID, "expense-approval")
	if err != nil {
		t.Fatalf("version history: %v", err)
	}
	if len(versions) != versionsOfOneKey {
		t.Errorf("the version history lists %d of the process's %d versions", len(versions), versionsOfOneKey)
	}

	// A quiet process, promoted back to its first version a year ago, and a
	// thousand promotions of the busy one since.
	h.seedDefinitions(t, "quiet", "Quiet", 2, time.Now())
	now := time.Now()
	promotions := []processdefinitionrelease.Row{{
		ID: uuid.New(), CreatedAt: now, UpdatedAt: now, ProjectID: h.projID,
		ProcessKey: "quiet", Version: 1, ActivateAt: now.AddDate(-1, 0, 0),
	}}
	for n := 1; n < versionsOfOneKey; n++ {
		promotions = append(promotions, processdefinitionrelease.Row{
			ID: uuid.New(), CreatedAt: now, UpdatedAt: now, ProjectID: h.projID,
			ProcessKey: "expense-approval", Version: int64(n), ActivateAt: now.Add(-time.Duration(n) * time.Minute),
		})
	}
	if _, err := processdefinitionrelease.InsertAll(ctx, h.ex(), promotions); err != nil {
		t.Fatalf("promote the quiet process and the busy one: %v", err)
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
