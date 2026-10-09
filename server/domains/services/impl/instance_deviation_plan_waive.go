package impl

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"maps"
	"slices"
	"unicode/utf8"

	"github.com/google/uuid"

	"github.com/gsoultan/metis/internal/pkg/apierr"
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
		p.refuse("Nobody has “%s” to do, so there is nothing to waive.", p.stepShown())
		return nil
	}
	if isControl(p.node) {
		p.warn("“%s” is marked as a control. Waiving it is recorded as a control that was not performed.", p.stepShown())
	}
	p.warnWhoLosesWork()
	if err := s.warnThatTheCallerWasNotRead(ctx, p); err != nil {
		return err
	}
	return s.planOutputs(ctx, p)
}

// controlProperty is how a definition marks a step as a control, and
// detailControl the key under which the record of a waive says the step was
// one.
const (
	controlProperty = "compliance_relevant"
	detailControl   = "control"
)

// isControl reports whether a step is marked as a control: one the business
// has to be able to show was performed.
//
// A control is waived as any step is, and the instance's own list of the steps
// it has passed then counts it — a later migration that drops the step reads
// that list and writes no "control waived" for this instance. So the waive
// itself says what it is: the plan warns, and the ledger row and the trail
// entry are marked (waived), where nobody has to join the row to the
// definition to see it.
func isControl(node *entities.Node) bool {
	return node != nil && boolProperty(node.Properties, controlProperty)
}

// warnThatTheCallerWasNotRead says, of a waive in an instance another process
// started, that what that process does with this one's results was not looked
// at.
//
// When a called instance ends its values go back to its caller, which goes on
// from the step that called it and decides from them — a gateway there may
// read a field the waived step would have set. The caller runs another
// definition, and a plan reads the one its instance runs: so nothing is
// refused for the caller's sake, and the plan says what it did not read,
// where, and what comes of it (callerWasNotRead). Said only of a caller that
// has not ended: one that has receives nothing.
func (s *instanceDeviationService) warnThatTheCallerWasNotRead(ctx context.Context, p *planning) error {
	started := p.instance.ParentInstance
	if started == nil {
		return nil
	}
	caller, err := s.engine.GetInstance(ctx, started.ID)
	if errors.Is(err, apierr.ErrNotFound) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("reading the instance that started instance %s: %w", p.instance.ID, err)
	}
	if instanceEnded(caller.Status) {
		return nil
	}
	process, step, err := s.whereItWasCalledFrom(ctx, caller, p.instance.ParentNode)
	if err != nil {
		return err
	}
	p.warn("%s", callerWasNotRead(process, step))
	return nil
}

// callerWasNotRead is the warning of a caller a plan did not read: the process
// it runs, the step of it that called when that is known, and what it does
// with a value the waiver does not give.
//
// It says both things that can happen, because they are opposite. A caller
// handed its values to the instance it called and takes them back when that
// instance ends: holding a value for a field the waived step would have set,
// it decides on that value and the waive is applied; holding none, its
// gateway has no way out and the waive is undone.
func callerWasNotRead(process, step string) string {
	at := ""
	if step != "" {
		at = fmt.Sprintf(" at “%s”", step)
	}
	return fmt.Sprintf("This instance was started by “%s”%s, which receives its results when it ends and was not read. "+
		"Where that process decides on a value this step would have set and you give none, "+
		"it decides on the value it already holds, or undoes the waive if it holds none. Check that process before applying.", process, at)
}

