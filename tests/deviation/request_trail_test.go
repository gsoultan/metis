package deviation_test

import (
	"net/http"
	"strings"
	"testing"

	"github.com/gsoultan/metis/server/domains/entities"
	serviceimpl "github.com/gsoultan/metis/server/domains/services/impl"
)

// The trail entry of a request is permanent, so what it says of who may
// approve has to be true wherever it is written. "A different administrator"
// is not: in an organization named as having one administrator, the
// administrator who asked approves. The entry says what holds in both — the
// request waits for approval, and whoever asked cannot give it unless the
// organization has been set up as having one administrator — in the words the
// migration dialog uses for the same rule.
func TestTheTrailSaysARequestWaitsInWordsTrueOfEveryOrganization(t *testing.T) {
	for name, harness := range map[string]func(*testing.T) *deviationHarness{
		"an organization with a second administrator":       newDeviationRouteHarness,
		"an organization named as having one administrator": withOrganizationNamed,
	} {
		t.Run(name, func(t *testing.T) {
			h := harness(t)
			boss := h.signIn(t, "boss", entities.RoleAdmin)
			instanceID := h.oneStep(t)
			requestID := h.askToWaive(t, boss, instanceID)
			status, read, raw := h.readRequest(t, boss, requestID)
			if status != http.StatusOK {
				t.Fatalf("read the request: %d (%s)", status, raw)
			}
			want := "boss asked for “Approve” to be waived — nobody would perform it. " +
				"Nothing changes until it is approved. The administrator who asked cannot approve it, " +
				"unless this organization has been set up as having one administrator. " +
				"The request expires on " + read.Request.ExpiresAt.UTC().Format(decidedOn) + ". Reason: " + routeReason
			entries := h.entriesOf(t, instanceID, serviceimpl.EventDeviationRequested)
			if len(entries) != 1 || entries[0].Narrative != want {
				t.Fatalf("the trail: %+v\nwant one entry that says\n  %s", entries, want)
			}
			if strings.Contains(entries[0].Narrative, "different administrator") {
				t.Fatalf("the entry promises a different administrator, which is not true of every organization: %s", entries[0].Narrative)
			}
		})
	}
}
