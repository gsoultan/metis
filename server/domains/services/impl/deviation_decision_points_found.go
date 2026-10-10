package impl

import "github.com/gsoultan/metis/server/domains/entities"

// decisionPointsFound is what reading a definition for its decision points
// answers (decisionPointsReading): the points, and — once for all of them —
// what they are missing.
//
// A point keeps only the first few names of each of its lists, with counts
// of the rest, so what is held is in proportion to the number of points and
// not to that times the length of the form. The whole of what is missing is
// held here instead, one set for the definition: no bigger than the form,
// however many points miss the same fields.
type decisionPointsFound struct {
	// points is every point, sorted by node id and then kind.
	points []entities.DecisionPoint
	// missing is every field the step declares that some point reads and the
	// waiver does not supply. What a plan refuses for is this, never the
	// short list a point shows.
	missing map[string]struct{}
	// missingAt is how many points are missing at least one value.
	missingAt int
}
