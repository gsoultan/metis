package impl

import (
	"bytes"
	"cmp"
	"fmt"
	"slices"
	"strings"

	"github.com/google/uuid"

	"github.com/gsoultan/metis/server/domains/entities"
	servicecontracts "github.com/gsoultan/metis/server/domains/services/contracts"
	"github.com/gsoultan/metis/server/repositories/models"
)

// newcomersNamed is how many of the instances a request does not cover are
// named when a migration is refused for them. All of them are counted.
const newcomersNamed = 5

// runningOf is the instances of a listing that have not ended: every one a
// run of a migration could come to act on, and so every one a decision can
// reach.
//
// A plan lists every instance of the version, ended ones too, and counts
// them as it always has. Who needs a second administrator is counted from
// these alone: nothing is skipped for, and no control taken from, an
// instance that ran to its end, failed or was cancelled.
//
// Not ended, rather than running now (instanceEnded, the question the
// in-place plan asks). A run acts only on an instance that is active when it
// reaches it — but it reaches it later than this listing, and a suspended
// instance can be active again by then. Counted only if active now, such an
// instance was acted on with nobody having been shown it. So a suspended
// instance is counted, and so is one in a state this does not know: the count
// may be larger than what the run then acts on, never smaller.
func runningOf(instances []models.ProcessInstanceModel) []models.ProcessInstanceModel {
	running := make([]models.ProcessInstanceModel, 0, len(instances))
	for _, instance := range instances {
		if !instanceEnded(entities.ProcessStatus(instance.Status)) {
			running = append(running, instance)
		}
	}
	return running
}

// activeInstanceIDs is the ids of the instances of a plan's listing that
// have not ended (runningOf), in one order: the instances a request for the
// plan shows, and the instances an apply under that request may act on.
//
// It is a list always, with nothing in it when nothing runs.
func activeInstanceIDs(covered []models.ProcessInstanceModel) []uuid.UUID {
	running := runningOf(covered)
	ids := make([]uuid.UUID, 0, len(running))
	for _, instance := range running {
		ids = append(ids, uuid.UUID(instance.ID))
	}
	slices.SortFunc(ids, func(a, b uuid.UUID) int { return bytes.Compare(a[:], b[:]) })
	return ids
}

// secondApproverReasons is why a migration is not one administrator's call,
// one sentence for each reason, sorted; none when it is.
//
// Two of the four things that make it so are counted here: a step skipped —
// nobody performs it — and a control the migration takes from instances that
// have not passed it. The other two have functions of their own, and planFor
// adds their sentences to these: a step redirected while some instance has a
// control it has not passed (redirectsPastControls), and a
// separation-of-duties rule loosened on a step some instance has still to
// pass (dutiesLoosened). A cancel ends an instance and a hold stops one;
// neither loosens a rule on work that goes on, and neither is anywhere among
// them.
//
// Both are counted over running, the instances that have not ended, and over
// nothing else: with nothing running there is nothing a skip could skip or a
// dropped control be taken from, and nobody is asked. A hold's own count
// (ComplianceHold.Instances) includes instances that ended and is not read.
//
// A skip is a reason whether or not anything waits at the step now. What is
// approved is the decision, over the instances the request lists: one of
// them that reaches the step before the migration runs is skipped under it.
// The sentence says so to whoever approves.
func secondApproverReasons(
	sourceNodes map[string]models.FlowNode,
	actions map[string]servicecontracts.NodeAction,
	holds []entities.ComplianceHold,
	running []models.ProcessInstanceModel,
) []string {
	if len(running) == 0 {
		return nil
	}
	var reasons []string
	for _, nodeID := range sortedKeys(actions) {
		if actions[nodeID].Kind != servicecontracts.NodeActionSkip {
			continue
		}
		reasons = append(reasons, fmt.Sprintf(
			"“%s” would be skipped for every listed instance waiting at it when the migration runs — "+
				"not only those waiting there when this was asked for — and nobody would perform it",
			stepCalled(sourceNodes[nodeID], nodeID)))
	}
	for _, hold := range holds {
		notPassed := notPassedBy(running, hold.NodeID)
		if notPassed == 0 {
			continue
		}
		reasons = append(reasons, fmt.Sprintf("“%s” carries a control %d instance(s) have not passed, and they never would",
			shownStepName(cmp.Or(sourceNodes[hold.NodeID].Name, hold.Name, hold.NodeID)), notPassed))
	}
	slices.Sort(reasons)
	return reasons
}

