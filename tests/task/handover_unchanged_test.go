package task_test

import (
	"net/http"
	"strings"
	"testing"

	"github.com/gsoultan/metis/server/domains/entities"
)

// The inbox's Release button goes over Connect, and Connect's UnclaimTask
// carries an id and nothing else (server/transports/connects/tasks/handler.go;
// gRPC builds the same request). Moving who-may-release into the service, and
// asking everybody but the holder why, must leave that call working for the
// person who holds the task: they are never asked.
func TestTheHolderStillReleasesOverConnectWhichCarriesNoReason(t *testing.T) {
	h := newTaskHarness(t)
	taskID := h.assignTaskTo(t, "alice")

	// Connect's unary protocol is a JSON POST to the procedure, mounted under
	// /api/v1 like the REST routes (tests/connectcodes does the same).
	status, reply := h.post(t, h.tokens["alice"], "/api/v1/process.TaskService/UnclaimTask", map[string]any{"id": taskID})
	if status != http.StatusOK || strings.Contains(reply, `"error"`) {
		t.Fatalf("alice releasing her own task over Connect: got %d (%s), want 200 and no error", status, strings.TrimSpace(reply))
	}
	if got := h.taskStatus(t, taskID); got != string(entities.TaskUnclaimed) {
		t.Fatalf("after alice released it over Connect the task is %q, want unclaimed", got)
	}
}
