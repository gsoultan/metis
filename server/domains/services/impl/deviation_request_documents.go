package impl

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"slices"

	"github.com/google/uuid"

	"github.com/gsoultan/metis/server/domains/entities"
)

// waiveCommandDocument is a waive as its request stores it (storedWaiveCommand).
//
// The values are copied, not shared with the command. They are kept as JSON
// keeps them, so what an approval sets is what reading them back gives: a
// number is a float, as the route reads every number in a request.
func waiveCommandDocument(cmd entities.DeviationCommand) map[string]any {
	doc := map[string]any{
		"instance_id": cmd.InstanceID.String(),
		"kind":        string(cmd.Kind),
		"node_id":     cmd.NodeID,
		"reason":      cmd.Reason,
		"visit_key":   cmd.VisitKey,
	}
	if len(cmd.Outputs) > 0 {
		doc["outputs"] = maps.Clone(cmd.Outputs)
	}
	return doc
}

// waiveCommandFrom reads a stored waive back into the command an approval
// applies. It answers an apply, never a dry run; its visit key is the one the
// document holds, which its caller checks against the request's fingerprint.
//
// A document that is not a waive of a step of an instance, whole and with
// nothing else in it, is refused with a plain error: it was written by this
// server, so one that cannot be read is the server's trouble, and an approval
// must never go ahead on a command that was read as partly empty.
func waiveCommandFrom(doc map[string]any) (entities.DeviationCommand, error) {
	var none entities.DeviationCommand
	written, err := json.Marshal(doc)
	if err != nil {
		return none, fmt.Errorf("the stored waive cannot be written out to be read: %w", err)
	}
	var stored storedWaiveCommand
	decoder := json.NewDecoder(bytes.NewReader(written))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&stored); err != nil {
		return none, fmt.Errorf("the stored waive cannot be read: %w", err)
	}
	instanceID, err := uuid.Parse(stored.InstanceID)
	switch {
	case err != nil || instanceID == uuid.Nil:
		return none, fmt.Errorf("the stored waive names no instance (%q)", stored.InstanceID)
	case stored.Kind != string(entities.DeviationWaive):
		return none, fmt.Errorf("the stored command is a %q, not a waive", stored.Kind)
	case stored.NodeID == "":
		return none, errors.New("the stored waive names no step")
	case stored.VisitKey == "":
		return none, errors.New("the stored waive names no visit")
	}
	command := entities.DeviationCommand{
		InstanceID: instanceID, Kind: entities.DeviationWaive, NodeID: stored.NodeID,
		Reason: stored.Reason, VisitKey: stored.VisitKey,
	}
	if len(stored.Outputs) > 0 {
		command.Outputs = stored.Outputs
	}
	return command, nil
}

// waivePlanDocument is the plan a requester was shown, as the request keeps
// it for whoever is asked to approve: the fields of the route's plan view,
// under the same names (endpoints/deviation, PlanView), and because — why it
// needs somebody else.
//
// It is for reading. Every list a plan caps is here beside its count, so the
// second administrator reads "x of y" where the requester did, and its
// warnings are here, so a waive of a control says so to both. Nothing is
// decided from it: an approval plans again from the stored command, under the
// instance's lock, where the refusals are complete.
//
// Every list is copied: a plan's lists may share what they are made of, and a
// record is not changed by what happens to the plan afterwards.
func waivePlanDocument(plan entities.DeviationPlan, because []string) map[string]any {
	openWork := make([]any, 0, len(plan.OpenWork))
	for _, work := range plan.OpenWork {
		openWork = append(openWork, openWorkDocument(work))
	}
	points := make([]any, 0, len(plan.DecisionPoints))
	for _, point := range plan.DecisionPoints {
		points = append(points, decisionPointDocument(point))
	}
	called := make([]string, 0, len(plan.CalledInstances))
	for _, id := range plan.CalledInstances {
		called = append(called, id.String())
	}
	outputs := maps.Clone(plan.Outputs)
	if outputs == nil {
		outputs = map[string]any{}
	}
	return map[string]any{
		"instance_id":              plan.InstanceID.String(),
		"kind":                     string(plan.Kind),
		"scope":                    string(plan.Scope),
		"node_id":                  plan.NodeID,
		"node_name":                plan.NodeName,
		"visit_key":                plan.VisitKey,
		"open_work":                openWork,
		"open_work_in_all":         plan.OpenWorkInAll,
		"outputs":                  outputs,
		"decision_points":          points,
		"decision_points_in_all":   plan.DecisionPointsInAll,
		"missing":                  listed(plan.Missing),
		"missing_in_all":           plan.MissingInAll,
		"called_instances":         called,
		"called_instances_in_all":  plan.CalledInstancesInAll,
		"requires_second_approver": plan.RequiresSecondApprover,
		"refusals":                 listed(plan.Refusals),
		"warnings":                 listed(plan.Warnings),
		"applicable":               plan.Applicable(),
		entities.PlanBecauseKey:    listed(because),
	}
}

// openWorkDocument is one open task as a stored plan lists it: the route's
// OpenWorkView, which leaves out a holder nobody is and an iteration a step
// that runs once does not have.
func openWorkDocument(work entities.DeviationOpenWork) map[string]any {
	doc := map[string]any{
		"task_id":   work.TaskID.String(),
		"name":      work.Name,
		"node_id":   work.NodeID,
		"node_name": work.NodeName,
		"status":    string(work.Status),
	}
	if work.Assignee != "" {
		doc["assignee"] = work.Assignee
	}
	if work.IterationID != "" {
		doc["iteration_id"] = work.IterationID
	}
	return doc
}

// decisionPointDocument is one decision point as a stored plan lists it: the
// route's DecisionPointView.
func decisionPointDocument(point entities.DecisionPoint) map[string]any {
	return map[string]any{
		"node_id":          point.NodeID,
		"node_name":        point.NodeName,
		"kind":             string(point.Kind),
		"reads":            listed(point.Reads),
		"reads_in_all":     point.ReadsInAll,
		"supplied":         listed(point.Supplied),
		"missing":          listed(point.Missing),
		"missing_in_all":   point.MissingInAll,
		"has_default_flow": point.HasDefaultFlow,
		"analysed":         point.Analysed,
	}
}

// listed is a copy of names that is a list even when there are none: stored,
// a nil list reads back as null.
func listed(names []string) []string {
	if len(names) == 0 {
		return []string{}
	}
	return slices.Clone(names)
}