// controlsNamed is how many controls, or steps, a reason names one by one.
// All of them count; the rest are said as a number.
const controlsNamed = 5

// reasonsListed is how many reasons a plan lists. A mapping is as long as
// whoever wrote it made it, and each redirect in it is a reason: the list is
// read by a person, stored with the request and said in a refusal, so it has
// a size. The rest are counted in one last sentence. Whether a second
// administrator is needed is decided from all of them, never from the list.
const reasonsListed = 10

// listedReasons is the reasons a plan shows: the first reasonsListed of them
// in their order, and one sentence counting the rest.
func listedReasons(all []string) []string {
	if len(all) <= reasonsListed {
		return all
	}
	listed := slices.Clone(all[:reasonsListed])
	return append(listed, fmt.Sprintf("and %d more reason(s) of the same kinds, not listed here", len(all)-reasonsListed))
}

// stepCalled is what a reason calls a step: its name, or its id when it has
// none, as much of it as a ledger row keeps of a step's name (shownStepName).
// A definition's author chooses the name and may make it as long as they
// like; a reason is said, stored and refused with.
func stepCalled(node models.FlowNode, id string) string {
	return shownStepName(cmp.Or(node.Name, id))
}

// redirectsPastControls is why a mapping that sends a step's work to a
// different step is not one administrator's call, one sentence for each such
// redirect, sorted; none when no control is at stake.
//
// A redirect drops nothing and skips nothing: the control is still in the
// new version. But an instance moved from "prepare" to "sign" lands beyond a
// control that stood between them, and nobody performs it. Whether a control
// still lies ahead of where an instance lands is a question about the graph
// that this does not try to answer — a wrong answer would be an instance past
// a control with nobody asked. So it asks whenever there is anything to lose:
// the mapping redirects a step, and some instance that has not ended has not
// passed some control of the version it runs.
//
// A redirect is a mapping of a step that is not that step renamed where it
// stands (inPlace, from renamedInPlace). A new id is not enough to make it
// one.
//
// It counts over running, the instances that have not ended, and not only
// those waiting at the step now: what is approved is the redirect, over the
// instances the request lists, and one of them that reaches the step before
// the migration reaches it is redirected under it — as a skip is.
//
// The sentence says what is known, and of whom. When instances wait at the
// step, it names the controls one of them has not passed. When none does, or
// those that do have passed every control, it says so, and names the
// controls an instance that may yet reach the step has not passed. Either
// way: that such a control may no longer be ahead — not that it is lost.
//
// It is not a control known to be dropped: nothing is held, nothing is
// acknowledged, and the run writes no row saying a control was waived. That
// row could be false.
func redirectsPastControls(
	sourceNodes, targetNodes map[string]models.FlowNode,
	nodeMapping, inPlace map[string]string,
	running []models.ProcessInstanceModel,
) []string {
	atStake := controlsNotPassedByAll(sourceNodes, running)
	if atStake == "" {
		return nil
	}
	var reasons []string
	for _, from := range sortedKeys(nodeMapping) {
		to := nodeMapping[from]
		if !redirects(sourceNodes, inPlace, from, to) {
			continue
		}
		step := sourceNodes[from]
		var waiting []models.ProcessInstanceModel
		for _, instance := range running {
			if holdsWork(instance, from) {
				waiting = append(waiting, instance)
			}
		}
		redirected := fmt.Sprintf("“%s” would be redirected to “%s” for every listed instance waiting at it when the migration runs",
			stepCalled(step, from), stepCalled(targetNodes[to], to))
		if theirs := controlsNotPassedByAll(sourceNodes, waiting); theirs != "" {
			reasons = append(reasons, fmt.Sprintf("%s — %d wait(s) there now — and a control such an instance has not passed may no longer be ahead of it: %s",
				redirected, len(waiting), theirs))
			continue
		}
		now := "nobody waits there now"
		if len(waiting) > 0 {
			now = fmt.Sprintf("%d wait(s) there now, having passed every control", len(waiting))
		}
		reasons = append(reasons, fmt.Sprintf("%s — %s — and for an instance that reaches it by then, a control it has not passed may no longer be ahead of it: %s",
			redirected, now, atStake))
	}
	slices.Sort(reasons)
	return reasons
}

