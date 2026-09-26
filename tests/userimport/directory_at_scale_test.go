package userimport_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/google/uuid"
	"github.com/gsoultan/metis/server/domains/entities"
	"github.com/gsoultan/metis/server/repositories/pg"
)

// peopleAhead is as many participants as one read of the generated store
// returns: storm starts every query with a limit of 1000.
const peopleAhead = 1000

// seedPeople adds n active participants to a project in one statement, and one
// more whose username sorts after all of them.
func seedPeople(t *testing.T, ctx context.Context, projectID uuid.UUID, n int, last string) {
	t.Helper()
	if _, err := connOf(t).Main().Exec(ctx, `
		INSERT INTO workflow_users (project_id, username, active)
		SELECT $1::uuid, format('person-%s', lpad(n::text, 5, '0')), true FROM generate_series(1, $2) AS n
		UNION ALL SELECT $1::uuid, $3::text, true`, projectID, n, last); err != nil {
		t.Fatalf("seed %d participants: %v", n+1, err)
	}
}

// A directory sync that deactivates whoever the source stops naming has to
// look at everybody in the directory.
//
// It read the directory through a query the store caps at a thousand rows, so
// in a directory of more than a thousand people the ones past the window were
// never compared with the source: somebody who had left kept receiving work.
func TestASyncDeactivatesEverybodyTheSourceStopsNaming(t *testing.T) {
	t.Setenv("METIS_HTTP_ALLOW_PRIVATE_NETWORKS", "true")
	source := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`[{"username":"ada"}]`))
	}))
	defer source.Close()

	sync, people, projectID := syncFixture(t)
	ctx := t.Context()
	seedPeople(t, ctx, projectID, peopleAhead, "zz-leaver")

	id, err := sync.SaveSource(ctx, entities.ParticipantSource{
		Project:   &entities.Project{ID: projectID},
		Name:      "HR",
		Kind:      "http",
		Config:    map[string]any{"url": source.URL},
		OnMissing: entities.OnMissingDeactivate,
		Enabled:   true,
	})
	if err != nil {
		t.Fatalf("save source: %v", err)
	}
	summary, err := sync.SyncSource(ctx, id)
	if err != nil {
		t.Fatalf("sync: %v", err)
	}
	if summary.Deactivated != peopleAhead+1 {
		t.Errorf("the sync deactivated %d of the %d people the source no longer names",
			summary.Deactivated, peopleAhead+1)
	}

	leaver, err := pg.NewWorkflowUserRepository(connOf(t)).GetByUsername(ctx, projectID, "zz-leaver")
	if err != nil {
		t.Fatalf("read the leaver back: %v", err)
	}
	if leaver.Active {
		t.Fatalf("somebody the source stopped naming is still active in a directory of %d; work still reaches them",
			peopleAhead+2)
	}
	if listed, err := people.ListWorkflowUsers(ctx, projectID, 0); err != nil || len(listed) != peopleAhead+2 {
		t.Fatalf("listing the whole directory returned %d of %d people (err %v)", len(listed), peopleAhead+2, err)
	}
}

// Removing a participant finds them wherever they sort in the directory.
//
// The removal checked that the person belongs to the project by looking for
// them in the directory — read through the same capped query — so somebody
// past the first thousand usernames could not be removed: "no such participant
// in this project".
func TestAParticipantPastTheFirstThousandCanBeRemoved(t *testing.T) {
	ctx, svc, projectID := fixture(t)
	seedPeople(t, ctx, projectID, peopleAhead, "zz-leaver")
	directory := pg.NewWorkflowUserRepository(connOf(t))
	leaver, err := directory.GetByUsername(ctx, projectID, "zz-leaver")
	if err != nil {
		t.Fatalf("read the leaver back: %v", err)
	}

	if err := svc.RemoveWorkflowUser(ctx, projectID, leaver.ID); err != nil {
		t.Fatalf("removing a participant who sorts after %d others was refused: %v", peopleAhead, err)
	}
	if _, err := directory.GetByUsername(ctx, projectID, "zz-leaver"); err == nil {
		t.Fatal("the participant is still in the directory after being removed")
	}
}
