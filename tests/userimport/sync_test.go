package userimport_test

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/google/uuid"
	"github.com/gsoultan/metis/server/domains/entities"
	"github.com/gsoultan/metis/server/domains/services/contracts"
	"github.com/gsoultan/metis/server/domains/services/impl"
	"github.com/gsoultan/metis/server/repositories/pg"
)

// syncFixture wires the sync service over the same real database the import
// tests use.
func syncFixture(t *testing.T) (contracts.ParticipantSyncService, contracts.WorkflowUserService, uuid.UUID) {
	t.Helper()
	_, _, projectID := fixture(t)
	conn := connOf(t)

	// Built here rather than unwrapped from the import fixture's service: that
	// one is wrapped for cross-package use, and unwrapping a wrapper is how the
	// syncer first got handed a nil write path.
	directory := pg.NewWorkflowUserRepository(conn)
	people := impl.NewWorkflowUserService(directory)
	sources := pg.NewParticipantSourceRepository(conn)
	sync := impl.NewParticipantSyncService(sources, impl.WorkflowUserServiceFor(people), directory, impl.NewNoOpLocker())
	return sync, people, projectID
}

// A source is saved, run on demand, and its outcome recorded.
func TestSyncingFromASavedSource(t *testing.T) {
	t.Setenv("METIS_HTTP_ALLOW_PRIVATE_NETWORKS", "true")
	directory := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`[{"username":"ada","email":"ada@example.com","active":true},
		                        {"username":"bob","email":"bob@example.com","active":true}]`))
	}))
	defer directory.Close()

	sync, people, projectID := syncFixture(t)

	id, err := sync.SaveSource(t.Context(), entities.ParticipantSource{
		Project: &entities.Project{ID: projectID},
		Name:    "HR",
		Kind:    "http",
		Config:  map[string]any{"url": directory.URL},
		Enabled: true,
	})
	if err != nil {
		t.Fatalf("save source: %v", err)
	}

	summary, err := sync.SyncSource(t.Context(), id)
	if err != nil {
		t.Fatalf("sync: %v", err)
	}
	if summary.Created != 2 {
		t.Fatalf("two participants should arrive, got %+v", summary)
	}

	// The outcome is on the source, so a directory that has been failing is
	// visible without reading logs.
	sources, err := sync.ListSources(t.Context(), projectID)
	if err != nil {
		t.Fatalf("list sources: %v", err)
	}
	if len(sources) != 1 || sources[0].LastRun == nil {
		t.Fatalf("the run should be recorded, got %+v", sources)
	}
	if !sources[0].LastRun.OK || sources[0].LastRun.Created != 2 {
		t.Fatalf("the recorded run should say what happened, got %+v", sources[0].LastRun)
	}

	listed, err := people.ListWorkflowUsers(t.Context(), projectID, 0)
	if err != nil {
		t.Fatalf("list participants: %v", err)
	}
	if len(listed) != 2 {
		t.Fatalf("expected two participants, got %d", len(listed))
	}
}

// What a sync does about somebody it has stopped naming is the source's choice,
// and the default is to leave them.
//
// A feed covering one department is not a statement about everybody else:
// deactivating on absence by default would mean syncing finance took work away
// from engineering.
func TestWhatHappensToSomebodyTheSourceStopsNaming(t *testing.T) {
	t.Setenv("METIS_HTTP_ALLOW_PRIVATE_NETWORKS", "true")

	for _, tc := range []struct {
		name          string
		onMissing     string
		wantActive    bool
		wantDeactived int
	}{
		{"left alone by default", "", true, 0},
		{"deactivated when the source says so", entities.OnMissingDeactivate, false, 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			// The directory drops bob on the second call.
			var call int
			directory := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				call++
				if call == 1 {
					_, _ = w.Write([]byte(`[{"username":"ada"},{"username":"bob"}]`))
					return
				}
				_, _ = w.Write([]byte(`[{"username":"ada"}]`))
			}))
			defer directory.Close()

			sync, people, projectID := syncFixture(t)
			id, err := sync.SaveSource(t.Context(), entities.ParticipantSource{
				Project:   &entities.Project{ID: projectID},
				Name:      "HR",
				Kind:      "http",
				Config:    map[string]any{"url": directory.URL},
				OnMissing: tc.onMissing,
				Enabled:   true,
			})
			if err != nil {
				t.Fatalf("save source: %v", err)
			}

			if _, err := sync.SyncSource(t.Context(), id); err != nil {
				t.Fatalf("first sync: %v", err)
			}
			summary, err := sync.SyncSource(t.Context(), id)
			if err != nil {
				t.Fatalf("second sync: %v", err)
			}
			if summary.Deactivated != tc.wantDeactived {
				t.Errorf("expected %d deactivated, got %d", tc.wantDeactived, summary.Deactivated)
			}

			listed, err := people.ListWorkflowUsers(t.Context(), projectID, 0)
			if err != nil {
				t.Fatalf("list: %v", err)
			}
			// Deactivated, never deleted: the record of what somebody did has
			// to survive them leaving.
			if len(listed) != 2 {
				t.Fatalf("both people should still exist, got %d", len(listed))
			}
			for _, person := range listed {
				if person.Username == "bob" && person.Active != tc.wantActive {
					t.Errorf("bob active=%v, wanted %v", person.Active, tc.wantActive)
				}
			}
		})
	}
}

// A source that could never be read is refused when it is saved, not at the
// first run — where nobody is watching.
func TestAnUnrunnableSourceIsRefusedOnSave(t *testing.T) {
	sync, _, projectID := syncFixture(t)

	for _, tc := range []struct {
		name   string
		source entities.ParticipantSource
	}{
		{"no name", entities.ParticipantSource{Project: &entities.Project{ID: projectID}, Kind: "http"}},
		{"unknown kind", entities.ParticipantSource{Project: &entities.Project{ID: projectID}, Name: "x", Kind: "carrier-pigeon"}},
		{"unreadable schedule", entities.ParticipantSource{Project: &entities.Project{ID: projectID}, Name: "x", Kind: "http", Schedule: "hourly-ish"}},
		{"unknown missing rule", entities.ParticipantSource{Project: &entities.Project{ID: projectID}, Name: "x", Kind: "http", OnMissing: "delete-everything"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := sync.SaveSource(t.Context(), tc.source); err == nil {
				t.Fatalf("%s should be refused", tc.name)
			}
		})
	}
}
