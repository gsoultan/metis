package impl

import (
	"context"
	"strings"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/gsoultan/metis/internal/pkg/apierr"
	"github.com/gsoultan/metis/server/domains/entities"
)

// prepareDeviation is where a row that cannot be trusted is stopped, before the
// repository sees it: an unknown kind, scope or origin, a missing actor, a
// reason a kind requires and lacks, or one it forbids. It trims the words a
// person typed, gives the row the ids it lacks, and names the signed-in account
// when that account is the actor.
func prepareDeviation(ctx context.Context, d entities.Deviation) (entities.Deviation, error) {
	d.Actor = strings.TrimSpace(d.Actor)
	d.Reason = strings.TrimSpace(d.Reason)
	if err := checkDeviation(d); err != nil {
		return entities.Deviation{}, err
	}
	var err error
	if d.ID == uuid.Nil {
		if d.ID, err = uuid.NewV7(); err != nil {
			return entities.Deviation{}, err
		}
	}
	if d.RunID == uuid.Nil {
		if d.RunID, err = uuid.NewV7(); err != nil {
			return entities.Deviation{}, err
		}
	}
	if account := signedIn(ctx); d.ActorID == uuid.Nil && account != nil && account.Username == d.Actor {
		d.ActorID = account.ID
	}
	return d, nil
}

func checkDeviation(d entities.Deviation) error {
	switch {
	case !d.Kind.Valid():
		return apierr.Invalidf("%q is not a kind of deviation the ledger records", d.Kind)
	case !d.Scope.Valid():
		return apierr.Invalidf("%q is not a scope a deviation can have", d.Scope)
	case !d.Origin.Valid():
		return apierr.Invalidf("%q is not a way a deviation can come about", d.Origin)
	case d.Status != entities.DeviationApplied && d.Status != entities.DeviationPendingApproval:
		return apierr.Invalidf("a new deviation is applied or awaiting approval, not %q", d.Status)
	case d.Actor == "":
		return apierr.Invalidf("a deviation names who did it")
	case d.Project == nil || d.Project.ID == uuid.Nil:
		return apierr.Invalidf("a deviation names the project of its instance")
	case d.Instance == nil || d.Instance.ID == uuid.Nil:
		return apierr.Invalidf("a deviation names the instance it was done to")
	}
	return checkDeviationReason(d)
}

func checkDeviationReason(d entities.Deviation) error {
	switch {
	case d.Kind.ReasonRequired() && d.Reason == "":
		return apierr.Invalidf("say why: a %s needs a reason", d.Kind)
	case d.Kind.ReasonForbidden() && d.Reason != "":
		return apierr.Invalidf("a %s is the system's record and takes no reason", d.Kind)
	case utf8.RuneCountInString(d.Reason) > entities.MaxDeviationReasonLength:
		return apierr.Invalidf("the reason is longer than %d characters", entities.MaxDeviationReasonLength)
	}
	return nil
}
