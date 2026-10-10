package deviation_test

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/gsoultan/metis/internal/pkg/platformadmins"
	"github.com/gsoultan/metis/server/domains/entities"
	serviceimpl "github.com/gsoultan/metis/server/domains/services/impl"
)

// candidate is an account that may or may not be "another administrator" of
// the organization under test.
type candidate struct {
	name string
	spec enrolled
	// deleted says the account is deleted once it has signed in.
	deleted bool
	// platform says the account is named in METIS_PLATFORM_ADMINS: the test
	// names it there before the server is put together.
	platform bool
	// administers says whether the account administers the organization:
	// whether it could approve the request, and so whether it counts.
	administers bool
	// refused is what the approve route answers the account's own approval,
	// sent for the organization under test, when it does not administer it:
	// 401 for an account that is gone or does not belong, 403 for a member
	// without the role.
	refused int
}

// "Nobody else administers the organization" and "nobody else could approve
// this request" are one question. An account that can pass the approval's own
// checks for this organization counts as another administrator, and takes the
// choice away; an account that cannot does not, and its approval is refused.
// Each kind of account is asked both ways, through the approve route: the
// requester's own approval is admitted exactly when the candidate's is not.
//
// Each case has an organization of its own in one installation, every one of
// them named, so every case also has, beside its candidate, the administrators of every other
// case's organization — accounts that hold the role in every organization
// they belong to, and do not belong to this one.
func TestWhoCountsAsAnotherAdministratorIsWhoCouldApprove(t *testing.T) {
	admin, user := []string{entities.RoleAdmin}, []string{entities.RoleUser}
	named, namedOutsider := uuid.Must(uuid.NewV7()), uuid.Must(uuid.NewV7())
	cases := []candidate{
		{name: "an administrator here, by a role held on the account",
			spec: enrolled{global: admin, here: true}, administers: true},
		{name: "an administrator here, by a role held in this organization alone",
			spec: enrolled{here: true, rolesHere: admin}, administers: true},
		{name: "an administrator here whose role is written in lower case",
			spec: enrolled{global: []string{"admin"}, here: true}, administers: true},
		{name: "an administrator here and of another organization",
			spec: enrolled{global: admin, here: true, elsewhere: true}, administers: true},
		{name: "an administrator here by a role held here, who is a plain member elsewhere",
			spec: enrolled{here: true, rolesHere: admin, elsewhere: true}, administers: true},
		{name: "a member who holds no administrator role",
			spec: enrolled{global: user, here: true}, refused: http.StatusForbidden},
		{name: "an operator and designer here",
			spec: enrolled{here: true, rolesHere: []string{entities.RoleOperator, entities.RoleDesigner}}, refused: http.StatusForbidden},
		{name: "an administrator of another organization only, by a role held on the account",
			spec: enrolled{global: admin, elsewhere: true}, refused: http.StatusUnauthorized},
		{name: "a member here who administers another organization, by a role held there",
			spec: enrolled{here: true, elsewhere: true, rolesElsewhere: admin}, refused: http.StatusForbidden},
		{name: "an account whose role is ADMINISTRATOR, which is no role",
			spec: enrolled{global: []string{"ADMINISTRATOR"}, here: true}, refused: http.StatusForbidden},
		{name: "an administrator here whose account was deleted, role held on the account",
			spec: enrolled{global: admin, here: true}, deleted: true, refused: http.StatusUnauthorized},
		{name: "an administrator here whose account was deleted, role held here",
			spec: enrolled{here: true, rolesHere: admin}, deleted: true, refused: http.StatusUnauthorized},
		{name: "a platform administrator who is a plain member here",
			spec: enrolled{id: named, global: user, here: true}, platform: true, refused: http.StatusForbidden},
		{name: "a platform administrator who does not belong here",
			spec: enrolled{id: namedOutsider, global: admin, elsewhere: true}, platform: true, refused: http.StatusUnauthorized},
	}
	// The list is read when the server is put together.
	var listed []string
	for _, c := range cases {
		if c.platform {
			listed = append(listed, c.spec.id.String())
		}
	}
	if len(listed) != 2 {
		t.Fatalf("%d platform administrators are named, want the two cases", len(listed))
	}
	t.Setenv(platformadmins.Env, strings.Join(listed, ","))
	// Every case's organization is made, then named, then the server is
	// started again: the list of them is read when it starts.
	base := newDeviationRouteHarness(t)
	organizations, ids := make([]*deviationHarness, len(cases)), make([]string, len(cases))
	for i := range cases {
		organizations[i] = base.inAnotherOrganization(t, fmt.Sprintf("Organization %d", i))
		ids[i] = organizations[i].orgID.String()
	}
	t.Setenv(serviceimpl.EnvSoleAdministratorOrganizations, strings.Join(ids, ","))
	installation := base.restarted(t)

	for i, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			h := installation.at(organizations[i])
			boss := h.signIn(t, fmt.Sprintf("boss-%d", i), entities.RoleAdmin)
			id, token := h.enrol(t, fmt.Sprintf("candidate-%d", i), c.spec)
			if c.deleted {
				h.remove(t, id)
			}
			instanceID := h.oneStep(t)
			requestID := h.askToWaive(t, boss, instanceID)

			if c.administers {
				// The requester first: refused, because the candidate could.
				want := refusal("forbidden", anotherMustApprove)
				if status, _, raw := h.decideHere(t, boss, requestID, "approve", decisionReason); status != http.StatusForbidden || !sameJSON(t, raw, want) {
					t.Fatalf("the requester's own approval: %d (%s), want 403 %s — the candidate administers this organization", status, raw, want)
				}
				status, approved, raw := h.decideHere(t, token, requestID, "approve", "")
				if status != http.StatusOK || !approved.Applied || approved.Request.SelfApproved {
					t.Fatalf("the candidate's approval: %d (%s), want it applied — they were counted as another administrator", status, raw)
				}
				return
			}
			// The candidate first: refused, whichever organization the
			// request is sent for, and nothing changes.
			before := h.everyRow(t)
			if status, _, raw := h.decideHere(t, token, requestID, "approve", decisionReason); status != c.refused {
				t.Fatalf("the candidate's approval, sent for this organization: %d (%s), want %d", status, raw, c.refused)
			}
			if status, _, raw := h.decide(t, token, requestID, "approve", decisionReason); status == http.StatusOK ||
				(status != http.StatusUnauthorized && status != http.StatusForbidden && status != http.StatusNotFound) {
				t.Fatalf("the candidate's approval, sent for the organization they sign in to: %d (%s), want it refused", status, raw)
			}
			h.requireUnchanged(t, before, "the candidate's refused approval")
			status, approved, raw := h.decideHere(t, boss, requestID, "approve", decisionReason)
			if status != http.StatusOK || !approved.Applied || !approved.Request.SelfApproved {
				t.Fatalf("the requester's own approval: %d (%s), want it applied and self-approved — the candidate could not approve", status, raw)
			}
		})
	}
}

