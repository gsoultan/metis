package strictscope

import (
	"fmt"
	"net/http"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/gsoultan/metis/server/repositories/gorms"
)

// The strict scope's failure mode is an empty result, not an error. A read path
// that lost its identity returns no rows, which is indistinguishable from
// having no data — so a suite that only checks status codes passes while the
// product is broken.
//
// This sweep therefore asserts two things about every tenant-scoped read:
// that it comes back with the data that was seeded, and that nothing was
// denied. The first catches a path that silently returned nothing; the second
// catches one that was denied but happened to have nothing to return anyway,
// which would otherwise wait to break until an installation had real data.
func TestStrictScope_EveryScopedReadStillReturnsItsTenantsData(t *testing.T) {
	h := newHarness(t)
	_, projectID := h.seedOrganization("Sweep Org", "sweeper", "correct-horse-battery")
	token := h.login("sweeper", "correct-horse-battery")

	// Seeded before the flag is forced on, the way real data predates it.
	seeded := h.seedOneOfEverything(token, projectID)

	underStrictScope(t)
	gorms.ResetDeniedSites()

	for _, read := range seeded.reads() {
		t.Run(read.name, func(t *testing.T) {
			var body map[string]any
			status := h.do(http.MethodGet, read.path, token, nil, &body)
			if status != http.StatusOK {
				t.Fatalf("GET %s returned %d under the strict scope", read.path, status)
			}
			items, ok := body[read.collection].([]any)
			if !ok {
				t.Fatalf("GET %s returned no %q array: %v", read.path, read.collection, body)
			}
			if len(items) == 0 {
				t.Fatalf(""+
					"GET %s returned an empty %s under the strict scope, but %s was seeded.\n"+
					"This is the flag's failure mode: the path did not error, it answered with nothing,\n"+
					"which on a real installation looks like a user whose data has vanished.",
					read.path, read.collection, read.name)
			}
		})
	}

	assertNothingWasDenied(t)
}

// The affirmative sweep above can only prove the paths it seeds data for. This
// one covers the rest of the read surface: every remaining GET is exercised and
// held to the weaker but still meaningful bar of not being denied.
func TestStrictScope_NoReadPathLosesItsIdentity(t *testing.T) {
	h := newHarness(t)
	orgID, projectID := h.seedOrganization("Identity Org", "walker", "correct-horse-battery")
	token := h.login("walker", "correct-horse-battery")
	seeded := h.seedOneOfEverything(token, projectID)

	underStrictScope(t)
	gorms.ResetDeniedSites()

	paths := []string{
		"/api/v1/organizations",
		"/api/v1/organizations/" + orgID.String(),
		"/api/v1/organizations/" + orgID.String() + "/users",
		"/api/v1/organizations/" + orgID.String() + "/groups",
		"/api/v1/projects",
		"/api/v1/projects/" + projectID.String(),
		"/api/v1/definitions?project_id=" + projectID.String(),
		"/api/v1/definitions/javascript-conditions",
		"/api/v1/decisions?project_id=" + projectID.String(),
		"/api/v1/instances?project_id=" + projectID.String(),
		"/api/v1/tasks",
		"/api/v1/tasks/assignee/walker",
		"/api/v1/connectors",
		"/api/v1/connectors/instances",
		"/api/v1/connector-manifests",
		"/api/v1/notifications",
		"/api/v1/webhooks",
		"/api/v1/setup/status",
	}
	if seeded.instanceID != uuid.Nil {
		paths = append(paths,
			"/api/v1/instances/"+seeded.instanceID.String(),
			"/api/v1/instances/"+seeded.instanceID.String()+"/audit",
			"/api/v1/instances/"+seeded.instanceID.String()+"/path",
			"/api/v1/instances/"+seeded.instanceID.String()+"/subprocesses",
			"/api/v1/incidents/"+seeded.instanceID.String(),
		)
	}
	if seeded.definitionID != uuid.Nil {
		paths = append(paths, "/api/v1/definitions/"+seeded.definitionID.String()+"/export")
	}

	for _, path := range paths {
		// A 404 is a legitimate answer here — some of these name a thing that
		// exists but has no children yet. What is not legitimate is reaching a
		// repository without an identity, which is what the assertion below
		// reads, and which no status code reveals.
		h.do(http.MethodGet, path, token, nil, nil)
	}

	assertNothingWasDenied(t)
}

