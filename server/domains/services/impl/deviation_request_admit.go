package impl

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"

	"github.com/gsoultan/metis/internal/pkg/apierr"
	"github.com/gsoultan/metis/server/domains/entities"
	"github.com/gsoultan/metis/server/repositories"
	repocontracts "github.com/gsoultan/metis/server/repositories/contracts"
)

// decidedOnLayout is how a refusal writes the moment something was decided:
// a date somebody reads, in UTC so that it is one moment for every reader.
const decidedOnLayout = "2 January 2006 15:04 MST"

// requireDecidingAdministrator answers the account that is asking for, or
// deciding, a request for a second administrator, and refuses anybody but a
// signed-in administrator of the organization the request is for who has an
// account id.
//
// The routes are administrators' only as well. The check is made again here
// because the service is what acts (AGENTS.md §2.3, a fast path that skips a
// check).
//
// The account id is what the whole control rests on: the requester and the
// approver are told apart by it, not by name, so a caller with none cannot be
// shown to be somebody else — and absent means refuse. A request that is for
// no organization is refused for the reason an in-place command is
// (requireDeviationAdministrator): nothing would narrow what it reads.
func requireDecidingAdministrator(ctx context.Context) (entities.User, error) {
	caller := signedIn(ctx)
	organization := entities.ActingOrganization(ctx)
	if caller == nil || caller.Username == "" || !administers(*caller, organization) {
		return entities.User{}, apierr.Forbiddenf("only an administrator can ask for or decide a request for a second administrator")
	}
	if caller.ID == uuid.Nil {
		return entities.User{}, errNoAccountToDecideWith()
	}
	return *caller, nil
}

// errNoAccountToDecideWith refuses a caller that carries no account id.
func errNoAccountToDecideWith() error {
	return apierr.Forbiddenf("asking for and giving a second administrator's approval needs an account, and this request carries none")
}

// approvalRules decides who may approve a request. It is one value so that
// the in-place approval and the migration's ask the same rule, and so that
// the one setting that relaxes it is held in one place.
type approvalRules struct {
	repo repositories.Repository
	// sole holds the organizations whose only administrator may approve a
	// request they asked for themselves (EnvSoleAdministratorOrganizations).
	// Empty unless whoever built the service named some: nothing here reads
	// the environment, and nothing a request carries reaches it.
	sole map[uuid.UUID]struct{}
}

// allowsSoleAdministratorOf reports whether organization is one whose only
// administrator may approve their own request. No organization is never one.
func (r approvalRules) allowsSoleAdministratorOf(organization uuid.UUID) bool {
	_, named := r.sole[organization]
	return named && organization != uuid.Nil
}

// naming is the rules with exactly organizations as the ones whose only
// administrator may approve their own request. The list is copied: what the
// caller does with its own afterwards names nobody. The nil id is left out.
func (r approvalRules) naming(organizations []uuid.UUID) approvalRules {
	r.sole = make(map[uuid.UUID]struct{}, len(organizations))
	for _, organization := range organizations {
		if organization != uuid.Nil {
			r.sole[organization] = struct{}{}
		}
	}
	return r
}

