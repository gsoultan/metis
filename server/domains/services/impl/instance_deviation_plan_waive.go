package impl

import (
	"context"
	"fmt"
	"maps"

	"github.com/google/uuid"

	"github.com/gsoultan/metis/server/domains/entities"
	"github.com/gsoultan/metis/server/repositories/models"
)

// planWaive asks what only a waive needs: work a person does, open, reached
// once, on a step with one way on, an engine that can end it, and a value for
// everything the process would decide from what the step sets.
func (s *instanceDeviationService) planWaive(ctx context.Context, p *planning) error {
	if p.node == nil {
		return nil
	}
	if !isWorkSomebodyDoes(p.node) {
		return s.refuseWorkNobodyDoes(ctx, p)
	}
	p.refuseWaysOut()
	p.refuseReachedMoreThanOnce()
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
		called, err := s.calledAndNotEnded(ctx, p.instance.ID, p.node.ID)
		if err != nil {
			return err
		}
		if len(called) == 0 {
			p.refuse("“%s” runs another process; waive the step inside that process instead.", name)
			return nil
		}
		p.refuse("“%s” runs another process; waive the step inside that process (instance %s) instead.", name, namesShown(idsAsText(called)))
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

// refuseReachedMoreThanOnce refuses to waive a step done once that the
// instance has reached several times at once — two flows of a parallel fork
// that both enter it leave two tokens and two tasks there.
//
// Ending the step whole takes every token off it and moves on once
// (Engine.FinishActivity), where finishing each task by hand moves the
// instance on once for each. The two cannot be told apart from here, and a
// waive that sent on fewer than the process would have is a branch dropped
// without a word: absent constraint means deny. A repeating approval holds a
// token for each of its runs by design, and is ended whole on purpose.
func (p *planning) refuseReachedMoreThanOnce() {
	if p.node.IsRepeatingApproval() {
		return
	}
	if reached := len(p.instance.GetTokensByNode(p.node)); reached > 1 {
		p.refuse("“%s” was reached %d times at once on this instance, and a waive would move the instance on only once. "+
			"Complete or reassign its tasks instead, or hold the instance.", p.plan.NodeName, reached)
	}
}

// planOutputs reads what the step's open tasks declare, finds every place in
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
	points := decisionPointsReading(p.def, p.node.ID, anyRun, p.command.Outputs, lookup.reads)
	if lookup.err != nil {
		return lookup.err
	}
	p.takePoints(points, anyRun, everyRun, func(point entities.DecisionPoint) bool {
		if point.Kind != entities.DecisionPointDecisionTable {
			return false
		}
		node := p.def.FindNode(point.NodeID)
		if node == nil {
			return false
		}
		key, version, _ := decisionConsulted(node)
		return key != "" && lookup.leftUnread(key, version)
	})
	return nil
}

