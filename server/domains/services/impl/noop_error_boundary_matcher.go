package impl

import "github.com/gsoultan/metis/server/domains/entities"

// NoOpErrorBoundaryMatcher is a Null Object implementation of ErrorBoundaryMatcher.
// It never matches any boundary event, effectively disabling error boundary routing.
// Use this in tests or single-node deployments where error boundary events are not needed.
type NoOpErrorBoundaryMatcher struct{}

// NewNoOpErrorBoundaryMatcher creates a new NoOpErrorBoundaryMatcher.
func NewNoOpErrorBoundaryMatcher() *NoOpErrorBoundaryMatcher {
	return &NoOpErrorBoundaryMatcher{}
}

func (m *NoOpErrorBoundaryMatcher) Matches(_ error, _ entities.Node) bool {
	return false
}