// redirects reports whether a mapping entry sends a step somewhere else: its
// key is a step the version being left has, it maps the step to another id,
// and it is not among renamed — the entries that are that step under a new
// id.
//
// It is the one place that is asked. What a plan counts as a redirect
// (redirectsPastControls) and what a request then says was redirected
// (redirectedSteps) ask it with the steps renamed where they stand
// (renamedInPlace), so a request never names a redirect its plan did not
// count. What a plan warns of (redirectWarnings) asks it with the renames
// finished work follows (finishedWorkFollows): its warning is about work
// already done, and that is the notion of a rename such work follows.
//
// An entry whose key is no step of the source is none: nothing waits, and
// nothing was done, at a step the version does not have.
func redirects(sourceNodes map[string]models.FlowNode, renamed map[string]string, from, to string) bool {
	if _, isStep := sourceNodes[from]; !isStep || to == from {
		return false
	}
	_, isRename := renamed[from]
	return !isRename
}

// controlsNotPassedByAll names the steps of a version marked as controls
// that some instance of running has not performed: by name, in the order of
// their ids, the first few one by one and the rest as a number. Empty when
// every one of them has passed every control, or there is none of either.
func controlsNotPassedByAll(sourceNodes map[string]models.FlowNode, running []models.ProcessInstanceModel) string {
	var names []string
	for _, id := range sortedKeys(sourceNodes) {
		if !boolProperty(sourceNodes[id].Properties, "compliance_relevant") {
			continue
		}
		notPassed := slices.ContainsFunc(running, func(instance models.ProcessInstanceModel) bool {
			return !slices.Contains(instance.CompletedNodes, id)
		})
		if notPassed {
			names = append(names, "“"+stepCalled(sourceNodes[id], id)+"”")
		}
	}
	if len(names) == 0 {
		return ""
	}
	return firstNamed(names)
}

