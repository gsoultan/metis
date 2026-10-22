package deviation_test

import (
	"encoding/json"
	"net/http"
	"net/url"
	"slices"
	"testing"

	"github.com/google/uuid"
	"github.com/gsoultan/metis/server/domains/entities"
)

type operationRow struct {
	ID         string `json:"id"`
	At         string `json:"at"`
	Actor      string `json:"actor"`
	Type       string `json:"type"`
	ProjectID  string `json:"project_id"`
	InstanceID string `json:"instance_id"`
	Narrative  string `json:"narrative"`
}

type operationPage struct {
	Operations []operationRow `json:"operations"`
	Next       string         `json:"next"`
}

// operations reads one page of the audit log as token's account, failing
// unless the reply is the status wanted.
func (h *deviationHarness) operations(t *testing.T, token string, query url.Values, want int) operationPage {
	t.Helper()
	path := "/api/v1/audit/operations"
	if len(query) > 0 {
		path += "?" + query.Encode()
	}
	status, reply := h.do(t, http.MethodGet, token, path, nil)
	if status != want {
		t.Fatalf("reading %s answered %d (%s), want %d", path, status, reply, want)
	}
	var page operationPage
	if want == http.StatusOK {
		if err := json.Unmarshal([]byte(reply), &page); err != nil {
			t.Fatalf("read %s: %v", reply, err)
		}
	}
	return page
}

func (p operationPage) has(actor, eventType string, instance uuid.UUID) bool {
	return slices.ContainsFunc(p.Operations, func(row operationRow) bool {
		return row.Actor == actor && row.Type == eventType && row.InstanceID == instance.String()
	})
}

// Who waived, approved or rejected what was written down only on each
// instance's own history, inside sealed data. The audit log reads it across
// the organization: every entry somebody's act wrote, newest first, filtered
// by who, what and when, a page at a time. It is an administrator's of the
// organization, and holds nothing of another's.
func TestTheAuditLogListsWhoDidWhatAcrossTheOrganization(t *testing.T) {
	h := newDeviationRouteHarness(t)
	boss, deputy := h.signIn(t, "boss", entities.RoleAdmin), h.signIn(t, "deputy", entities.RoleAdmin)
	operator := h.signIn(t, "ops", entities.RoleOperator)
	outsider := h.signInElsewhere(t, "outsider-boss", entities.RoleAdmin)

	approved := h.oneStep(t)
	if status, _, raw := h.decide(t, deputy, h.askToWaive(t, boss, approved), "approve", ""); status != http.StatusOK {
		t.Fatalf("approving the waive: %d (%s)", status, raw)
	}
	rejected := h.oneStep(t)
	if status, _, raw := h.decide(t, deputy, h.askToWaive(t, boss, rejected), "reject", "not this one"); status != http.StatusOK {
		t.Fatalf("rejecting the waive: %d (%s)", status, raw)
	}

	whole := h.operations(t, boss, nil, http.StatusOK)
	for _, want := range []struct {
		actor, eventType string
		instance         uuid.UUID
	}{
		{"boss", "deviation_requested", approved},
		{"deputy", "deviation_approved", approved},
		{"boss", "deviation_requested", rejected},
		{"deputy", "deviation_rejected", rejected},
	} {
		if !whole.has(want.actor, want.eventType, want.instance) {
			t.Errorf("the log has no %s by %s on %s: %+v", want.eventType, want.actor, want.instance, whole.Operations)
		}
	}
	for i, row := range whole.Operations {
		if row.Actor == "" || row.Narrative == "" || row.ProjectID != h.projID.String() {
			t.Errorf("entry %d does not say who, what and where: %+v", i, row)
		}
		if row.Type == "process_started" || row.Type == "node_reached" {
			t.Errorf("the engine's own entry %s is in the log of what people did", row.Type)
		}
		if i > 0 && row.At > whole.Operations[i-1].At {
			t.Errorf("entry %d (%s) is newer than the one before it (%s): the log is newest first", i, row.At, whole.Operations[i-1].At)
		}
	}
	if whole.Next != "" {
		t.Errorf("a log that fits on one page says there is another: %q", whole.Next)
	}
	if first := whole.Operations[0]; first.Type != "deviation_rejected" || first.Actor != "deputy" {
		t.Errorf("the newest entry is %s by %s, want the rejection, the last thing anybody did", first.Type, first.Actor)
	}

	byDeputy := h.operations(t, boss, url.Values{"actor": {"deputy"}}, http.StatusOK)
	if len(byDeputy.Operations) == 0 || slices.ContainsFunc(byDeputy.Operations, func(row operationRow) bool { return row.Actor != "deputy" }) {
		t.Errorf("the deputy's acts: %+v", byDeputy.Operations)
	}
	requests := h.operations(t, boss, url.Values{"type": {"deviation_requested"}}, http.StatusOK)
	if len(requests.Operations) != 2 || requests.Operations[0].InstanceID != rejected.String() || requests.Operations[1].InstanceID != approved.String() {
		t.Errorf("the requests, newest first: %+v", requests.Operations)
	}
	inProject := h.operations(t, boss, url.Values{"project": {h.projID.String()}}, http.StatusOK)
	if len(inProject.Operations) != len(whole.Operations) {
		t.Errorf("the project's log has %d entries, the organization's %d", len(inProject.Operations), len(whole.Operations))
	}
	future := h.operations(t, boss, url.Values{"from": {"2999-01-01T00:00:00Z"}}, http.StatusOK)
	if len(future.Operations) != 0 || future.Operations == nil {
		t.Errorf("entries from the year 2999: %+v; want an empty list, not null", future.Operations)
	}

	// A page at a time, each continuing where the last ended, comes to
	// exactly the whole log in the same order.
	var paged []string
	query := url.Values{"limit": {"1"}}
	for range len(whole.Operations) + 1 {
		page := h.operations(t, boss, query, http.StatusOK)
		for _, row := range page.Operations {
			paged = append(paged, row.ID)
		}
		if page.Next == "" {
			break
		}
		query.Set("before", page.Next)
	}
	var ids []string
	for _, row := range whole.Operations {
		ids = append(ids, row.ID)
	}
	if !slices.Equal(paged, ids) {
		t.Errorf("paged one at a time the log reads %v, whole %v", paged, ids)
	}

	// An operator runs instances but does not read everybody's acts, and an
	// administrator of another organization reads a log with none of these.
	h.operations(t, operator, nil, http.StatusForbidden)
	if elsewhere := h.operations(t, outsider, nil, http.StatusOK); len(elsewhere.Operations) != 0 {
		t.Errorf("another organization's administrator reads %+v", elsewhere.Operations)
	}
	for _, bad := range []url.Values{
		{"from": {"yesterday"}},
		{"before": {"not-a-cursor"}},
		{"limit": {"0"}},
		{"project": {"nope"}},
		{"from": {"2026-02-01T00:00:00Z"}, "to": {"2026-01-01T00:00:00Z"}},
	} {
		h.operations(t, boss, bad, http.StatusBadRequest)
	}
}
