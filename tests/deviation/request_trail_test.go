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
	// Three configurations, and the sentence has to be true of each: not
	// named; named, with one administrator; and named, though it has a second
	// — where the administrator who asked still cannot approve, so "unless it
	// has been set up as having one" alone would promise what is refused.
	withASecond := func(t *testing.T) *deviationHarness {
		h := withOrganizationNamed(t)
		h.signIn(t, "deputy", entities.RoleAdmin)
		return h
	}
	for name, harness := range map[string]func(*testing.T) *deviationHarness{
		"an organization with a second administrator":             newDeviationRouteHarness,
		"an organization named as having one administrator":       withOrganizationNamed,
		"an organization named as having one, which has a second": withASecond,
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
				"unless this organization has been set up as having one administrator and nobody else administers it. " +
				"The request expires on " + read.Request.ExpiresAt.UTC().Format(decidedOn) + ". Reason: " + routeReason
			entries := h.entriesOf(t, instanceID, serviceimpl.EventDeviationRequested)
			if len(entries) != 1 || entries[0].Narrative != want {
				t.Fatalf("the trail: %+v\nwant one entry that says\n  %s", entries, want)
			}
			// And where it has a second, the administrator who asked is in
			// fact refused, named or not: what the sentence must not promise.
			if strings.Contains(name, "which has a second") {
				if status, _, raw := h.decide(t, boss, requestID, "approve", "nobody else is here"); status != http.StatusForbidden {
					t.Fatalf("the requester's own approval in a named organization that has a second administrator: %d (%s), want 403", status, raw)
				}
			}
			if strings.Contains(entries[0].Narrative, "different administrator") {
				t.Fatalf("the entry promises a different administrator, which is not true of every organization: %s", entries[0].Narrative)
			}
		})
	}
}

// The two kinds of request record the same things the same way. A waive its
// requester approved says on the request itself what the exception rested
// on — that nobody else approved, that no other administrator was found, and
// in which organization — as a migration's request does; and a request found
// stale says who found it, on the request, for a waive as for a migration.
func TestAWaivesRequestRecordsASelfApprovalAndAStaleFindingAsAMigrationsDoes(t *testing.T) {
	t.Run("self-approved", func(t *testing.T) {
		h := withOrganizationNamed(t)
		boss := h.signIn(t, "boss", entities.RoleAdmin)
		requestID := h.askToWaive(t, boss, h.oneStep(t))
		status, approved, raw := h.decide(t, boss, requestID, "approve", "nobody else is here; the director agreed")
		if status != http.StatusOK || !approved.Request.SelfApproved {
			t.Fatalf("the sole administrator's own approval: %d (%s)", status, raw)
		}
		outcome := approved.Request.Outcome
		if outcome["self_approved"] != true || outcome["other_administrators"] != float64(0) || outcome["organization_id"] != h.orgID.String() {
			t.Fatalf("the request's outcome is %v; want it to say nobody else approved, that no other administrator was found, and in which organization", outcome)
		}
	})
	t.Run("approved by a second administrator", func(t *testing.T) {
		h := newDeviationRouteHarness(t)
		boss, deputy := h.signIn(t, "boss", entities.RoleAdmin), h.signIn(t, "deputy", entities.RoleAdmin)
		requestID := h.askToWaive(t, boss, h.oneStep(t))
		status, approved, raw := h.decide(t, deputy, requestID, "approve", "")
		if status != http.StatusOK || approved.Request.SelfApproved {
			t.Fatalf("the deputy's approval: %d (%s)", status, raw)
		}
		for _, key := range []string{"self_approved", "other_administrators", "organization_id"} {
			if _, said := approved.Request.Outcome[key]; said {
				t.Fatalf("a request a second administrator approved says %s: %v", key, approved.Request.Outcome)
			}
		}
	})
	t.Run("found stale", func(t *testing.T) {
		h := newDeviationRouteHarness(t)
		boss, deputy := h.signIn(t, "boss", entities.RoleAdmin), h.signIn(t, "deputy", entities.RoleAdmin)
		moved := h.start(t, twoSteps())
		requestID := h.askToWaive(t, boss, moved)
		h.completeStep(t, moved)
		if status, _, raw := h.decide(t, deputy, requestID, "approve", ""); status != http.StatusBadRequest {
			t.Fatalf("the deputy's approval of a request the instance left behind: %d (%s), want 400", status, raw)
		}
		_, read, raw := h.readRequest(t, deputy, requestID)
		entries := h.entriesOf(t, moved, serviceimpl.EventDeviationStale)
		if read.Request.Status != "stale" || read.Request.Outcome["attempted_by"] != "deputy" || len(entries) != 1 || entries[0].Data["attempted_by"] != "deputy" {
			t.Fatalf("the stale request reads %s and its trail entry carries %v; want who found it on both", raw, entries)
		}
	})
}