// dutiesLoosened is why a migration onto a version that takes away part of a
// separation-of-duties rule is not one administrator's call, sorted; none
// when no rule is loosened for anybody.
//
// A step's rule names the steps whose performer may not also perform it. It
// is the approval rule the engine enforces, against the version an instance
// runs: it looks for a completed task of the instance on a step the rule
// names, by that step's id. So for an instance that has still to pass the
// step, two things loosen it, and each is said in its own sentence.
//
// A rule the instance can come to perform the step under (stepsInPlaceOf) no
// longer names a step the source's rule named. Whoever performed that step
// is then no longer refused. That is said in one of two sentences, by its
// cause (namesLoosened): the new version dropped the name, or it kept the
// rule under the id a mapping sends the named step to, where the work
// already done on that step does not follow. A name counts as kept only when every one of
// those rules keeps it. The names are compared after the renames finished
// work follows (finishedWorkFollows), which is how finished work is found
// under a step's new id: a version that renames the steps and the rule with
// them has loosened nothing. A step renamed onto a control that stands
// elsewhere is not among them — its finished work stays under the old id —
// so a rule that names it by the new one no longer finds who did it, and
// that is said.
//
// Or every such rule still names it, and the new version no longer has the
// step. Whoever performed it before the migration is still refused — their
// completed task keeps its step's id — but nobody can perform it afterwards,
// so the rule refuses nobody new. This is counted only for a step the source
// version has: a rule that names a step neither version has is unchanged by
// the migration, refuses whom it refused, and asks nobody.
//
// A step the new version has nothing in place of is not looked at: its work
// is refused, moved or decided by other rules. It counts over running, the
// instances that have not ended, and of those only the ones that have not
// passed the step: one that has is held to no rule there any longer.
//
// It is not a control dropped: nothing is held and nothing acknowledged.
func dutiesLoosened(
	sourceNodes, targetNodes map[string]models.FlowNode,
	nodeMapping, inPlace map[string]string,
	running []models.ProcessInstanceModel,
) []string {
	if len(running) == 0 {
		return nil
	}
	renames := finishedWorkFollows(sourceNodes, targetNodes, nodeMapping, inPlace)
	var reasons []string
	for _, id := range sortedKeys(sourceNodes) {
		barred := splitNodeList(stringProperty(sourceNodes[id].Properties, SeparationOfDutiesKey))
		inItsPlace := stepsInPlaceOf(id, targetNodes, nodeMapping)
		if len(barred) == 0 || len(inItsPlace) == 0 {
			continue
		}
		notPassed := notPassedBy(running, id)
		if notPassed == 0 {
			continue
		}
		unnamed, unfollowed, gone := namesLoosened(barred, inItsPlace, renames, nodeMapping, sourceNodes, targetNodes)
		step := stepCalled(sourceNodes[id], id)
		if len(unfollowed) > 0 {
			reasons = append(reasons, fmt.Sprintf(
				"“%s” would no longer be refused to whoever performed %s: the new version's rule names the step that is "+
					"mapped onto, but work already done does not follow that mapping, so the rule would not find who did it — "+
					"and %d instance(s) have not passed “%s”", step, firstNamed(unfollowed), notPassed, step))
		}
		if len(unnamed) > 0 {
			reasons = append(reasons, fmt.Sprintf(
				"“%s” would no longer be refused to whoever performed %s: the new version does not keep that separation of duties, "+
					"and %d instance(s) have not passed “%s”", step, firstNamed(unnamed), notPassed, step))
		}
		if len(gone) > 0 {
			reasons = append(reasons, fmt.Sprintf(
				"“%s” may not be done by whoever performed %s, which the new version no longer has: whoever performed it before the "+
					"migration is still refused, but nobody can perform it afterwards, so the rule refuses nobody new — "+
					"and %d instance(s) have not passed “%s”", step, firstNamed(gone), notPassed, step))
		}
	}
	slices.Sort(reasons)
	return reasons
}

// stepsInPlaceOf is every step of the new version that an instance which has
// not passed a source step can come to perform in its place.
//
// The step the mapping lands on: an instance waiting at the source step when
// the migration runs is moved there. And, when the mapping sends the step
// elsewhere while the new version keeps a step under the source id, that
// step too: an instance that has not reached the source step yet is not
// there to be moved, and comes to the step the new version has under the id.
// A mapping entry is therefore no way round a rule the step under the old id
// has lost.
//
// None when the new version has neither.
func stepsInPlaceOf(id string, targetNodes map[string]models.FlowNode, nodeMapping map[string]string) []models.FlowNode {
	var steps []models.FlowNode
	to := mapNode(nodeMapping, id)
	if landed, lands := targetNodes[to]; lands {
		steps = append(steps, landed)
	}
	if to != id {
		if kept, stays := targetNodes[id]; stays {
			steps = append(steps, kept)
		}
	}
	return steps
}

