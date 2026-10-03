package impl

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/gsoultan/metis/internal/pkg/apierr"
	"github.com/gsoultan/metis/server/domains/entities"
)

// deviationNodeNameLength is how many characters of a step's name a ledger row
// keeps: the width of instance_deviations.node_name.
const deviationNodeNameLength = 255

// prepareDeviation is where a row that cannot be trusted is stopped, before the
// repository sees it: an unknown kind, scope or origin, a missing actor, a
// reason a kind requires and lacks, or one it forbids. It trims the words a
// person typed, keeps as much of a step's name as the ledger has room for,
// gives the row the ids it lacks, and names the signed-in account when that
// account is the actor.
func prepareDeviation(ctx context.Context, d entities.Deviation) (entities.Deviation, error) {
	d.Actor = strings.TrimSpace(d.Actor)
	d.Reason = strings.TrimSpace(d.Reason)
	if err := checkDeviation(d); err != nil {
		return entities.Deviation{}, err
	}
	d.Node = deviationNode(d.Node)
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

// checkDeviation stops a row an engine writer built wrongly. Such a mistake is
// a plain error, not apierr.Invalidf, for the reason the repository gives: it
// is never the client's, so it must surface as a server error that is logged,
// not as a 400 about something the caller did not do.
func checkDeviation(d entities.Deviation) error {
	switch {
	case !d.Kind.Valid():
		return fmt.Errorf("deviation: %q is not a kind of deviation the ledger records", d.Kind)
	case !d.Scope.Valid():
		return fmt.Errorf("deviation: %q is not a scope a deviation can have", d.Scope)
	case !d.Origin.Valid():
		return fmt.Errorf("deviation: %q is not a way a deviation can come about", d.Origin)
	case d.Status != entities.DeviationApplied && d.Status != entities.DeviationPendingApproval:
		return fmt.Errorf("deviation: a new deviation is applied or awaiting approval, not %q", d.Status)
	case d.Actor == "":
		return errors.New("deviation: the actor who did it is not named")
	case d.Project == nil || d.Project.ID == uuid.Nil:
		return errors.New("deviation: the project of its instance is not named")
	case d.Instance == nil || d.Instance.ID == uuid.Nil:
		return errors.New("deviation: the instance it was done to is not named")
	case d.Kind.ReasonForbidden() && d.Reason != "":
		return fmt.Errorf("deviation: a %s is the system's record and takes no reason", d.Kind)
	}
	return checkDeviationReason(d)
}

// checkDeviationReason judges the one thing a person typed and can correct: the
// reason. Only these refusals are the client's (400).
func checkDeviationReason(d entities.Deviation) error {
	switch {
	case d.Kind.ReasonRequired() && d.Reason == "":
		return apierr.Invalidf("say why: a reason is required for this change")
	case utf8.RuneCountInString(d.Reason) > entities.MaxDeviationReasonLength:
		return apierr.Invalidf("the reason is longer than %d characters", entities.MaxDeviationReasonLength)
	}
	return nil
}

// deviationNode is the step a row names, with as much of its name as the ledger
// keeps, cut between characters. A definition's author chooses the name and may
// make it longer than the column; it is there to be read, so one that does not
// fit is shortened rather than failing the change the row records, for every
// writer alike.
//
// Only the name. What identifies — the step's id, the iteration, the actor and
// whoever approved — is never cut: a shortened identifier names a different
// step or a different person. One too long for its column is a writer's
// mistake, and is left for the insert to refuse as a server error. The kind,
// scope, origin and status are closed sets checked above, and the reason has
// its own limit, which is the person's to correct.
//
// A node that needs cutting is replaced, not changed: the caller's own is the
// one its audit entry tells in full.
func deviationNode(node *entities.Node) *entities.Node {
	if node == nil || utf8.RuneCountInString(node.Name) <= deviationNodeNameLength {
		return node
	}
	return &entities.Node{ID: node.ID, Name: string([]rune(node.Name)[:deviationNodeNameLength])}
}
