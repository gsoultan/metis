package impl

import (
	"context"
	"encoding/json"
	"slices"
	"strings"

	"github.com/google/uuid"

	"github.com/gsoultan/metis/internal/pkg/apierr"
	"github.com/gsoultan/metis/server/domains/entities"
)

// requireDeviationAdministrator answers who is asking for an in-place
// command, and refuses anybody but a signed-in administrator of the
// organization the request is for.
//
// The route is administrators' only as well. The check is made again here
// because the service is what acts: a caller that reaches it by another road
// must not find it open (AGENTS.md §2.3, a fast path that skips a check).
//
// A request that is for no organization is refused, whatever the account
// holds everywhere. With no organization the repositories do not narrow what
// is read unless the strict scope is on, and the account would be acting on
// an instance of an organization nobody showed it administers. With one, the
// instance is read inside that organization and nowhere else, so the caller
// is an administrator of the organization the instance belongs to — and an
// administrator of another is told there is no such instance.
//
// Every refusal reads the same, and is made before anything is read: it says
// nothing about the instance, not even whether there is one.
func requireDeviationAdministrator(ctx context.Context) (actor string, err error) {
	caller := signedIn(ctx)
	organization := entities.ActingOrganization(ctx)
	if caller == nil || caller.Username == "" || organization == uuid.Nil ||
		!caller.HoldsRoleIn(organization, entities.RoleAdmin) {
		return "", apierr.Forbiddenf("only an administrator can waive, cancel or hold an instance")
	}
	return caller.Username, nil
}

// normalizedDeviationCommand makes a command ready for the planner — the
// step, the reason and the visit key without the spaces around them, no
// outputs as nil — and refuses one that is malformed.
//
// Malformed is what no plan could be made for: a kind that is not one of the
// three, a waive or a hold of no step, outputs where none can be set, or too
// many, or given as nothing, and an apply that names no plan. Each is
// something the caller typed and can fix, so each is an invalid argument.
//
// A reason that is missing or too long is not refused here. It is something a
// preview should show beside everything else that is wrong, so the plan
// refuses it (rulings addendum §9).
func normalizedDeviationCommand(command entities.DeviationCommand) (entities.DeviationCommand, error) {
	switch command.Kind {
	case entities.DeviationWaive, entities.DeviationCancel, entities.DeviationHold:
	default:
		return entities.DeviationCommand{}, apierr.Invalidf("kind must be waive, cancel or hold")
	}
	command.NodeID = strings.TrimSpace(command.NodeID)
	command.Reason = strings.TrimSpace(command.Reason)
	command.VisitKey = strings.TrimSpace(command.VisitKey)
	// A cancel may name no step: that is how an instance waiting nowhere is
	// closed. A waive and a hold act on a step.
	if command.NodeID == "" && command.Kind != entities.DeviationCancel {
		return entities.DeviationCommand{}, apierr.Invalidf("say which step: node_id is required for a waive and a hold")
	}
	if err := checkDeviationOutputs(command.Kind, command.Outputs); err != nil {
		return entities.DeviationCommand{}, err
	}
	if len(command.Outputs) == 0 {
		command.Outputs = nil
	}
	if !command.DryRun && command.VisitKey == "" {
		return entities.DeviationCommand{}, apierr.Invalidf("preview first: an apply names the visit_key of the plan it previewed")
	}
	return command, nil
}

// checkDeviationOutputs refuses outputs no waive could set: any at all on a
// cancel or a hold, more than a form has fields, heavier than the record
// keeps, a value with no name, and a value given as nothing.
//
// A null is refused because it is not a value. Whatever reads the field after
// the step would be told it had been supplied, and would decide on nothing —
// the silent default a waive must say its way out of (AGENTS.md §0).
func checkDeviationOutputs(kind entities.DeviationKind, outputs map[string]any) error {
	if len(outputs) == 0 {
		return nil
	}
	if kind != entities.DeviationWaive {
		return apierr.Invalidf("outputs are what a waived step counts as; a %s sets none", kind)
	}
	if len(outputs) > entities.MaxDeviationOutputs {
		return apierr.Invalidf("a waive sets at most %d values, and this one names %d", entities.MaxDeviationOutputs, len(outputs))
	}
	var null []string
	for name, value := range outputs {
		if name == "" {
			return apierr.Invalidf("an output needs the name of the field it sets")
		}
		if value == nil {
			null = append(null, name)
		}
	}
	if len(null) > 0 {
		return nullOutputs(null)
	}
	encoded, err := json.Marshal(outputs)
	if err != nil {
		return apierr.Invalidf("the outputs cannot be written as JSON: %v", err)
	}
	if len(encoded) > entities.MaxDeviationOutputBytes {
		return apierr.Invalidf("the outputs are larger than %d KiB, which is more than the record of a waive keeps",
			entities.MaxDeviationOutputBytes>>10)
	}
	return nil
}

// nullOutputs is the refusal of outputs given as null, naming them.
func nullOutputs(names []string) error {
	slices.Sort(names)
	if len(names) == 1 {
		return apierr.Invalidf("output %s is null: say what the waiver counts as, or leave it out", names[0])
	}
	return apierr.Invalidf("outputs %s are null: say what the waiver counts as, or leave them out", namesShown(names))
}