// An account an identity provider signs in belongs where its latest sign-in
// put it: the organizations that sign-in's claim named, written as its
// memberships before the request goes on. So it is counted, and can approve,
// in exactly those — an administrator here while the provider names this
// organization for it, and nobody here once a sign-in no longer does.
//
// The tests' server signs local accounts in, not a provider's, so whether
// such an account's own approval would be let through is asked of the
// principal its sign-in answers: the organizations a request of its can be
// for, and the role it holds there — what the tenant resolver and the role
// gate read.
func TestAnAccountAnIdentityProviderPlacesIsCountedWhereItsLatestSignInPutIt(t *testing.T) {
	h := withOrganizationNamed(t)
	elsewhere := h.inAnotherOrganization(t, "Elsewhere")
	instanceID := h.oneStep(t)
	boss := h.signIn(t, "boss", entities.RoleAdmin)
	requestID := h.askToWaive(t, boss, instanceID)

	signedIn := func(organizations ...uuid.UUID) entities.User {
		t.Helper()
		claims := entities.IdentityClaims{Issuer: "https://idp.example", Subject: "sub-dita", Username: "dita",
			OrganizationClaim: "orgs", HasOrganizationClaim: true}
		for _, organization := range organizations {
			claims.Organizations = append(claims.Organizations, organization.String())
		}
		account, err := h.svc.SignInThroughIdentityProvider(context.Background(), claims)
		if err != nil {
			t.Fatalf("sign dita in through the provider: %v", err)
		}
		return account
	}
	couldApproveHere := func(account entities.User) bool {
		belongs := false
		for _, organization := range account.Organizations {
			belongs = belongs || (organization != nil && organization.ID == h.orgID)
		}
		return belongs && account.HoldsRoleIn(h.orgID, entities.RoleAdmin)
	}

	// Placed here, and made an administrator here by an administrator.
	dita := signedIn(h.orgID, elsewhere.orgID)
	there := entities.WithTenantContext(entities.WithSystemContext(context.Background()), entities.TenantContext{TenantID: h.orgID.String()})
	if err := h.svc.SetOrganizationRoles(there, dita.ID, []string{entities.RoleAdmin}); err != nil {
		t.Fatalf("make dita an administrator here: %v", err)
	}
	if dita = signedIn(h.orgID, elsewhere.orgID); !couldApproveHere(dita) {
		t.Fatalf("dita, placed here and made an administrator, could not approve here: %+v", dita)
	}
	want := refusal("forbidden", anotherMustApprove)
	if status, _, raw := h.decide(t, boss, requestID, "approve", decisionReason); status != http.StatusForbidden || !sameJSON(t, raw, want) {
		t.Fatalf("boss approving his own request while dita administers: %d (%s), want 403 %s", status, raw, want)
	}

	// The provider stops naming this organization: dita's next sign-in takes
	// her out of it, and the role she held here goes with the membership.
	if dita = signedIn(elsewhere.orgID); couldApproveHere(dita) {
		t.Fatalf("dita, no longer placed here, could still approve here: %+v", dita)
	}
	if status, approved, raw := h.decide(t, boss, requestID, "approve", decisionReason); status != http.StatusOK || !approved.Request.SelfApproved {
		t.Fatalf("boss approving his own request once dita is gone: %d (%s), want it applied and self-approved", status, raw)
	}
}