// admit answers the decision caller may make on request, or why not: a
// request that is no longer waiting says what became of it, and then who may
// decide is asked (admitDecider).
//
// Two things are asked of the request's organization, and of nothing else:
// whether it is one whose only administrator may approve their own request,
// and whether anybody else administers it. The organization is the one the
// request's project belongs to, read once (organizationOf) — never one a
// caller says, and refused when it is not the one the request is being
// decided in: the repository answers "nobody else" for an organization
// outside the caller's scope, which here would read as leave to approve
// alone.
//
// A self-approval's decision names that organization: it is what the
// exception rested on, and the record of the approval says it. And when the
// requester is refused in a named organization because somebody else
// administers it, that is said in the server's log — the list names an
// organization it should no longer name — from the lookup's own answer, with
// nothing asked a second time.
//
// Both are asked only when the caller is the requester. Whether anybody else
// administers the organization is asked whether or not the organization is
// named, because the two refusals differ: with somebody else to ask, the
// requester is told to ask them; with nobody, that the request waits. Only
// the second is ever relaxed, and only for a named organization.
//
// request is the row its caller holds — locked, by an approval — and ctx the
// unit of work that holds it; caller is who requireDecidingAdministrator
// answered.
func (r approvalRules) admit(ctx context.Context, request entities.DeviationRequest, caller entities.User, reason string) (entities.DeviationDecision, error) {
	if request.Status != entities.DeviationRequestPending {
		return entities.DeviationDecision{}, decidedRefusal(request)
	}
	organization := sync.OnceValues(func() (uuid.UUID, error) { return r.organizationOf(ctx, request) })
	allowSole := false
	if len(r.sole) > 0 && caller.ID != uuid.Nil && caller.ID == request.RequestedByID {
		// An organization that could not be told is not a named one. Why it
		// could not is said below, where the same answer is asked for again.
		if id, err := organization(); err == nil {
			allowSole = r.allowsSoleAdministratorOf(id)
		}
	}
	found := false
	decision, err := admitDecider(caller, request, reason, allowSole, func() (bool, error) {
		id, err := organization()
		if err != nil {
			return false, err
		}
		another, err := r.anotherAdministrator(ctx, id, caller)
		found = another
		return another, err
	})
	// The organization has been read by now in both cases below, and is not
	// read again; an approval by somebody else never reads it at all.
	if allowSole && found {
		// Named as having one administrator, and it has another: the
		// requester was refused, and the list is out of date.
		if id, told := organization(); told == nil {
			traceNamedOrganizationHasAnotherAdministrator(request, id)
		}
	}
	if err == nil && decision.SelfApproved {
		// A self-approval that cannot say which organization it rested on
		// is not made: the record would name none.
		id, told := organization()
		if told != nil {
			return entities.DeviationDecision{}, told
		}
		decision.Organization = id
	}
	return decision, err
}

// organizationOf is the organization request belongs to: its project's. It
// refuses a request that is not for the organization it is being decided in.
func (r approvalRules) organizationOf(ctx context.Context, request entities.DeviationRequest) (uuid.UUID, error) {
	if request.Project == nil {
		return uuid.Nil, fmt.Errorf("request %s names no project, so its organization cannot be told", request.ID)
	}
	project, err := r.repo.Project().Get(ctx, request.Project.ID)
	if errors.Is(err, apierr.ErrNotFound) {
		// The request was found, so "not found" is not an answer about it:
		// the cause is kept as words, as graphRunBy keeps a missing version.
		return uuid.Nil, fmt.Errorf("reading the project of request %s: it is not there (%s)", request.ID, err.Error())
	}
	if err != nil {
		return uuid.Nil, fmt.Errorf("reading the project of request %s: %w", request.ID, err)
	}
	organization := uuid.UUID(project.OrganizationID)
	if organization == uuid.Nil || organization != entities.ActingOrganization(ctx) {
		return uuid.Nil, apierr.Forbiddenf("this request belongs to another organization")
	}
	return organization, nil
}