// takePoints turns the decision points of a process into what a plan says of
// them: the refusals and the warnings, worked out from every one, and the
// points themselves, of which a plan lists no more than a screenful and says
// how many there are. pastBound tells a point left unread because the process
// consults more decision tables than one plan reads.
func (p *planning) takePoints(points []entities.DecisionPoint, anyRun, everyRun map[string]struct{}, pastBound func(entities.DecisionPoint) bool) {
	step := p.plan.NodeName
	p.plan.Refusals = append(p.plan.Refusals, outputRefusals(step, p.command.Outputs, anyRun, everyRun, points)...)
	p.plan.Warnings = append(p.plan.Warnings, unreadPointWarnings(step, points, pastBound)...)
	p.plan.DecisionPoints, p.plan.DecisionPointsInAll = listedPoints(points), len(points)
	if more := len(points) - len(p.plan.DecisionPoints); more > 0 {
		p.warn("%d more %s what “%s” would have set and %s not listed here.", more, stepsRead(more), step, isOrAre(more))
	}
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
//
// Every point is counted, and every missing value is among those named. The
// first few points are named one by one and the rest together, so the answer
// is a dozen sentences for a process of any size.
func outputRefusals(step string, outputs map[string]any, anyRun, everyRun map[string]struct{}, points []entities.DecisionPoint) []string {
	var refusals []string
	if undeclared := namesOutside(outputs, anyRun); len(undeclared) > 0 {
		refusals = append(refusals, fmt.Sprintf("“%s”'s form does not declare %s, so a waiver cannot set %s.",
			step, namesShown(undeclared), itOrThem(len(undeclared))))
	}
	var wanting []entities.DecisionPoint
	for _, point := range points {
		if len(point.Missing) > 0 {
			wanting = append(wanting, point)
		}
	}
	named := min(len(wanting), maxPointsNamed)
	for _, point := range wanting[:named] {
		refusals = append(refusals, missingAt(point, step))
	}
	if rest := wanting[named:]; len(rest) > 0 {
		names := namesShown(sortedKeys(missingAcross(rest)))
		refusals = append(refusals, fmt.Sprintf("%d more %s %s, which “%s” would have set; say what the waiver counts as by supplying %s.",
			len(rest), stepsRead(len(rest)), names, step, names))
	}
	// asked is every value the waiver gives or is asked for.
	asked := missingAcross(wanting)
	for name := range outputs {
		asked[name] = struct{}{}
	}
	var someRunsOnly []string
	for _, name := range sortedKeys(asked) {
		_, some := anyRun[name]
		_, every := everyRun[name]
		if some && !every {
			someRunsOnly = append(someRunsOnly, name)
		}
	}
	if len(someRunsOnly) > 0 {
		refusals = append(refusals, fmt.Sprintf("%s %s not declared by every open task of “%s”, so a waiver cannot supply %s.",
			namesShown(someRunsOnly), isOrAre(len(someRunsOnly)), step, itOrThem(len(someRunsOnly))))
	}
	return refusals
}

// missingAt is the refusal for one decision point that is missing a value the
// step would have set. A step that repeats over a list does not decide from
// it: it takes its runs from it.
func missingAt(point entities.DecisionPoint, step string) string {
	names := namesShown(point.Missing)
	does := "decides from"
	if point.Kind == entities.DecisionPointCollection {
		does = "takes its list of runs from"
	}
	return fmt.Sprintf("“%s” %s %s, which “%s” would have set; say what the waiver counts as by supplying %s.",
		point.NodeName, does, names, step, names)
}

// missingAcross is every value some point of a list is missing, each once.
//
// Points that read the same decision share one list of what they miss, and a
// process may have thousands of them: a list already taken is not taken
// again, so this costs what the lists are long, not that times the points.
func missingAcross(points []entities.DecisionPoint) map[string]struct{} {
	names := map[string]struct{}{}
	taken := map[*string]int{}
	for _, point := range points {
		if len(point.Missing) == 0 || taken[&point.Missing[0]] >= len(point.Missing) {
			continue
		}
		taken[&point.Missing[0]] = len(point.Missing)
		addNames(names, point.Missing)
	}
	return names
}

// unreadPointWarnings says which decision points nobody could vouch for, the
// first few one by one and the rest counted. One whose condition could not be
// read is to be looked at. A process the instance calls is handed the step's
// fields and is never read: the warning says what it is handed. A decision
// left unread because the process consults more tables than one plan reads
// is said to be that, since reading it again will not help.
func unreadPointWarnings(step string, points []entities.DecisionPoint, pastBound func(entities.DecisionPoint) bool) []string {
	var warnings []string
	more := 0
	for _, point := range points {
		switch {
		case point.Analysed:
		case len(warnings) == maxPointsNamed:
			more++
		case point.Kind == entities.DecisionPointCalledProcess:
			warnings = append(warnings, fmt.Sprintf("“%s” starts another process and hands it %s, which “%s” would have set; "+
				"that process was not read, so check what it does with %s before applying.",
				point.NodeName, namesShown(point.Reads), step, itOrThem(len(point.Reads))))
		case pastBound != nil && pastBound(point):
			warnings = append(warnings, fmt.Sprintf("“%s” was not read: this process consults more decision tables than one preview reads (%d). "+
				"Check it before applying.", point.NodeName, maxDecisionTablesPerPlan))
		default:
			warnings = append(warnings, fmt.Sprintf("“%s” could not be read to see what it decides from; check it before applying.", point.NodeName))
		}
	}
	if more > 0 {
		warnings = append(warnings, fmt.Sprintf("%d more %s could not be read either; check %s before applying.", more, stepOrSteps(more), itOrThem(more)))
	}
	return warnings
}

// listedPoints is the decision points a plan shows: those missing a value
// first, then those nobody could read, then the rest, each group in the order
// it came in, and no more than maxDecisionPointsListed in all.
//
// Each is shown with its lists cut as a sentence would cut them. The cut is
// made on a copy: the points share their lists, and what is cut for one would
// be cut for all.
func listedPoints(points []entities.DecisionPoint) []entities.DecisionPoint {
	if len(points) == 0 {
		return nil
	}
	missing := func(point entities.DecisionPoint) bool { return len(point.Missing) > 0 }
	unread := func(point entities.DecisionPoint) bool { return !missing(point) && !point.Analysed }
	rest := func(point entities.DecisionPoint) bool { return !missing(point) && point.Analysed }

	listed := make([]entities.DecisionPoint, 0, min(len(points), maxDecisionPointsListed))
	for _, wanted := range []func(entities.DecisionPoint) bool{missing, unread, rest} {
		for _, point := range points {
			if len(listed) == maxDecisionPointsListed {
				return listed
			}
			if wanted(point) {
				point.Reads, point.Supplied, point.Missing = cutForShowing(point.Reads), cutForShowing(point.Supplied), cutForShowing(point.Missing)
				listed = append(listed, point)
			}
		}
	}
	return listed
}

// cutForShowing is a copy of a list of names as a plan shows it: the first
// few, each cut to a length somebody would read, and nil for none.
func cutForShowing(names []string) []string {
	if len(names) == 0 {
		return nil
	}
	shown, _ := shownNames(names)
	return shown
}

// stepOrSteps, stepsRead and isOrAre word a count of steps.
func stepOrSteps(count int) string {
	if count == 1 {
		return "step"
	}
	return "steps"
}

func stepsRead(count int) string {
	if count == 1 {
		return "step reads"
	}
	return "steps read"
}

func isOrAre(count int) string {
	if count == 1 {
		return "is"
	}
	return "are"
}
