package impl

import (
	"errors"

	"github.com/gsoultan/gobpm/server/domains/entities"
)

// ErrorBoundaryMatcherImpl implements the ErrorBoundaryMatcher contract using
// standard BPMN matching rules: an empty ErrorCode on the boundary node
// catches all errors; a non-empty ErrorCode matches only CatchableError with
// an identical code.
type ErrorBoundaryMatcherImpl struct{}

// NewErrorBoundaryMatcher creates a new ErrorBoundaryMatcherImpl.
func NewErrorBoundaryMatcher() *ErrorBoundaryMatcherImpl {
	return &ErrorBoundaryMatcherImpl{}
}

// Matches returns true if the given error should be caught by the boundary node.
func (m *ErrorBoundaryMatcherImpl) Matches(err error, boundaryNode entities.Node) bool {
	if boundaryNode.Type != entities.BoundaryEvent || !boundaryNode.IsErrorBoundary() {
		return false
	}
	// An error boundary event with no code catches every failure (BPMN 2.0
	// §10.4.4). Scoping that to error events is what stops it swallowing
	// failures meant for a timer or message boundary on the same activity.
	code := boundaryNode.ErrorCodeValue()
	if code == "" || code == "*" {
		return true
	}
	var catchable *entities.CatchableError
	if errors.As(err, &catchable) {
		return catchable.Code == code
	}
	return false
}