// anotherAdministrator reports whether somebody other than caller
// administers organization, which organizationOf answered.
//
// "Administers" is what the approval's own checks ask of whoever approves
// (the routes' role gate, and requireDecidingAdministrator): an account that
// is not deleted, belongs to the organization, and holds the administrator
// role on the account or in that organization alone — compared by
// entities.HasRole, the comparison those checks make. So somebody counts
// here exactly when their own approval of this request would be let through.
// The administrator role held in every organization by an account that does
// not belong to this one does not count: that account cannot act here. Nor
// does being named a platform administrator, which grants no role.
//
// It is one read of the accounts as committed when it is made, after the
// approval has taken the request's row: on the approval's own transaction
// when the request is for the main database, and on the main database's pool
// when it is for an environment's, where the accounts are not. Nothing holds
// the accounts still while the approval finishes, so an administrator
// appointed a moment later is not seen — the approval is then one made just
// before a second administrator appeared, and says "nobody else" of the
// moment it asked. One appointed, and committed, before this read is always
// seen.
func (r approvalRules) anotherAdministrator(ctx context.Context, organization uuid.UUID, caller entities.User) (bool, error) {
	orgCtx := entities.WithTenantContext(ctx, entities.TenantContext{TenantID: organization.String()})
	another, err := r.repo.User().HasAnotherAdministrator(orgCtx, organization, caller.ID)
	if err != nil {
		return false, fmt.Errorf("could not count the administrators of organization %s: %w", organization, err)
	}
	return another, nil
}

// admitDecider is the rule for who may approve a request, with nothing read:
// the caller, the request, the reason they gave, whether this installation
// lets a sole administrator approve their own request, and a way to ask
// whether anybody else administers the organization.
//
// Somebody other than the requester — by account id, so that renaming an
// account does not make it another — may approve, and their reason is a note:
// optional, kept without the spaces around it, no longer than the ledger
// keeps a reason.
//
// The requester may not, with one exception an installation has to switch on
// (allowSole): when nobody else administers the organization, and they say
// why. anotherAdministrator is asked only then, and a failure to answer it is
// returned as it is — never read as "nobody else".
//
// A request that names no requester's account has no second administrator
// anybody could be shown to be. Nothing writes one; refused as the server's.
func admitDecider(
	caller entities.User,
	request entities.DeviationRequest,
	reason string,
	allowSole bool,
	anotherAdministrator func() (bool, error),
) (entities.DeviationDecision, error) {
	var none entities.DeviationDecision
	if caller.ID == uuid.Nil {
		return none, errNoAccountToDecideWith()
	}
	if request.RequestedByID == uuid.Nil {
		return none, fmt.Errorf("request %s names no account that asked for it, so nobody can be shown to be a second administrator", request.ID)
	}
	decision := entities.DeviationDecision{Decider: caller.Username, DeciderID: caller.ID, Reason: strings.TrimSpace(reason), At: time.Now()}
	tooLong := utf8.RuneCountInString(decision.Reason) > entities.MaxDeviationReasonLength
	if caller.ID != request.RequestedByID {
		if tooLong {
			return none, errDecisionReasonTooLong()
		}
		return decision, nil
	}

	another, err := anotherAdministrator()
	if errors.Is(err, apierr.ErrForbidden) {
		// A refusal is said in its own words, not as a failure to ask.
		return none, err
	}
	if err != nil {
		return none, fmt.Errorf("asking whether anybody else administers the organization: %w", err)
	}
	switch {
	case another:
		return none, apierr.Forbiddenf("You asked for this. A different administrator has to approve it.")
	case !allowSole:
		return none, apierr.Forbiddenf("You asked for this, and nobody else administers this organization, so it waits. " +
			"Make another account an administrator so they can approve it, reject it yourself, or let it expire. " +
			"Whoever operates this installation can name this organization as one that has a single administrator, " +
			"whose own approval is then accepted; until they do, the request waits: " +
			"see \"A second administrator approves waivers and skips\" in docs/upgrading.md.")
	case decision.Reason == "":
		return none, apierr.Invalidf("Say why you are approving your own request: with nobody else to approve it, the reason is the record.")
	case tooLong:
		return none, errDecisionReasonTooLong()
	}
	decision.SelfApproved = true
	return decision, nil
}

// errDecisionReasonTooLong refuses a decision's reason the ledger would not
// keep whole.
func errDecisionReasonTooLong() error {
	return apierr.Invalidf("The reason is longer than %d characters; say it more briefly.", entities.MaxDeviationReasonLength)
}

