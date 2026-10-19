package deviation_test

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/gsoultan/metis/server/domains/entities"
)

// A retention hold is placed and released over its own routes, by an
// administrator of the instance's organization, each time with a reason that
// the instance's trail keeps. Reading it is open to whoever may read the
// instance; another organization's administrator learns nothing of it.
func TestARetentionHoldIsPlacedAndReleasedByAnAdministratorWithAReason(t *testing.T) {
	h := newDeviationHarness(t)
	instanceID := h.startOneStep(t, entities.Node{Name: "Approve", Type: entities.UserTask, Assignee: "alice"})
	hold := "/api/v1/instances/" + instanceID.String() + "/retention-hold"
	release := hold + "/release"
	list := "/api/v1/projects/" + h.projID.String() + "/retention-holds"
	reason := map[string]string{"reason": "litigation 2026-114"}

	admin := h.signIn(t, "boss", entities.RoleAdmin)
	reader := h.signIn(t, "reader", entities.RoleUser)
	outsider := h.signInElsewhere(t, "outsider-boss", entities.RoleAdmin)

	if status, body := h.do(t, http.MethodPost, "", hold, reason); status != http.StatusUnauthorized {
		t.Fatalf("placing a hold signed out answered %d (%s), want 401", status, body)
	}
	for _, role := range []string{entities.RoleUser, entities.RoleOperator, entities.RoleDesigner} {
		token := h.signIn(t, "not-an-admin-"+role, role)
		if status, body := h.do(t, http.MethodPost, token, hold, reason); status != http.StatusForbidden {
			t.Errorf("a %s placing a hold answered %d (%s), want 403", role, status, body)
		}
		if status, body := h.do(t, http.MethodGet, token, list, nil); status != http.StatusForbidden {
			t.Errorf("a %s listing the project's holds answered %d (%s), want 403", role, status, body)
		}
	}
	if status, body := h.do(t, http.MethodPost, outsider, hold, reason); status != http.StatusNotFound {
		t.Errorf("another organization's administrator placing a hold answered %d (%s), want 404", status, body)
	}
	if status, body := h.do(t, http.MethodPost, admin, hold, map[string]string{"reason": "  "}); status != http.StatusBadRequest {
		t.Errorf("a hold with no reason answered %d (%s), want 400", status, body)
	}
	if status, body := h.do(t, http.MethodPost, admin, hold, map[string]any{"reason": "x", "placed_by": "someone else"}); status != http.StatusBadRequest {
		t.Errorf("a hold naming who placed it answered %d (%s), want 400: the caller places it", status, body)
	}
	if got := readHold(t, h, reader, hold); got.Held {
		t.Fatalf("the instance reads as held before any hold was placed: %+v", got)
	}

	if status, body := h.do(t, http.MethodPost, admin, hold, reason); status != http.StatusOK {
		t.Fatalf("the administrator placing a hold answered %d (%s), want 200", status, body)
	}
	got := readHold(t, h, reader, hold)
	if !got.Held || got.Hold.PlacedBy != "boss" || got.Hold.Reason != "litigation 2026-114" ||
		got.Hold.InstanceID != instanceID.String() || got.Hold.ProjectID != h.projID.String() {
		t.Errorf("the hold reads back as %+v, want it placed by boss for litigation 2026-114", got)
	}
	if status, body := h.do(t, http.MethodGet, outsider, hold, nil); status != http.StatusNotFound {
		t.Errorf("another organization's administrator reading the hold answered %d (%s), want 404", status, body)
	}
	if status, body := h.do(t, http.MethodPost, admin, hold, reason); status != http.StatusBadRequest {
		t.Errorf("a second hold on a held instance answered %d (%s), want 400", status, body)
	}
	status, body := h.do(t, http.MethodGet, admin, list, nil)
	if status != http.StatusOK || !strings.Contains(body, instanceID.String()) {
		t.Errorf("the project's holds answered %d (%s), want 200 naming the held instance", status, body)
	}

	if status, body := h.do(t, http.MethodPost, outsider, release, reason); status != http.StatusNotFound {
		t.Errorf("another organization's administrator releasing the hold answered %d (%s), want 404", status, body)
	}
	if status, body := h.do(t, http.MethodPost, reader, release, reason); status != http.StatusForbidden {
		t.Errorf("a user releasing the hold answered %d (%s), want 403", status, body)
	}
	if status, body := h.do(t, http.MethodPost, admin, release, map[string]string{}); status != http.StatusBadRequest {
		t.Errorf("a release with no reason answered %d (%s), want 400", status, body)
	}
	if got := readHold(t, h, reader, hold); !got.Held {
		t.Fatalf("a refused release removed the hold")
	}
	if status, body := h.do(t, http.MethodPost, admin, release, map[string]string{"reason": "case settled"}); status != http.StatusOK {
		t.Fatalf("the administrator releasing the hold answered %d (%s), want 200", status, body)
	}
	if got := readHold(t, h, reader, hold); got.Held {
		t.Errorf("the instance still reads as held after its hold was released: %+v", got)
	}
	if status, body := h.do(t, http.MethodPost, admin, release, map[string]string{"reason": "again"}); status != http.StatusNotFound {
		t.Errorf("releasing a hold that is gone answered %d (%s), want 404", status, body)
	}

	placed := h.entriesOf(t, instanceID, "retention_hold_placed")
	released := h.entriesOf(t, instanceID, "retention_hold_released")
	if len(placed) != 1 || placed[0].Message != "litigation 2026-114" {
		t.Errorf("the trail has %d placed entries (%+v), want one with the hold's reason", len(placed), placed)
	}
	if len(released) != 1 || released[0].Message != "case settled" {
		t.Errorf("the trail has %d released entries (%+v), want one with the release's reason", len(released), released)
	}
}

type holdRead struct {
	Held bool `json:"held"`
	Hold *struct {
		ProjectID  string `json:"project_id"`
		InstanceID string `json:"instance_id"`
		Reason     string `json:"reason"`
		PlacedBy   string `json:"placed_by"`
	} `json:"hold"`
}

func readHold(t *testing.T, h *deviationHarness, token, path string) holdRead {
	t.Helper()
	status, body := h.do(t, http.MethodGet, token, path, nil)
	if status != http.StatusOK {
		t.Fatalf("reading the hold answered %d (%s), want 200", status, body)
	}
	var got holdRead
	if err := json.Unmarshal([]byte(body), &got); err != nil {
		t.Fatalf("read the hold's answer %s: %v", body, err)
	}
	if got.Held && got.Hold == nil {
		t.Fatalf("the answer says held but carries no hold: %s", body)
	}
	return got
}
