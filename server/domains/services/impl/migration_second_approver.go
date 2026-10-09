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
// Exactly two things are: a step skipped — nobody performs it — and a control
// the migration takes from instances that have not passed it. A cancel ends
// an instance and a hold stops one; neither loosens a rule on work that goes
// on, and neither is here.
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
			cmp.Or(sourceNodes[nodeID].Name, nodeID)))
	}
	for _, hold := range holds {
		notPassed := 0
		for _, instance := range running {
			if !slices.Contains(instance.CompletedNodes, hold.NodeID) {
				notPassed++
			}
		}
		if notPassed == 0 {
			continue
		}
		reasons = append(reasons, fmt.Sprintf("“%s” carries a control %d instance(s) have not passed, and they never would",
			cmp.Or(sourceNodes[hold.NodeID].Name, hold.Name, hold.NodeID), notPassed))
	}
	slices.Sort(reasons)
	return reasons
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