// decidedRefusal is what somebody who tries to decide a request is told when
// it has been decided already: what became of it, by whom and when. It is a
// state they met, not a mistake of the server's — and there is no class for a
// conflict, so it is an invalid argument, as "already waived by …" is.
//
// An expiry is dated by the deadline, which is when it happened whenever it
// was written down. A stale request names nobody — the instance made it
// stale — and is dated by when that was found.
//
// Asked of a request that still waits, it is the caller's mistake and a plain
// error: nothing was decided, and saying so to a client would be untrue.
func decidedRefusal(request entities.DeviationRequest) error {
	return decidedRefusalAt(request, time.Now())
}

// decidedRefusalAt is decidedRefusal at a given moment. The moment matters to
// one answer only: a request stored as approved is being applied while its
// run window is open, and is not once it has closed — its run never reported,
// or never started — whether or not anything has written that down.
func decidedRefusalAt(request entities.DeviationRequest, now time.Time) error {
	on := decidedOn(request).UTC().Format(decidedOnLayout)
	// "X approved this" alone reads as a second administrator's approval, so
	// a request its own requester approved says that it was.
	approved := fmt.Sprintf("%s approved this on %s", request.DecidedBy, on)
	if request.SelfApproved() {
		approved = fmt.Sprintf("%s approved this — their own request, with no second administrator — on %s", request.DecidedBy, on)
	}
	switch request.Status {
	case entities.DeviationRequestApproved:
		if request.RunWindowClosed(now) {
			return apierr.Invalidf("%s, and no run of it reported back in the time one is given; it is no longer in use. Ask again.", approved)
		}
		return apierr.Invalidf("%s; it is being applied.", approved)
	case entities.DeviationRequestApplied:
		// A run that ended well and changed nothing says so: "it was
		// applied" would read as something having been done.
		if note, said := request.Outcome[outcomeNote].(string); said && note != "" {
			return apierr.Invalidf("%s, and its run changed nothing: %s. Ask again for what remains.", approved, note)
		}
		return apierr.Invalidf("%s, and it was applied.", approved)
	case entities.DeviationRequestInterrupted:
		return apierr.Invalidf("%s, and the run stopped part-way: %v. Ask again for what remains.", approved, request.Outcome["error"])
	case entities.DeviationRequestRejected:
		return apierr.Invalidf("%s rejected this on %s.", request.DecidedBy, on)
	case entities.DeviationRequestExpired:
		return apierr.Invalidf("This request expired on %s.", request.ExpiresAt.UTC().Format(decidedOnLayout))
	case entities.DeviationRequestStale:
		return apierr.Invalidf("This request went stale on %s: what it asked for no longer held.", on)
	}
	return fmt.Errorf("request %s is %q, which is not a decision anybody made", request.ID, request.Status)
}

// decidedOn is when a request was decided: the time its decision carries, or
// — for one closed with no time of its own — when its row was last written.
func decidedOn(request entities.DeviationRequest) time.Time {
	if request.DecidedAt != nil {
		return *request.DecidedAt
	}
	return request.UpdatedAt
}

// decidedFirst turns the repositories' "somebody decided this first" into
// what whoever lost is told: the request as it now reads, through
// decidedRefusal. Any other error, and none, are returned as they are.
//
// A decision holds its request's row before it looks at the request, so two
// deciders meet there and the second reads what the first left — it never
// gets this far. The repositories refuse a second decision all the same
// (ErrDeviationRequestDecided, ErrDeviationRowDecided), and should a path
// ever decide without the row, that refusal is still a request somebody else
// decided, not the server's failure. reread reads the request again; when it
// cannot, that failure is the answer.
func decidedFirst(err error, reread func() (entities.DeviationRequest, error)) error {
	if !errors.Is(err, repocontracts.ErrDeviationRequestDecided) && !errors.Is(err, repocontracts.ErrDeviationRowDecided) {
		return err
	}
	request, readErr := reread()
	if readErr != nil {
		return fmt.Errorf("reading a request somebody else decided first: %w", readErr)
	}
	return decidedRefusal(request)
}
