package contracts

import "context"

// RequestExecutor is a ConnectorExecutor that can be told what the step wants.
//
// Optional, and found by type assertion, so every executor written before it
// is untouched: one that does not implement it is called through Execute with
// the process variables, exactly as before.
type RequestExecutor interface {
	ConnectorExecutor
	ExecuteRequest(ctx context.Context, config map[string]any, req ConnectorRequest) (map[string]any, error)
}
