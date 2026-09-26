package slo

import (
	"fmt"
	"net/http"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/gsoultan/metis/server/repositories/pg"
)

// Each request works out its tenant's scope once.
//
// A scoped repository call finds what the caller may see by reading the ids of
// every project in the caller's organization, and a request makes several such
// calls — the dashboard's statistics make six, starting a process makes more.
// Each call read the list again, so an organization paid for the size of its
// project list once per call: at ten thousand projects, four milliseconds and
// ten megabytes a call, and the statistics went past the read target
// (docs/performance.md).
//
// Counted rather than timed, so it holds on any machine and fails on the first
// request that reads the list twice. Exactly once, not at most once: a request
// that reads it no times at all was not scoped. The requests are the ones a
// person opening the product makes, and the two actions the engine does the most
// work in.
func TestEachRequestReadsItsTenantScopeOnce(t *testing.T) {
	h := newHarness(t)
	project := h.projID.String()

	var failures []string
	count := func(method, path string, body, out any) {
		t.Helper()
		before := pg.ScopeReads()
		status := h.call(method, path, h.token, body, out)
		reads := pg.ScopeReads() - before
		t.Logf("%-4s %-60s %d  reads=%d", method, path, status, reads)
		if status != http.StatusOK {
			t.Fatalf("%s %s answered %d; a refusal says nothing about what a request costs", method, path, status)
		}
		if reads != 1 {
			failures = append(failures, fmt.Sprintf("%s %s read the organization's project list %d times", method, path, reads))
		}
	}

	var started struct {
		InstanceID uuid.UUID `json:"instance_id"`
	}
	count(http.MethodPost, "/api/v1/process/start",
		map[string]any{"project_id": project, "definition_key": "slo-approval"}, &started)
	var listed struct {
		Tasks []struct {
			ID uuid.UUID `json:"id"`
		} `json:"tasks"`
	}
	count(http.MethodGet, "/api/v1/tasks?instance_id="+started.InstanceID.String(), nil, &listed)
	if len(listed.Tasks) != 1 {
		t.Fatalf("the started approval has %d tasks, want 1", len(listed.Tasks))
	}
	task := listed.Tasks[0].ID.String()

	count(http.MethodGet, "/api/v1/tasks?page=1&page_size=25", nil, nil)
	count(http.MethodGet, "/api/v1/tasks/assignee/load", nil, nil)
	count(http.MethodGet, "/api/v1/tasks/"+task, nil, nil)
	count(http.MethodGet, "/api/v1/instances?project_id="+project+"&page=1&page_size=25", nil, nil)
	count(http.MethodGet, "/api/v1/instances/"+started.InstanceID.String(), nil, nil)
	count(http.MethodGet, "/api/v1/definitions?project_id="+project, nil, nil)
	count(http.MethodGet, "/api/v1/projects/"+project+"/waiting", nil, nil)
	// The dashboard's statistics, a Connect call sent as JSON the way the UI
	// sends it.
	count(http.MethodPost, "/api/v1/process.StatsService/GetProcessStatistics", map[string]any{"projectId": project}, nil)
	count(http.MethodPost, "/api/v1/tasks/"+task+"/complete", map[string]any{}, nil)

	if len(failures) > 0 {
		t.Errorf("a request reads its organization's project list once and every scoped call in it reuses that:\n  %s",
			strings.Join(failures, "\n  "))
	}
}
