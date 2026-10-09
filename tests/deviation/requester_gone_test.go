package deviation_test

import (
	"context"
	"net/http"
	"slices"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/gsoultan/metis/server/domains/entities"
	serviceimpl "github.com/gsoultan/metis/server/domains/services/impl"
)

// A request does not outlive its requester's standing.
//
// A second administrator approves what an administrator asked for. If the
// account that asked has since been deleted, has left the organization or no
// longer holds the administrator role there, approving would carry out a
// request on behalf of somebody who could no longer make it. So the approval
// asks, of the accounts as they are when it is made: the requester must still
// administer the request's organization. Otherwise the request is closed as
// stale, saying why, and the approver is told that nothing was applied and
// that somebody who administers the organization has to ask afresh.
//
// These go through the routes, against accounts that are really there: the
// question is asked of the accounts, so accounts are what it is tested
// against.

// requesterGone is what the approver is told.
const requesterGone = "The administrator who asked for this, boss, no longer administers this organization, " +
	"so nothing was applied. It has to be asked for afresh by somebody who does."

// departure is one way an administrator stops administering an organization
// after asking for something in it.
type departure struct {
	name string
	// spec is the requester as they were when they asked.
	spec enrolled
	// leave makes the change, and why is what the request then records.
	leave func(t *testing.T, h *deviationHarness, boss uuid.UUID)
	why   string
	// refused is what the requester's own later rejection is answered.
	refused int
	// restart says the change was written under the account service, which
	// keeps who a token belongs to for a while: the server is started again
	// before the requester's next request, so that it is answered from the
	// accounts as they are and not from what the account used to be.
	restart bool
}

func system() context.Context { return entities.WithSystemContext(context.Background()) }

func (h *deviationHarness) asSystemHere() context.Context {
	return entities.WithTenantContext(system(), entities.TenantContext{TenantID: h.orgID.String()})
}

func departures() []departure {
	admin := []string{entities.RoleAdmin}
	return []departure{
		{
			name: "the administrator role held in this organization is taken away",
			spec: enrolled{here: true, rolesHere: admin},
			leave: func(t *testing.T, h *deviationHarness, boss uuid.UUID) {
				if err := h.svc.SetOrganizationRoles(h.asSystemHere(), boss, []string{entities.RoleOperator}); err != nil {
					t.Fatalf("take the administrator role from the requester: %v", err)
				}
			},
			why:     "boss, who asked for it, no longer administers this organization: their account no longer holds the administrator role in it",
			refused: http.StatusForbidden,
		},
		{
			name: "the administrator role held on the account is taken away",
			spec: enrolled{global: admin, here: true},
			leave: func(t *testing.T, h *deviationHarness, boss uuid.UUID) {
				if err := h.svc.UpdateUser(system(), entities.User{ID: boss, Roles: []string{entities.RoleUser}}); err != nil {
					t.Fatalf("take the administrator role from the requester's account: %v", err)
				}
			},
			why:     "boss, who asked for it, no longer administers this organization: their account no longer holds the administrator role in it",
			refused: http.StatusForbidden,
		},
		{
			name: "the account is deleted",
			spec: enrolled{global: admin, here: true},
			leave: func(t *testing.T, h *deviationHarness, boss uuid.UUID) {
				h.remove(t, boss)
			},
			why:     "boss, who asked for it, no longer administers this organization: their account has been deleted",
			refused: http.StatusUnauthorized,
		},
		{
			// The write an identity provider's sign-in makes when its claim
			// no longer names the organization (pg/user_identity.go); no
			// route removes one membership of a local account.
			name: "the account leaves the organization, keeping the role on the account",
			spec: enrolled{global: admin, here: true, elsewhere: true},
			leave: func(t *testing.T, h *deviationHarness, boss uuid.UUID) {
				if err := h.repo.User().RemoveOrganization(system(), boss, h.orgID); err != nil {
					t.Fatalf("take the requester out of the organization: %v", err)
				}
			},
			why:     "boss, who asked for it, no longer administers this organization: their account no longer belongs to it",
			refused: http.StatusUnauthorized,
			restart: true,
		},
	}
}

