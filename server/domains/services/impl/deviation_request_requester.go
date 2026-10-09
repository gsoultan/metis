package impl

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"

	"github.com/gsoultan/metis/internal/pkg/apierr"
	"github.com/gsoultan/metis/server/domains/adapters"
	"github.com/gsoultan/metis/server/domains/entities"
)

// The three ways the account that asked for a request can have stopped
// administering the request's organization, as the request then records them.
const (
	requesterDeleted = "their account has been deleted"
	requesterLeft    = "their account no longer belongs to it"
	requesterDemoted = "their account no longer holds the administrator role in it"
)

// administers reports whether account holds the administrator role in
// organization — on the account, or in that organization alone. It is the one
// test of the role this control makes: of whoever asks or decides
// (requireDecidingAdministrator), and of whoever asked, when somebody else
// comes to approve (requesterNoLongerAdministers). No organization is
// administered by nobody.
//
// Four places have to agree on what "administers this organization" means,
// and this is two of them: the service's check of whoever asks or decides,
// and its check of whoever asked. The other two stay where they are and are
// kept in step with it by hand and by test: the routes' gate (adminOnly, in
// server/endpoints), which reads the same roles of the same principal before
// a request reaches a service; and the count of other administrators
// (pg.userRepository.HasAnotherAdministrator), which asks the database with
// the same comparison of roles (entities.HasRole) and adds, in SQL, what is
// asked beside this function of an account read from the directory — not
// deleted, and a member. TestWhoCountsAsAnotherAdministratorIsWhoCouldApprove
// (tests/deviation) holds the count to what the gate and this let through.
//
// It does not ask whether the account belongs to the organization. For a
// signed-in caller the tenant resolver has: a request is only ever for an
// organization its account is in. For an account read from the directory the
// caller asks it as well (belongsTo).
func administers(account entities.User, organization uuid.UUID) bool {
	return organization != uuid.Nil && account.HoldsRoleIn(organization, entities.RoleAdmin)
}

// requesterNoLongerAdministers answers why the account that asked for request
// no longer administers the organization the request is for — or nothing,
// while it still does.
//
// A second administrator carries out what an administrator asked for. Somebody
// who has since been removed could no longer ask, and an approval would then
// be carrying out a request on nobody's standing but the approver's — one
// person's call, which is what this control exists to prevent. So the
// requester is asked about at the approval, as the accounts are when it is
// made: the account is still there (a deleted one is not read), still belongs
// to the organization, and still holds the administrator role there — by
// administers, the test the approver's own role passed. How the role is held
// may have changed; only whether it is held is asked.
//
// It is one read of the account as committed when it is made, after the
// approval has taken the request's row, as the count of other administrators
// is (anotherAdministrator): nothing holds the accounts still while the
// approval finishes. A role taken away, and committed, before this read is
// always seen.
//
// A read that fails is that failure, and the request waits: it says nothing
// about the requester. Only an account that is not there is an answer.
func (r approvalRules) requesterNoLongerAdministers(ctx context.Context, request entities.DeviationRequest) (string, error) {
	organization, err := r.organizationOf(ctx, request)
	if err != nil {
		return "", err
	}
	account, err := r.repo.User().Get(ctx, request.RequestedByID)
	if errors.Is(err, apierr.ErrNotFound) {
		return requesterGoneBecause(request, requesterDeleted), nil
	}
	if err != nil {
		return "", fmt.Errorf("reading the account that asked for request %s: %w", request.ID, err)
	}
	switch {
	case !belongsTo(account, organization):
		return requesterGoneBecause(request, requesterLeft), nil
	case !administers(adapters.UserEntityAdapter{Model: account}.ToEntity(), organization):
		return requesterGoneBecause(request, requesterDemoted), nil
	}
	return "", nil
}

// requesterGoneBecause is why a request is stale when whoever asked for it no
// longer administers its organization: the words its record keeps.
func requesterGoneBecause(request entities.DeviationRequest, how string) string {
	return fmt.Sprintf("%s, who asked for it, no longer administers this organization: %s", request.RequestedBy, how)
}

// errRequesterGone is what the approver is told of a request closed because
// whoever asked for it no longer administers its organization. It names what
// there is to do, which is not what a request the instance left behind is
// told: nothing needs previewing again — somebody who administers has to ask.
func errRequesterGone(request entities.DeviationRequest) error {
	return apierr.Invalidf("The administrator who asked for this, %s, no longer administers this organization, "+
		"so nothing was applied. It has to be asked for afresh by somebody who does.", request.RequestedBy)
}

// errExpiredBeforeApproval is what the approver is told of a request found
// past its deadline: one sentence, for a waive's approval and a migration's.
// The expiry is recorded before it is said (refuseAfterCommit).
func errExpiredBeforeApproval(request entities.DeviationRequest) error {
	return apierr.Invalidf("This request expired on %s before anybody approved it, so nothing was applied. Ask again if it is still needed.",
		request.ExpiresAt.UTC().Format(decidedOnLayout))
}