// namesLoosened sorts the steps a source rule names into those some rule of
// inItsPlace no longer names (unnamed), those every such rule names by the id
// the mapping sends them to while finished work does not follow that mapping
// (unfollowed), and those every such rule still names while the new version
// no longer has the step (gone), each by the name the source gives it.
// renames is what finished work follows.
//
// unnamed and unfollowed are both a rule that no longer finds who performed
// the step, and they are said apart because their causes are: in the first
// the new version dropped the name; in the second it kept the rule, under
// the step's new id, and what is lost is that the work already done stays
// under the old one.
func namesLoosened(
	barred []string,
	inItsPlace []models.FlowNode,
	renames, nodeMapping map[string]string,
	sourceNodes, targetNodes map[string]models.FlowNode,
) (unnamed, unfollowed, gone []string) {
	namedByAll := func(id string) bool {
		return !slices.ContainsFunc(inItsPlace, func(step models.FlowNode) bool {
			return !slices.Contains(splitNodeList(stringProperty(step.Properties, SeparationOfDutiesKey)), id)
		})
	}
	for _, other := range barred {
		now := mapNode(renames, other)
		_, wasAStep := sourceNodes[other]
		_, isAStep := targetNodes[now]
		switch called := "“" + stepCalled(sourceNodes[other], other) + "”"; {
		case namedByAll(now) && wasAStep && !isAStep:
			gone = append(gone, called)
		case namedByAll(now):
		case mapNode(nodeMapping, other) != now && namedByAll(mapNode(nodeMapping, other)):
			unfollowed = append(unfollowed, called)
		default:
			unnamed = append(unnamed, called)
		}
	}
	return unnamed, unfollowed, gone
}

// notPassedBy counts the instances that have not performed a step.
func notPassedBy(instances []models.ProcessInstanceModel, nodeID string) int {
	notPassed := 0
	for _, instance := range instances {
		if !slices.Contains(instance.CompletedNodes, nodeID) {
			notPassed++
		}
	}
	return notPassed
}

// firstNamed is a list of names for a sentence: the first few one by one, and
// the rest as a number.
func firstNamed(names []string) string {
	named := strings.Join(names[:min(len(names), controlsNamed)], ", ")
	if more := len(names) - controlsNamed; more > 0 {
		named += fmt.Sprintf(", and %d more", more)
	}
	return named
}

// whyNoLongerHolds says why a request does not cover a migration, and says
// nothing when it does.
//
// A request covers a migration when the migration is the policy that was
// asked for (the fingerprint) and acts on no instance the request did not
// show (covered, the running instances of the plan about to be applied). It
// is asked twice with the same answer expected: when a second administrator
// approves, and again by the apply, of the very plan it runs.
//
// Fewer instances is still covered — one that finished or was moved since is
// simply not acted on. One more is not: nobody was shown it. A request with
// no fingerprint covers nothing.
func whyNoLongerHolds(request entities.DeviationRequest, fingerprint string, covered []uuid.UUID) string {
	if request.Fingerprint == "" || request.Fingerprint != fingerprint {
		return "the migration is no longer the one that was asked for: its mapping, its decisions, " +
			"the controls it drops or the instances it names changed"
	}
	shown := make(map[uuid.UUID]struct{}, len(request.ApprovedInstances))
	for _, id := range request.ApprovedInstances {
		shown[id] = struct{}{}
	}
	var newcomers []string
	for _, id := range covered {
		if _, ok := shown[id]; !ok {
			newcomers = append(newcomers, id.String())
		}
	}
	if len(newcomers) == 0 {
		return ""
	}
	slices.Sort(newcomers)
	named := strings.Join(newcomers[:min(len(newcomers), newcomersNamed)], ", ")
	if more := len(newcomers) - newcomersNamed; more > 0 {
		named += fmt.Sprintf(", and %d more", more)
	}
	return fmt.Sprintf("%d instance(s) reached the version being migrated from after it was asked for (%s)", len(newcomers), named)
}
