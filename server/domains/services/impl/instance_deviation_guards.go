package impl

import (
	"strings"

	"github.com/gsoultan/metis/internal/pkg/apierr"
	"github.com/gsoultan/metis/server/domains/entities"
	"github.com/gsoultan/metis/server/repositories/models"
)

// errMovedSincePreview is the refusal of an apply made for work that is no
// longer what its preview showed. It is the caller's to fix, by previewing
// again: there is no class for a conflict, so it is an invalid argument, as
// the engine's own "this step has already finished" is.
func errMovedSincePreview() error {
	return apierr.Invalidf("this instance has moved since you previewed it; preview again")
}

// refuseEnded refuses an act on an instance that is no longer running, from
// the row the apply locked.
//
// The plan refuses this too. It is asked here as well, of the row itself and
// before anything else is read, so that it holds however the plan's own
// refusal is one day worded or relaxed.
//
// A suspended instance has not ended, so it is not said that it can no longer
// be acted on. Nor is it told to resume it first: no route, service or handler
// suspends an instance or resumes one, and a refusal does not tell somebody to
// do what the product cannot. It is said what the instance is and that it is
// refused, in one sentence for all three kinds.
func refuseEnded(locked models.ProcessInstanceModel, kind entities.DeviationKind) error {
	switch locked.Status {
	case models.ProcessActive:
		return nil
	case models.ProcessSuspended:
		return apierr.Invalidf("this instance is suspended, and a suspended instance is not waived, cancelled or held in place")
	}
	return apierr.Invalidf("this instance is %s, so it can no longer be %s; preview again", locked.Status, pastTense(kind))
}

// refuseUnlessAsPreviewed refuses an apply unless the plan made from the
// locked row is the one that was previewed and nothing refuses it, and the
// locked row still waits where the command acts.
//
// The key first: when the work has changed, that is what the caller needs to
// hear, whatever else the new plan would say. Then the plan's own refusals,
// which are what a preview of the same request shows. The last question
// repeats what the key covers and what the plan refuses, on purpose: it is
// read off the locked row, and the act is never reached without it.
func refuseUnlessAsPreviewed(locked models.ProcessInstanceModel, plan entities.DeviationPlan, command entities.DeviationCommand) error {
	if plan.VisitKey != command.VisitKey {
		return errMovedSincePreview()
	}
	if !plan.Applicable() {
		return apierr.Invalidf("%s", strings.Join(plan.Refusals, " "))
	}
	if !waitsWhereItActs(locked, command) {
		return errMovedSincePreview()
	}
	return nil
}

// waitsWhereItActs asks the locked row where the instance stands: on the step
// the command names, or — for a cancel that names none — on no step at all.
//
// On the step means holding a token there, one or several: the question a
// migration's skip asks of the row it locked (parkedOn), put to the row as
// the store holds it. Whether the instance is still running is refuseEnded's.
func waitsWhereItActs(locked models.ProcessInstanceModel, command entities.DeviationCommand) bool {
	if command.NodeID == "" {
		return len(locked.Tokens) == 0
	}
	return holdsWork(locked, command.NodeID)
}