// A waive: the approval closes the request and its ledger row as stale, says
// why on the request and on the trail, and leaves the step as it was.
func TestAWaiveIsNotApprovedOnceItsRequesterNoLongerAdministers(t *testing.T) {
	for _, gone := range departures() {
		t.Run(gone.name, func(t *testing.T) {
			h := newDeviationRouteHarness(t)
			deputy := h.signIn(t, "deputy", entities.RoleAdmin)
			boss, bossToken := h.enrol(t, "boss", gone.spec)
			instanceID := h.oneStep(t)
			requestID := h.askToWaive(t, bossToken, instanceID)
			gone.leave(t, h, boss)

			waited := h.everyRow(t)
			want := invalid(requesterGone)
			if status, _, raw := h.decide(t, deputy, requestID, "approve", decisionReason); status != http.StatusBadRequest || !sameJSON(t, raw, want) {
				t.Fatalf("deputy's approval of a request whose requester no longer administers: %d (%s), want 400 %s", status, raw, want)
			}
			h.requireOnlyTheClosing(t, waited, "an approval that found the requester gone")
			status, read, raw := h.readRequest(t, deputy, requestID)
			if status != http.StatusOK || read.Request.Status != "stale" || read.Request.DecidedBy != "" || read.Request.DecidedAt.IsZero() ||
				read.Request.Outcome["why"] != gone.why || read.Request.Outcome["attempted_by"] != "deputy" || h.requestStatus(t, requestID) != "stale" {
				t.Fatalf("after the 400 the request reads %d (%s), stored as %s; want it stale, decided by nobody, saying who tried and\n  %s",
					status, raw, h.requestStatus(t, requestID), gone.why)
			}
			if !h.stepIsOpen(t, instanceID) {
				t.Fatal("the step was waived on a request whose requester no longer administers")
			}
			_, ledger, raw := h.readDeviations(t, deputy, instanceID.String())
			if len(ledger.Deviations) != 1 || ledger.Deviations[0]["status"] != "stale" {
				t.Fatalf("the ledger: %s, want the one row that waited, stale", raw)
			}
			entries := h.entriesOf(t, instanceID, serviceimpl.EventDeviationStale)
			wantNarrative := "The request to waive “Approve” that boss made no longer held when deputy tried to approve it, so nothing changed: " + gone.why
			if len(entries) != 1 || entries[0].Narrative != wantNarrative || entries[0].Data["attempted_by"] != "deputy" {
				t.Fatalf("the trail: %+v\nwant one entry that says\n  %s", entries, wantNarrative)
			}

			// Closed, a later decision is told why it was closed — not that
			// "what it asked for no longer held", which is untrue of this
			// cause — and the same waive can be asked for again by somebody
			// who administers.
			closed := h.everyRow(t)
			want = invalid("This request went stale on " + read.Request.DecidedAt.UTC().Format(decidedOn) + ": " + gone.why + ".")
			if status, _, raw := h.decide(t, deputy, requestID, "approve", ""); status != http.StatusBadRequest || !sameJSON(t, raw, want) {
				t.Fatalf("deputy's second approval: %d (%s), want 400 %s", status, raw, want)
			}
			h.requireUnchanged(t, closed, "approving a request closed as stale")
			if again := h.askToWaive(t, deputy, instanceID); again == requestID {
				t.Fatal("asking afresh answered the request that was closed")
			}
		})
	}
}

// A migration: the approval closes the request as stale, saying why and who
// found it, and no instance is moved.
func TestAMigrationIsNotApprovedOnceItsRequesterNoLongerAdministers(t *testing.T) {
	for _, gone := range departures() {
		t.Run(gone.name, func(t *testing.T) {
			h := newDeviationRouteHarness(t)
			deputy := h.signIn(t, "deputy", entities.RoleAdmin)
			boss, bossToken := h.enrol(t, "boss", gone.spec)
			m := h.twoVersionsToMigrate(t)
			instanceID := h.waitingAtTheApproval(t)
			requestID := h.askToMigrate(t, bossToken, m)
			gone.leave(t, h, boss)

			waited := h.everyRow(t)
			want := invalid(requesterGone)
			if status, _, raw := h.decide(t, deputy, requestID, "approve", decisionReason); status != http.StatusBadRequest || !sameJSON(t, raw, want) {
				t.Fatalf("deputy's approval of a migration whose requester no longer administers: %d (%s), want 400 %s", status, raw, want)
			}
			if changed := tablesChanged(waited, h.everyRow(t)); len(changed) != 1 || changed[0] != "deviation_requests" {
				t.Fatalf("the refused approval changed %v, want the request and nothing else", changed)
			}
			status, read, raw := h.readRequest(t, deputy, requestID)
			if status != http.StatusOK || read.Request.Status != "stale" || read.Request.DecidedBy != "" || read.Request.DecidedAt.IsZero() ||
				read.Request.Outcome["why"] != gone.why || read.Request.Outcome["attempted_by"] != "deputy" {
				t.Fatalf("after the 400 the request reads %d (%s); want it stale, decided by nobody, saying who tried and\n  %s", status, raw, gone.why)
			}
			if h.versionOf(t, instanceID) != m.v1 {
				t.Fatal("the instance was migrated on a request whose requester no longer administers")
			}
			// Somebody who administers asks afresh, and a second administrator
			// is then needed as for any request: the deputy's own is refused.
			if again := h.askToMigrate(t, deputy, m); again == requestID {
				t.Fatal("asking afresh answered the request that was closed")
			}
		})
	}
}