// Background work spans every tenant and must say so. A worker that lost its
// system marker does not fail — it reads nothing and looks idle, which is the
// hardest version of this bug to notice in a staging environment.
func TestStrictScope_BackgroundWorkersKeepTheirSystemIdentity(t *testing.T) {
	h := newHarness(t)
	_, projectID := h.seedOrganization("Worker Org", "watcher", "correct-horse-battery")
	token := h.login("watcher", "correct-horse-battery")

	underStrictScope(t)
	gorms.ResetDeniedSites()

	h.svc.StartWorkers(t.Context())

	if status := h.do(http.MethodPost, "/api/v1/definitions", token,
		map[string]any{"definition": timerThenApproval(projectID, "worker-drill", "watcher")}, nil); status != http.StatusOK {
		t.Fatalf("deploy definition: status %d", status)
	}
	var started struct {
		InstanceID uuid.UUID `json:"instance_id"`
	}
	if status := h.do(http.MethodPost, "/api/v1/process/start", token,
		map[string]any{"project_id": projectID.String(), "definition_key": "worker-drill"}, &started); status != http.StatusOK {
		t.Fatalf("start process: status %d", status)
	}

	// The timer only fires if the worker's queries carry a system identity.
	//
	// Waited on with an eye on the denial list rather than on the clock alone.
	// A lost system context shows up there within a tick or two, while waiting
	// for the task to appear takes the full timeout to conclude the same
	// thing — and concludes it as "never appeared", which is a symptom rather
	// than the name of the path that has to change.
	h.waitForTaskOrDenial(t, token, "Approve")

	assertNothingWasDenied(t)
}

// waitForTaskOrDenial waits for the worker to do its job, and gives up early if
// the strict scope refuses one of its queries.
func (h *harness) waitForTaskOrDenial(t *testing.T, token, name string) {
	t.Helper()
	done := make(chan struct{})
	go func() {
		defer close(done)
		h.waitForTask(token, name)
	}()

	ticker := time.NewTicker(200 * time.Millisecond)
	defer ticker.Stop()
	for {
		select {
		case <-done:
			return
		case <-ticker.C:
			if len(gorms.DeniedSites()) > 0 {
				// The wait goroutine is left to finish against the test
				// context; assertNothingWasDenied is what reports, and it
				// prints the paths rather than the symptom.
				assertNothingWasDenied(t)
				return
			}
		}
	}
}

// assertNothingWasDenied turns the flag's diagnostic into a build failure.
//
// The message is the same one an operator would read off a staging log during
// the rollout, so a developer who trips it gets the answer rather than a
// puzzle.
func assertNothingWasDenied(t *testing.T) {
	t.Helper()
	denied := gorms.DeniedSites()
	if len(denied) == 0 {
		return
	}
	message := fmt.Sprintf("%d path(s) reached a repository with neither a tenant nor a system identity:\n", len(denied))
	for _, site := range denied {
		message += "  - " + site + "\n"
	}
	t.Fatal(message +
		"\nEach needs entities.WithSystemContext if it is background work, or a resolved tenant if it serves a request.\n" +
		"Under the strict scope these answer with nothing rather than failing, so they would not show up as an error\n" +
		"in production — they would show up as data that quietly went missing.")
}

// seededData records what was created, so the sweep can assert that reads
// return it rather than merely returning.
type seededData struct {
	projectID    uuid.UUID
	definitionID uuid.UUID
	instanceID   uuid.UUID
}

type scopedRead struct {
	name       string
	path       string
	collection string
}

func (s seededData) reads() []scopedRead {
	return []scopedRead{
		{"organizations", "/api/v1/organizations", "organizations"},
		{"projects", "/api/v1/projects", "projects"},
		{"definitions", "/api/v1/definitions?project_id=" + s.projectID.String(), "definitions"},
		{"instances", "/api/v1/instances?project_id=" + s.projectID.String(), "instances"},
	}
}

// seedOneOfEverything creates data through the HTTP API, which exercises the
// write paths on the way in.
func (h *harness) seedOneOfEverything(token string, projectID uuid.UUID) seededData {
	h.t.Helper()
	seeded := seededData{projectID: projectID}

	if status := h.do(http.MethodPost, "/api/v1/definitions", token,
		map[string]any{"definition": timerThenApproval(projectID, "sweep-drill", "sweeper")}, nil); status != http.StatusOK {
		h.t.Fatalf("seed definition: status %d", status)
	}

	var list struct {
		Definitions []struct {
			ID uuid.UUID `json:"id"`
		} `json:"definitions"`
	}
	if status := h.do(http.MethodGet, "/api/v1/definitions?project_id="+projectID.String(), token, nil, &list); status == http.StatusOK && len(list.Definitions) > 0 {
		seeded.definitionID = list.Definitions[0].ID
	}

	var started struct {
		InstanceID uuid.UUID `json:"instance_id"`
	}
	if status := h.do(http.MethodPost, "/api/v1/process/start", token,
		map[string]any{"project_id": projectID.String(), "definition_key": "sweep-drill"}, &started); status == http.StatusOK {
		seeded.instanceID = started.InstanceID
	}

	return seeded
}
