package impl

import (
	"context"
	"fmt"
	"maps"

	"github.com/google/uuid"

	"github.com/gsoultan/metis/server/domains/entities"
	"github.com/gsoultan/metis/server/repositories/models"
)

// planWaive asks what only a waive needs: work a person does, open, on a step
// with one way on, an engine that can end it, and a value for everything the
// process would decide from what the step sets.
func (s *instanceDeviationService) planWaive(ctx context.Context, p *planning) error {
	if p.node == nil {
		return nil
	}
	if !isWorkSomebodyDoes(p.node) {
		return s.refuseWorkNobodyDoes(ctx, p)
	}
	p.refuseWaysOut()
	if s.actions.finisher == nil {
		p.refuse("Waiving needs the execution engine, and this server was wired without it.")
	}
	if len(p.open) == 0 {
		// With no task there is no form to read, so nothing is said of what
		// the step declares or of what reads it.
		p.refuse("Nobody has “%s” to do, so there is nothing to waive.", p.plan.NodeName)
		return nil
	}
	p.warnWhoLosesWork()
	return s.planOutputs(ctx, p)
}

// refuseWorkNobodyDoes refuses to waive a step that is not a person's work,
// and says what to do with that kind of step instead.
func (s *instanceDeviationService) refuseWorkNobodyDoes(ctx context.Context, p *planning) error {
	name := p.plan.NodeName
	switch p.node.Type {
	case entities.ServiceTask:
		p.refuse("“%s” is work for a system, not a person; retry it or resolve its incident instead of waiving it.", name)
	case entities.CallActivity:
		called, err := s.calledAndRunning(ctx, p.instance.ID, p.node.ID)
		if err != nil {
			return err
		}
		if len(called) == 0 {
			p.refuse("“%s” runs another process; waive the step inside that process instead.", name)
			return nil
		}
		p.refuse("“%s” runs another process; waive the step inside that process (instance %s) instead.", name, listed(idsAsText(called)))
	default:
		p.refuse("“%s” is not work somebody does; hold the instance instead.", name)
	}
	return nil
}

// refuseWaysOut refuses a step that has any number of ways out but one. With
// several, moving on would choose a branch; with none there is nowhere to
// move on to — unless the step is inside an ad-hoc sub-process, which has no
// flows and asks its completion condition instead (Engine.checkAdHocCompletion).
//
// Counted from the definition's flows, not from the step's own list of them:
// that is what the engine follows.
func (p *planning) refuseWaysOut() {
	switch ways := len(p.def.GetOutgoingFlows(p.node.ID)); {
	case ways == 1:
	case ways == 0 && p.insideAdHoc():
	case ways == 0:
		p.refuse("“%s” has no way out, so there is nowhere for the instance to go once it is waived.", p.plan.NodeName)
	default:
		p.refuse("“%s” has %d ways out, so waiving it would choose a branch on the business's behalf.", p.plan.NodeName, ways)
	}
}

// insideAdHoc reports whether the step is one of an ad-hoc sub-process's, as
// the engine tells.
func (p *planning) insideAdHoc() bool {
	if p.node.ParentID == "" {
		return false
	}
	parent := p.def.FindNode(p.node.ParentID)
	return parent != nil && parent.IsAdHoc
}

// planOutputs reads what the step's open tasks declare, lists every place in
// the process that decides from one of those fields, and refuses a waive that
// sets what it may not or leaves a decision without the value it reads.
func (s *instanceDeviationService) planOutputs(ctx context.Context, p *planning) error {
	anyRun, everyRun, err := s.declaredByOpenTasks(ctx, p.open)
	if err != nil {
		return err
	}
	var project uuid.UUID
	if p.instance.Project != nil {
		project = p.instance.Project.ID
	}
	lookup := s.decisionLookup(ctx, project)
	p.plan.DecisionPoints = decisionPointsReading(p.def, p.node.ID, anyRun, p.command.Outputs, lookup.reads)
	if lookup.err != nil {
		return lookup.err
	}
	p.plan.Refusals = append(p.plan.Refusals, outputRefusals(p.plan.NodeName, p.command.Outputs, anyRun, everyRun, p.plan.DecisionPoints)...)
	p.plan.Warnings = append(p.plan.Warnings, unreadPointWarnings(p.plan.NodeName, p.plan.DecisionPoints)...)
	return nil
}