// Who administers the organization is asked when the approval is made, of
// the accounts as they then are — not when the request was made.
func TestTheAdministratorsAreCountedWhenTheApprovalIsMade(t *testing.T) {
	t.Run("an administrator appointed while it waited takes the choice away", func(t *testing.T) {
		h := withOrganizationNamed(t)
		instanceID := h.oneStep(t)
		boss := h.signIn(t, "boss", entities.RoleAdmin)
		clerk, _ := h.enrol(t, "clerk", enrolled{global: []string{entities.RoleUser}, here: true})
		requestID := h.askToWaive(t, boss, instanceID)

		there := entities.WithTenantContext(entities.WithSystemContext(context.Background()), entities.TenantContext{TenantID: h.orgID.String()})
		if err := h.svc.SetOrganizationRoles(there, clerk, []string{entities.RoleAdmin}); err != nil {
			t.Fatalf("make the clerk an administrator: %v", err)
		}
		want := refusal("forbidden", anotherMustApprove)
		if status, _, raw := h.decide(t, boss, requestID, "approve", decisionReason); status != http.StatusForbidden || !sameJSON(t, raw, want) {
			t.Fatalf("self-approval with a second administrator now present: %d (%s), want 403 %s", status, raw, want)
		}
		if h.requestStatus(t, requestID) != "pending_approval" || !h.stepIsOpen(t, instanceID) {
			t.Fatal("the refused self-approval changed something")
		}
	})

	t.Run("the only other administrator gone, the requester may approve", func(t *testing.T) {
		h := withOrganizationNamed(t)
		instanceID := h.oneStep(t)
		boss := h.signIn(t, "boss", entities.RoleAdmin)
		former, _ := h.enrol(t, "former", enrolled{here: true, rolesHere: []string{entities.RoleAdmin}})
		requestID := h.askToWaive(t, boss, instanceID)
		if status, _, raw := h.decide(t, boss, requestID, "approve", decisionReason); status != http.StatusForbidden {
			t.Fatalf("self-approval while the other administrator is there: %d (%s), want 403", status, raw)
		}
		there := entities.WithTenantContext(entities.WithSystemContext(context.Background()), entities.TenantContext{TenantID: h.orgID.String()})
		if err := h.svc.SetOrganizationRoles(there, former, []string{entities.RoleOperator}); err != nil {
			t.Fatalf("take the administrator role from the other administrator: %v", err)
		}
		if status, approved, raw := h.decide(t, boss, requestID, "approve", decisionReason); status != http.StatusOK || !approved.Request.SelfApproved {
			t.Fatalf("self-approval once nobody else administers: %d (%s), want it applied and self-approved", status, raw)
		}
	})

	// The count is made by the approval, after it holds the request — not
	// before it waits for it. An approval that waits behind somebody else
	// holding the request, while a second administrator is appointed, finds
	// that administrator when its turn comes.
	t.Run("an administrator appointed while the approval waited for the request is counted", func(t *testing.T) {
		h := withOrganizationNamed(t)
		instanceID := h.oneStep(t)
		boss := h.signIn(t, "boss", entities.RoleAdmin)
		clerk, _ := h.enrol(t, "clerk", enrolled{global: []string{entities.RoleUser}, here: true})
		requestID := h.askToWaive(t, boss, instanceID)

		holder := h.holdOpen(t, `SELECT 1 FROM deviation_requests WHERE id = ? FOR UPDATE`, requestID)
		var got posted
		finished := make(chan error, 1)
		go func() {
			var err error
			got, err = h.post(boss, requestPath(requestID)+"/approve", `{"reason":"`+decisionReason+`"}`)
			finished <- err
		}()
		holder.untilBehind(finished)
		there := entities.WithTenantContext(entities.WithSystemContext(context.Background()), entities.TenantContext{TenantID: h.orgID.String()})
		if err := h.svc.SetOrganizationRoles(there, clerk, []string{entities.RoleAdmin}); err != nil {
			t.Fatalf("make the clerk an administrator: %v", err)
		}
		holder.undo()
		if err := answerOf(t, finished, "the approval that waited"); err != nil {
			t.Fatalf("the approval that waited: %v", err)
		}
		want := refusal("forbidden", anotherMustApprove)
		if got.status != http.StatusForbidden || !sameJSON(t, got.raw, want) {
			t.Fatalf("the approval that waited while a second administrator was appointed: %d (%s), want 403 %s", got.status, got.raw, want)
		}
		if h.requestStatus(t, requestID) != "pending_approval" || !h.stepIsOpen(t, instanceID) {
			t.Fatal("the refused self-approval changed something")
		}
	})

	// What is left of the race, and which way it fails. A grant that has been
	// made and not yet committed is nobody's role yet: the account it is for
	// cannot approve, and the count does not see it. So an approval made at
	// that moment is admitted, and the second administrator appears just
	// after it. The other order — the grant committed, then the approval —
	// is the first subtest, and is refused.
	t.Run("a grant not yet committed is not yet an administrator", func(t *testing.T) {
		h := withOrganizationNamed(t)
		instanceID := h.oneStep(t)
		boss := h.signIn(t, "boss", entities.RoleAdmin)
		clerk, clerkToken := h.enrol(t, "clerk", enrolled{global: []string{entities.RoleUser}, here: true})
		requestID := h.askToWaive(t, boss, instanceID)

		granting := h.holdOpen(t, `UPDATE user_organizations SET roles = '["ADMIN"]' WHERE user_id = ? AND organization_id = ?`, clerk, h.orgID)
		if status, _, raw := h.decide(t, clerkToken, requestID, "approve", ""); status != http.StatusForbidden {
			t.Fatalf("the clerk approving on a grant not yet committed: %d (%s), want 403", status, raw)
		}
		status, approved, raw := h.decide(t, boss, requestID, "approve", decisionReason)
		if status != http.StatusOK || !approved.Request.SelfApproved {
			t.Fatalf("self-approval while the grant is not committed: %d (%s), want it applied and self-approved", status, raw)
		}
		if err := granting.commit(); err != nil {
			t.Fatalf("commit the grant: %v", err)
		}
	})
}
