package deviation_test

import (
	"encoding/json"
	"net/http"
	"net/url"
	"strings"
	"testing"

	"github.com/gsoultan/metis/server/domains/entities"
)

// A caller starts an instance with its own name for the case, reads it back
// on the instance, and finds the instance by it in the project's list. A key
// that cannot be kept as written is a 400 and starts nothing.
func TestAnInstanceStartedOverRESTWithABusinessKeyIsFoundByIt(t *testing.T) {
	h := newDeviationHarness(t)
	h.startOneStep(t, entities.Node{Name: "Approve", Type: entities.UserTask, Assignee: "alice"})
	key := "deviation-one-step-1"
	admin := h.signIn(t, "boss", entities.RoleAdmin)

	start := func(businessKey string) (int, string) {
		return h.do(t, http.MethodPost, admin, "/api/v1/process/start", map[string]any{
			"project_id": h.projID.String(), "definition_key": key, "business_key": businessKey,
		})
	}
	status, body := start("ORDER-4411")
	if status != http.StatusOK {
		t.Fatalf("starting with a business key answered %d (%s), want 200", status, body)
	}
	var started struct {
		InstanceID string `json:"instance_id"`
	}
	if err := json.Unmarshal([]byte(body), &started); err != nil || started.InstanceID == "" {
		t.Fatalf("the start answered no instance: %s", body)
	}
	if status, body := start("ORDER-9000"); status != http.StatusOK {
		t.Fatalf("starting another case answered %d (%s)", status, body)
	}
	if status, body := start("ORDER\n4411"); status != http.StatusBadRequest {
		t.Errorf("a business key with a line break answered %d (%s), want 400", status, body)
	}
	if status, body := start(strings.Repeat("k", entities.MaxBusinessKeyLength+1)); status != http.StatusBadRequest {
		t.Errorf("a business key longer than the column answered %d (%s), want 400", status, body)
	}

	status, body = h.do(t, http.MethodGet, admin, "/api/v1/instances/"+started.InstanceID, nil)
	if status != http.StatusOK || !strings.Contains(body, `"business_key":"ORDER-4411"`) {
		t.Errorf("the instance answered %d (%s), want it to carry ORDER-4411", status, body)
	}

	query := url.Values{"project_id": {h.projID.String()}, "business_key": {"ORDER-4411"}}
	status, body = h.do(t, http.MethodGet, admin, "/api/v1/instances?"+query.Encode(), nil)
	if status != http.StatusOK {
		t.Fatalf("listing by business key answered %d (%s)", status, body)
	}
	var listed struct {
		Instances []struct {
			ID          string `json:"id"`
			BusinessKey string `json:"business_key"`
		} `json:"instances"`
	}
	if err := json.Unmarshal([]byte(body), &listed); err != nil {
		t.Fatalf("read the list %s: %v", body, err)
	}
	if len(listed.Instances) != 1 || listed.Instances[0].ID != started.InstanceID {
		t.Errorf("listing ORDER-4411 returned %+v, want only the instance started for it", listed.Instances)
	}
}