// whereItWasCalledFrom names the process a caller runs and the step of it
// that called: each by its name, cut as a plan shows a name, falling back to
// what identifies it — the process's key, the step's id — and, for a process
// that cannot be named at all, the caller's own id.
func (s *instanceDeviationService) whereItWasCalledFrom(ctx context.Context, caller entities.ProcessInstance, at *entities.Node) (process, step string, err error) {
	process = caller.ID.String()
	if at != nil {
		step = at.ID
	}
	if caller.Definition == nil {
		return process, step, nil
	}
	def, err := s.engine.GetProcessDefinition(ctx, caller.Definition.ID)
	if errors.Is(err, apierr.ErrNotFound) || (err == nil && def == nil) {
		return process, step, nil
	}
	if err != nil {
		return "", "", fmt.Errorf("reading the process instance %s runs: %w", caller.ID, err)
	}
	process = cmp.Or(shownStepName(def.Name), def.Key, process)
	if node := def.FindNode(step); node != nil && node.Name != "" {
		step = shownStepName(node.Name)
	}
	return process, step, nil
}

// refuseWorkNobodyDoes refuses to waive a step that is not a person's work,
// and says what to do with that kind of step instead.
//
// What it says can be done. A call step points at the process it called
// while one is running. When the instance waits at the step and none is —
// the called instance was cancelled in place, or ended at a terminate end
// event, neither of which resumes its caller — there is no step inside it
// left to waive, and the refusal says what is left: the instance can be
// cancelled or held. An instance that has not reached the step is told of the
// process in general, beside being told it is not waiting there; so is one
// that waits at a call step under which no process was ever started, a state
// nothing here has been able to produce.
func (s *instanceDeviationService) refuseWorkNobodyDoes(ctx context.Context, p *planning) error {
	name := p.stepShown()
	switch p.node.Type {
	case entities.ServiceTask:
		p.refuse("“%s” is work for a system, not a person; retry it or resolve its incident instead of waiving it.", name)
	case entities.CallActivity:
		called, ended, err := s.calledFrom(ctx, p.instance.ID, p.node.ID)
		if err != nil {
			return err
		}
		waitsThere := len(p.instance.GetTokensByNode(p.node)) > 0
		switch {
		case len(called) > 0:
			p.refuse("“%s” runs another process; waive the step inside that process (instance %s) instead.", name, namesShown(idsAsText(called)))
		case waitsThere && ended > 0:
			p.refuse("“%s” is waiting for a process that has ended and will not resume it; this instance can be cancelled or held instead.", name)
		default:
			p.refuse("“%s” runs another process; waive the step inside that process instead.", name)
		}
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
		p.refuse("“%s” has no way out, so there is nowhere for the instance to go once it is waived.", p.stepShown())
	default:
		p.refuse("“%s” has %d ways out, so waiving it would choose a branch on the business's behalf.", p.stepShown(), ways)
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
			"Complete or reassign its tasks instead, or hold the instance.", p.stepShown(), reached)
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
	found := decisionPointsReading(p.def, p.node.ID, anyRun, p.command.Outputs, lookup.reads)
	if lookup.err != nil {
		return lookup.err
	}
	p.takePoints(found, anyRun, everyRun, func(point entities.DecisionPoint) bool {
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

// takePoints turns what a scan found into what a plan says of it: the
// refusals and the warnings, worked out from every point, the points
// themselves, of which a plan lists no more than a screenful and says how
// many there are, and the whole of what is missing. pastBound tells a point
// left unread because the process consults more decision tables than one plan
// reads.
func (p *planning) takePoints(found decisionPointsFound, anyRun, everyRun map[string]struct{}, pastBound func(entities.DecisionPoint) bool) {
	step, points := p.stepShown(), found.points
	p.plan.Refusals = append(p.plan.Refusals, outputRefusals(step, p.command.Outputs, anyRun, everyRun, found)...)
	p.plan.Warnings = append(p.plan.Warnings, unreadPointWarnings(step, points, pastBound)...)
	p.plan.DecisionPoints, p.plan.DecisionPointsInAll = listedPoints(points), len(points)
	if more := len(points) - len(p.plan.DecisionPoints); more > 0 {
		p.warn("%d more %s what “%s” would have set and %s not listed here.", more, stepsRead(more), step, isOrAre(more))
	}
	missing := sortedKeys(found.missing)
	p.plan.Refusals = append(p.plan.Refusals, namelessToSet(missing)...)
	p.plan.Refusals = append(p.plan.Refusals, tooLongToSet(missing)...)
	p.plan.Missing, p.plan.MissingInAll = missingShown(missing), len(missing)
}

// missingShown is what a plan lists as missing, from every missing name in
// order: each in full, so that it can be copied from the list into the
// outputs of the next request — a name cut short names nothing — and no more
// of them than one waive may set, since a waive that needs more cannot be
// made at all and is refused for that.
//
// In full up to the length the ledger keeps of a name. A form's field is its
// author's to name, and a plan has a size whatever the form: a longer name is
// listed cut, and the plan refuses the waive for it (tooLongToSet).
func missingShown(missing []string) []string {
	if len(missing) == 0 {
		return nil
	}
	shown := slices.Clone(missing[:min(len(missing), entities.MaxDeviationOutputs)])
	for i, name := range shown {
		shown[i] = shownStepName(name)
	}
	return shown
}

// tooLongToSet refuses a waive for each missing value whose name is longer
// than a plan lists a name at: listed cut, it cannot be supplied from the
// plan, and the waive cannot be made without it. The first few are named —
// as a sentence names anything, by their first characters — and the rest
// counted.
func tooLongToSet(missing []string) []string {
	var refusals []string
	more := 0
	for _, name := range missing {
		switch {
		case utf8.RuneCountInString(name) <= deviationNodeNameLength:
		case len(refusals) == maxPointsNamed:
			more++
		default:
			refusals = append(refusals, fmt.Sprintf("“%s” is too long a name for a waive to set; complete or reassign the task instead.", shortened(name)))
		}
	}
	if more > 0 {
		refusals = append(refusals, fmt.Sprintf("%d more of the values it would have to set have names as long.", more))
	}
	return refusals
}

// namelessToSet refuses a waive that is missing a value with no name. The
// command refuses an output with no name (checkDeviationOutputs), so the plan
// must not ask for one: it says in words that the waive cannot supply it.
//
// A form's field with no id declares nothing (addFieldIDs), so nothing the
// service reads puts an empty name among what a step declares. This is for
// whoever hands the planner a set that does.
func namelessToSet(missing []string) []string {
	if !slices.Contains(missing, "") {
		return nil
	}
	return []string{"A field with no name is read by a decision, and a waive cannot set a value with no name; complete or reassign the task instead."}
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
// reads from the step, the values it would have to give are more than one
// waive may set, or a value is one only some of the step's open runs could
// have set.
//
// It is worked out from what the scan found for the whole process — every
// value that is missing, and how many points miss one — and never from the
// few names a point shows. The first few points are named one by one and the
// rest counted, so the answer is a dozen sentences for a process of any size;
// the plan's own list of what is missing is the complete one.
func outputRefusals(step string, outputs map[string]any, anyRun, everyRun map[string]struct{}, found decisionPointsFound) []string {
	var refusals []string
	if undeclared := namesOutside(outputs, anyRun); len(undeclared) > 0 {
		refusals = append(refusals, fmt.Sprintf("“%s”'s form does not declare %s, so a waiver cannot set %s.",
			step, namesShown(undeclared), itOrThem(len(undeclared))))
	}
	named := 0
	for _, point := range found.points {
		if named == maxPointsNamed {
			break
		}
		if point.MissingInAll > 0 {
			refusals = append(refusals, missingAt(point, step))
			named++
		}
	}
	if rest := found.missingAt - named; rest > 0 {
		refusals = append(refusals, fmt.Sprintf("%d more %s values “%s” would have set. In all, say what the waiver counts as by supplying %s.",
			rest, stepsRead(rest), step, namesShown(sortedKeys(found.missing))))
	}
	if tooMany := moreThanOneWaiveSets(step, givenOf(outputs, everyRun), len(found.missing)); tooMany != "" {
		refusals = append(refusals, tooMany)
	}
	if someRunsOnly := declaredBySomeRunsOnly(outputs, found.missing, anyRun, everyRun); len(someRunsOnly) > 0 {
		refusals = append(refusals, fmt.Sprintf("%s %s not declared by every open task of “%s”, so a waiver cannot supply %s.",
			namesShown(someRunsOnly), isOrAre(len(someRunsOnly)), step, itOrThem(len(someRunsOnly))))
	}
	return refusals
}

// givenOf is how many of the values a waive gives are ones it may set: those
// every open task's form declares. A name no form declares, or only some do,
// is refused for that and is taken out before the waive is sent again, so it
// is not counted toward how many values the waive would come to set.
func givenOf(outputs map[string]any, everyRun map[string]struct{}) int {
	given := 0
	for name := range outputs {
		if _, may := everyRun[name]; may {
			given++
		}
	}
	return given
}

// moreThanOneWaiveSets is the refusal of a waive that would have to give more
// values than a waive may: what it gives already (givenOf) and what is still
// missing come to more than MaxDeviationOutputs. Said at once, with the
// count, so that nobody finds it by supplying the values ten at a time.
func moreThanOneWaiveSets(step string, given, missing int) string {
	switch {
	case missing == 0 || given+missing <= entities.MaxDeviationOutputs:
		return ""
	case given == 0:
		return fmt.Sprintf("This process decides from %d %s “%s” would have set, and one waive may set at most %d. "+
			"Complete or reassign “%s” instead, or hold the instance.", missing, valueOrValues(missing), step, entities.MaxDeviationOutputs, step)
	}
	return fmt.Sprintf("This process decides from %d more %s “%s” would have set, beside the %d this waive gives, and one waive may set at most %d. "+
		"Complete or reassign “%s” instead, or hold the instance.", missing, valueOrValues(missing), step, given, entities.MaxDeviationOutputs, step)
}

// valueOrValues words a count of values.
func valueOrValues(count int) string {
	if count == 1 {
		return "value"
	}
	return "values"
}

// declaredBySomeRunsOnly is the values the waiver gives or is asked for that
// some of the step's open runs could have set and others could not, sorted.
func declaredBySomeRunsOnly(outputs map[string]any, missing, anyRun, everyRun map[string]struct{}) []string {
	var names []string
	consider := func(name string) {
		_, some := anyRun[name]
		_, every := everyRun[name]
		if some && !every {
			names = append(names, name)
		}
	}
	for name := range outputs {
		consider(name)
	}
	for name := range missing {
		if _, given := outputs[name]; !given {
			consider(name)
		}
	}
	slices.Sort(names)
	return names
}

// missingAt is the refusal for one decision point that is missing a value the
// step would have set: the first few it is missing, and how many more. A step
// that repeats over a list does not decide from it: it takes its runs from it.
func missingAt(point entities.DecisionPoint, step string) string {
	names := namesShownOf(point.Missing, point.MissingInAll)
	does := "decides from"
	if point.Kind == entities.DecisionPointCollection {
		does = "takes its list of runs from"
	}
	return fmt.Sprintf("“%s” %s %s, which “%s” would have set; say what the waiver counts as by supplying %s.",
		point.NodeName, does, names, step, names)
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
				point.NodeName, namesShownOf(point.Reads, point.ReadsInAll), step, itOrThem(len(point.Reads))))
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
// Each is shown with the names in its lists cut to a length somebody would
// read. The cut is made on a copy: the points share their lists, and what is
// cut for one would be cut for all.
func listedPoints(points []entities.DecisionPoint) []entities.DecisionPoint {
	if len(points) == 0 {
		return nil
	}
	// By the count, as the refusals are worked out (outputRefusals): the list
	// beside it is for showing.
	missing := func(point entities.DecisionPoint) bool { return point.MissingInAll > 0 }
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
