package contracts

import "github.com/gsoultan/metis/server/domains/entities"

// ErrorBoundaryMatcher determines whether a given error should be caught by
// a specific boundary event node. Implementations apply the BPMN matching
// rule: an empty errorCode on the boundary event catches all errors; a
// non-empty errorCode matches only errors with the same code.
type ErrorBoundaryMatcher interface {
	Matches(err error, boundaryNode entities.Node) bool
}
