package impl

import (
	"github.com/gsoultan/metis/server/repositories/models"
)

// stepPlace is where a step stands in a version: the steps its incoming flows
// come from and the steps its outgoing flows go to.
type stepPlace struct {
	from, to map[string]struct{}
}

// stepPlaces is where every step of a version stands, read off its sequence
// flows — nested ones included — in one pass.
func stepPlaces(def models.ProcessDefinitionModel) map[string]stepPlace {
	places := map[string]stepPlace{}
	at := func(id string) stepPlace {
		place, known := places[id]
		if !known {
			place = stepPlace{from: map[string]struct{}{}, to: map[string]struct{}{}}
			places[id] = place
		}
		return place
	}
	for _, flow := range allFlows(def) {
		at(flow.TargetRef).from[flow.SourceRef] = struct{}{}
		at(flow.SourceRef).to[flow.TargetRef] = struct{}{}
	}
	return places
}

// renamedInPlace is the part of a mapping that gives a step a new id and
// leaves it where it stands: a rename (renamedSteps) of a step whose
// neighbours are the same in both versions. It is the notion that decides
// whether a mapping needs anybody's approval: a step renamed in place is the
// same step, and asks nobody on its own; every other mapping of a step sends
// its work somewhere else.
//
// Ids alone do not tell the two apart. A version that drops "prepare" and
// adds "file" after the signature makes "prepare → file" a rename by ids — the
// old one is gone, the new one is new — and it takes whoever waits at prepare
// past everything between.
//
// "The same neighbours" is asked of the step's own sequence flows and of
// nothing further: the set of steps its incoming flows come from, and the set
// its outgoing flows go to, in the source — each written as the mapping
// renames or moves it — against the same two sets of the step it is mapped
// to in the target. It is a comparison of two steps' own flows in two
// definitions. Nothing is walked, and what lies beyond a neighbour is not
// looked at.
//
// Three shapes have no place of that kind to compare, and a mapping of one
// is not counted as in place — the direction that asks:
//   - a boundary event, which stands on the step it is attached to and has no
//     incoming flow;
//   - an event sub-process, which nothing flows into or out of;
//   - any step with no sequence flow at all, in either version.
func renamedInPlace(
	source, target models.ProcessDefinitionModel,
	sourceNodes, targetNodes map[string]models.FlowNode,
	nodeMapping map[string]string,
) map[string]string {
	inPlace := map[string]string{}
	renames := renamedSteps(sourceNodes, targetNodes, nodeMapping)
	if len(renames) == 0 {
		return inPlace
	}
	stood, stands := stepPlaces(source), stepPlaces(target)
	for from, to := range renames {
		if hasNoPlace(sourceNodes[from]) || hasNoPlace(targetNodes[to]) {
			continue
		}
		was, is := stood[from], stands[to]
		if len(was.from)+len(was.to) == 0 || len(is.from)+len(is.to) == 0 {
			continue
		}
		if sameSteps(was.from, is.from, nodeMapping) && sameSteps(was.to, is.to, nodeMapping) {
			inPlace[from] = to
		}
	}
	return inPlace
}

// hasNoPlace reports whether a step is of a shape whose sequence flows do
// not say where it stands.
func hasNoPlace(node models.FlowNode) bool {
	return node.Type == models.BoundaryEvent || node.IsEventSubProcess
}

// sameSteps reports whether the source's steps, each under the id the mapping
// gives it, are exactly the target's.
func sameSteps(source, target map[string]struct{}, nodeMapping map[string]string) bool {
	mapped := make(map[string]struct{}, len(source))
	for id := range source {
		mapped[mapNode(nodeMapping, id)] = struct{}{}
	}
	if len(mapped) != len(target) {
		return false
	}
	for id := range mapped {
		if _, there := target[id]; !there {
			return false
		}
	}
	return true
}