// Still an administrator, by another grant than the one they asked with: the
// request stands. What is asked is whether the requester administers the
// organization now, not whether anything about their account has changed.
func TestARequestStandsWhileItsRequesterStillAdministersByAnotherGrant(t *testing.T) {
	h := newDeviationRouteHarness(t)
	deputy := h.signIn(t, "deputy", entities.RoleAdmin)
	boss, bossToken := h.enrol(t, "boss", enrolled{global: []string{entities.RoleAdmin}, here: true})
	instanceID := h.oneStep(t)
	requestID := h.askToWaive(t, bossToken, instanceID)

	if err := h.svc.SetOrganizationRoles(h.asSystemHere(), boss, []string{entities.RoleAdmin}); err != nil {
		t.Fatalf("give the requester the administrator role in the organization: %v", err)
	}
	if err := h.svc.UpdateUser(system(), entities.User{ID: boss, Roles: []string{entities.RoleUser}}); err != nil {
		t.Fatalf("take the administrator role from the requester's account: %v", err)
	}
	status, approved, raw := h.decide(t, deputy, requestID, "approve", "")
	if status != http.StatusOK || !approved.Applied || approved.Request.Status != "applied" || h.stepIsOpen(t, instanceID) {
		t.Fatalf("deputy's approval: %d (%s), want the waive applied — the requester still administers this organization", status, raw)
	}
}

// Rejecting is not carrying out: an administrator rejects a request whose
// requester has gone, as they reject any other. The requester, no longer an
// administrator, cannot withdraw it — nor do anything else on the route.
func TestARequestWhoseRequesterNoLongerAdministersIsStillRejected(t *testing.T) {
	for _, gone := range departures() {
		t.Run(gone.name, func(t *testing.T) {
			h := newDeviationRouteHarness(t)
			deputy := h.signIn(t, "deputy", entities.RoleAdmin)
			boss, bossToken := h.enrol(t, "boss", gone.spec)
			instanceID := h.oneStep(t)
			requestID := h.askToWaive(t, bossToken, instanceID)
			gone.leave(t, h, boss)
			if gone.restart {
				h = h.restarted(t)
				deputy = h.login(t, "deputy")
			}

			before := h.everyRow(t)
			if status, _, raw := h.decideHere(t, bossToken, requestID, "reject", "no longer mine to ask"); status != gone.refused {
				t.Fatalf("the former administrator withdrawing their request: %d (%s), want %d", status, raw, gone.refused)
			}
			h.requireUnchanged(t, before, "a withdrawal by somebody who no longer administers")

			status, rejected, raw := h.decide(t, deputy, requestID, "reject", "the requester has left")
			if status != http.StatusOK || rejected.Request.Status != "rejected" || rejected.Request.DecidedBy != "deputy" ||
				rejected.Request.DecisionReason != "the requester has left" || rejected.Applied {
				t.Fatalf("deputy rejects: %d (%s), want it rejected by deputy", status, raw)
			}
			if !h.stepIsOpen(t, instanceID) || strings.Contains(raw, "no longer administers") {
				t.Fatalf("a rejection is told as one, and closes no step: %s", raw)
			}
		})
	}
}

// tablesChanged is the tables that hold something else in after than in
// before, in order.
func tablesChanged(before, after map[string]string) []string {
	var changed []string
	for table, rows := range after {
		if before[table] != rows {
			changed = append(changed, table)
		}
	}
	for table := range before {
		if _, still := after[table]; !still {
			changed = append(changed, table)
		}
	}
	slices.Sort(changed)
	return changed
}
