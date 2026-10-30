package deviation_test

import (
	"encoding/json"
	"net/http"
	"testing"
	"time"

	"github.com/gsoultan/metis/server/domains/entities"
	"github.com/gsoultan/metis/server/repositories/models"
	"github.com/gsoultan/metis/server/repositories/store/processinstance"
	"github.com/gsoultan/metis/tests/testutils"
)

// An instance past its period is archived, and the archive is read over its
// own routes by whoever may read the project's instances: listed, and opened
// with everything kept of it. Another organization learns nothing of it.
func TestAnArchivedInstanceIsListedAndOpenedByItsOrganization(t *testing.T) {
	h := newDeviationHarness(t)
	instanceID := h.startOneStep(t, entities.Node{Name: "Approve", Type: entities.UserTask, Assignee: "alice"})
	finished := processinstance.MutateKey(instanceID)
	finished.SetStatus(string(models.ProcessCompleted))
	finished.SetUpdatedAt(time.Now().Add(-100 * 24 * time.Hour))
	if err := finished.Update(t.Context(), testutils.Executor(h.db)); err != nil {
		t.Fatalf("finish the instance long ago: %v", err)
	}
	archived, err := h.repo.Process().ArchiveFinished(entities.WithSystemContext(t.Context()), time.Now(), 90*24*time.Hour)
	if err != nil || archived != 1 {
		t.Fatalf("the sweep archived %d instances (%v), want 1", archived, err)
	}

	list := "/api/v1/projects/" + h.projID.String() + "/archived-instances"
	one := "/api/v1/archived-instances/" + instanceID.String()
	reader := h.signIn(t, "reader", entities.RoleUser)
	outsider := h.signInElsewhere(t, "outsider", entities.RoleAdmin)

	for _, path := range []string{list, one} {
		if status, body := h.do(t, http.MethodGet, "", path, nil); status != http.StatusUnauthorized {
			t.Errorf("GET %s signed out answered %d (%s), want 401", path, status, body)
		}
		if status, body := h.do(t, http.MethodGet, outsider, path, nil); status != http.StatusNotFound {
			t.Errorf("GET %s from another organization answered %d (%s), want 404", path, status, body)
		}
	}
	if status, body := h.do(t, http.MethodGet, reader, "/api/v1/instances/"+instanceID.String(), nil); status != http.StatusNotFound {
		t.Errorf("the archived instance still reads as live: %d (%s), want 404", status, body)
	}

	status, body := h.do(t, http.MethodGet, reader, list, nil)
	var page struct {
		Instances []struct {
			ID     string `json:"id"`
			Status string `json:"status"`
		} `json:"instances"`
		NextCursor string `json:"next_cursor"`
	}
	if status != http.StatusOK || json.Unmarshal([]byte(body), &page) != nil {
		t.Fatalf("the project's archive answered %d (%s), want 200", status, body)
	}
	if len(page.Instances) != 1 || page.Instances[0].ID != instanceID.String() || page.Instances[0].Status != "completed" || page.NextCursor != "" {
		t.Errorf("the project's archive is %+v, want the one completed instance and no next page", page)
	}

	status, body = h.do(t, http.MethodGet, reader, one, nil)
	var opened struct {
		Record models.ArchiveRecord `json:"record"`
	}
	if status != http.StatusOK || json.Unmarshal([]byte(body), &opened) != nil {
		t.Fatalf("opening the archived instance answered %d (%s), want 200", status, body)
	}
	if opened.Record.Instance.ID.String() != instanceID.String() || len(opened.Record.Tasks) != 1 || len(opened.Record.AuditTrail) == 0 {
		t.Errorf("the archive record holds instance %s, %d tasks and %d audit entries; want the instance, its task and its trail",
			opened.Record.Instance.ID, len(opened.Record.Tasks), len(opened.Record.AuditTrail))
	}

	if status, body := h.do(t, http.MethodGet, reader, list+"?cursor=nonsense", nil); status != http.StatusBadRequest {
		t.Errorf("a cursor the list never gave out answered %d (%s), want 400", status, body)
	}
}