// declaredByOpenTasks is what the forms of a step's open tasks declare: what
// any of them does — the values the process may expect from the step — and
// what every one of them does, which is what a waive may set. A value only
// some runs could have set cannot be supplied for all of them, so it is
// refused and not guessed.
//
// A form is read once however many runs carry it.
func (s *instanceDeviationService) declaredByOpenTasks(ctx context.Context, open []models.TaskModel) (anyRun, everyRun map[string]struct{}, err error) {
	anyRun = map[string]struct{}{}
	forms := map[[2]string]map[string]struct{}{}
	for i, task := range open {
		form := [2]string{task.FormKey, task.FormDefinition}
		declared, read := forms[form]
		if !read {
			if declared, err = declaredFieldIDs(ctx, s.repo.Form(), task); err != nil {
				return nil, nil, err
			}
			forms[form] = declared
		}
		maps.Copy(anyRun, declared)
		if i == 0 {
			everyRun = maps.Clone(declared)
			continue
		}
		maps.DeleteFunc(everyRun, func(name string, _ struct{}) bool {
			_, here := declared[name]
			return !here
		})
	}
	if everyRun == nil {
		everyRun = map[string]struct{}{}
	}
	return anyRun, everyRun, nil
}

// outputRefusals is why a waive's outputs will not do: it sets what the
// step's form does not declare, it leaves a decision point without a value it
// reads from the step, or the value is one only some of the step's open runs
// could have set.
func outputRefusals(step string, outputs map[string]any, anyRun, everyRun map[string]struct{}, points []entities.DecisionPoint) []string {
	var refusals []string
	if undeclared := namesOutside(outputs, anyRun); len(undeclared) > 0 {
		refusals = append(refusals, fmt.Sprintf("“%s”'s form does not declare %s, so a waiver cannot set %s.",
			step, listed(undeclared), itOrThem(len(undeclared))))
	}
	// asked is every value the waiver gives or is asked for.
	asked := make(map[string]struct{}, len(outputs))
	for name := range outputs {
		asked[name] = struct{}{}
	}
	for _, point := range points {
		if len(point.Missing) == 0 {
			continue
		}
		names := listed(point.Missing)
		refusals = append(refusals, fmt.Sprintf("“%s” decides from %s, which “%s” would have set; say what the waiver counts as by supplying %s.",
			point.NodeName, names, step, names))
		addNames(asked, point.Missing)
	}
	var someRunsOnly []string
	for _, name := range sortedKeys(asked) {
		_, some := anyRun[name]
		_, every := everyRun[name]
		if some && !every {
			someRunsOnly = append(someRunsOnly, name)
		}
	}
	switch len(someRunsOnly) {
	case 0:
	case 1:
		refusals = append(refusals, fmt.Sprintf("%s is not declared by every open task of “%s”, so a waiver cannot supply it.", someRunsOnly[0], step))
	default:
		refusals = append(refusals, fmt.Sprintf("%s are not declared by every open task of “%s”, so a waiver cannot supply them.", listed(someRunsOnly), step))
	}
	return refusals
}

// unreadPointWarnings says which decision points nobody could vouch for. One
// whose condition could not be read is to be looked at. A process the
// instance calls is handed the step's fields and is never read: the warning
// says what it is handed.
func unreadPointWarnings(step string, points []entities.DecisionPoint) []string {
	var warnings []string
	for _, point := range points {
		switch {
		case point.Analysed:
		case point.Kind == entities.DecisionPointCalledProcess:
			warnings = append(warnings, fmt.Sprintf("“%s” starts another process and hands it %s, which “%s” would have set; "+
				"that process was not read, so check what it does with %s before applying.",
				point.NodeName, listed(point.Reads), step, itOrThem(len(point.Reads))))
		default:
			warnings = append(warnings, fmt.Sprintf("“%s” could not be read to see what it decides from; check it before applying.", point.NodeName))
		}
	}
	return warnings
}
