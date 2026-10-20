package deviation_test

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/gsoultan/metis/server/domains/entities"
)

// A process's retention period is set over its own route, by an
// administrator of the project's organization, with a reason, and each change
// is kept. Reading the periods is open to whoever may read the project;
// another organization learns nothing of them.
func TestAProcessRetentionPeriodIsSetByAnAdministratorAndEveryChangeIsKept(t *testing.T) {
	h := newDeviationHarness(t)
	h.startOneStep(t, entities.Node{Name: "Approve", Type: entities.UserTask, Assignee: "alice"})
	key := "deviation-one-step-1"
	project := "/api/v1/projects/" + h.projID.String()
	set := project + "/retention-periods/" + key
	list := project + "/retention-periods"
	history := set + "/history"
	year := map[string]any{"days": 365, "reason": "records schedule 4.2"}

	admin := h.signIn(t, "boss", entities.RoleAdmin)
	reader := h.signIn(t, "reader", entities.RoleUser)
	outsider := h.signInElsewhere(t, "outsider-boss", entities.RoleAdmin)

	if status, body := h.do(t, http.MethodPut, "", set, year); status != http.StatusUnauthorized {
		t.Fatalf("setting a period signed out answered %d (%s), want 401", status, body)
	}
	for _, role := range []string{entities.RoleUser, entities.RoleOperator, entities.RoleDesigner} {
		token := h.signIn(t, "not-an-admin-"+role, role)
		if status, body := h.do(t, http.MethodPut, token, set, year); status != http.StatusForbidden {
			t.Errorf("a %s setting a period answered %d (%s), want 403", role, status, body)
		}
	}
	if status, body := h.do(t, http.MethodPut, outsider, set, year); status != http.StatusNotFound {
		t.Errorf("another organization's administrator setting a period answered %d (%s), want 404", status, body)
	}
	for name, body := range map[string]any{
		"no reason":        map[string]any{"days": 365},
		"no days at all":   map[string]any{"reason": "x"},
		"zero days":        map[string]any{"days": 0, "reason": "x"},
		"too many days":    map[string]any{"days": entities.MaxRetentionPeriodDays + 1, "reason": "x"},
		"days as text":     map[string]any{"days": "365", "reason": "x"},
		"an unknown field": map[string]any{"days": 365, "reason": "x", "set_by": "someone else"},
	} {
		if status, answer := h.do(t, http.MethodPut, admin, set, body); status != http.StatusBadRequest {
			t.Errorf("a period with %s answered %d (%s), want 400", name, status, answer)
		}
	}
	if status, body := h.do(t, http.MethodPut, admin, project+"/retention-periods/no-such-process", year); status != http.StatusNotFound {
		t.Errorf("a period for a process the project does not have answered %d (%s), want 404", status, body)
	}
	if got := readPeriods(t, h, reader, list); len(got) != 0 {
		t.Fatalf("the project lists periods before any was set: %+v", got)
	}

	if status, body := h.do(t, http.MethodPut, admin, set, year); status != http.StatusOK {
		t.Fatalf("the administrator setting a period answered %d (%s), want 200", status, body)
	}
	got := readPeriods(t, h, reader, list)
	if len(got) != 1 || got[0].ProcessKey != key || got[0].Days == nil || *got[0].Days != 365 || got[0].SetBy != "boss" {
		t.Errorf("the project's periods read back as %+v, want the year boss set", got)
	}
	if status, body := h.do(t, http.MethodGet, outsider, list, nil); status != http.StatusNotFound {
		t.Errorf("another organization's administrator reading the periods answered %d (%s), want 404", status, body)
	}

	if status, body := h.do(t, http.MethodPut, admin, set, map[string]any{"days": nil, "reason": "back to the installation's schedule"}); status != http.StatusOK {
		t.Fatalf("returning the process to the installation's setting answered %d (%s), want 200", status, body)
	}
	got = readPeriods(t, h, reader, list)
	if len(got) != 1 || got[0].Days != nil {
		t.Errorf("after returning to the installation's setting the periods read %+v, want the process with no days", got)
	}
	changes := readPeriods(t, h, reader, history)
	if len(changes) != 2 || changes[0].Days != nil || changes[1].Days == nil || *changes[1].Days != 365 ||
		changes[0].Reason != "back to the installation's schedule" || changes[1].Reason != "records schedule 4.2" {
		t.Errorf("the history reads %+v, want both changes, newest first, with their reasons", changes)
	}
}

type periodRead struct {
	ProcessKey string `json:"process_key"`
	Days       *int   `json:"days"`
	Reason     string `json:"reason"`
	SetBy      string `json:"set_by"`
}

func readPeriods(t *testing.T, h *deviationHarness, token, path string) []periodRead {
	t.Helper()
	status, body := h.do(t, http.MethodGet, token, path, nil)
	if status != http.StatusOK {
		t.Fatalf("reading %s answered %d (%s), want 200", path, status, body)
	}
	var got struct {
		Periods []periodRead `json:"periods"`
	}
	if err := json.Unmarshal([]byte(body), &got); err != nil {
		t.Fatalf("read the answer %s: %v", body, err)
	}
	return got.Periods
}
