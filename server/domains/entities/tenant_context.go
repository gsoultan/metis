package entities

import "context"

type tenantContextKey struct{}

// TenantContext carries the active tenant identifier through a request context.
// It is injected by TenantMiddleware and validated on every repository query.
type TenantContext struct {
	// TenantID is the unique identifier for the tenant (maps to OrganizationID).
	TenantID string
}

// WithTenantContext returns a new context with the given TenantContext attached.
func WithTenantContext(ctx context.Context, tc TenantContext) context.Context {
	return context.WithValue(ctx, tenantContextKey{}, tc)
}

// TenantContextFrom extracts the TenantContext from the context.
// Returns the zero value and false if no TenantContext is present.
func TenantContextFrom(ctx context.Context) (TenantContext, bool) {
	tc, ok := ctx.Value(tenantContextKey{}).(TenantContext)
	return tc, ok
}

type systemContextKey struct{}

// WithSystemContext marks work that legitimately spans every tenant.
//
// The engine, the job worker, the message consumers and the migration runner all
// operate across the whole installation: a job worker that could only see one
// organization's jobs would be useless. Until now they were recognised by having
// *no* tenant identity at all, which meant "background work" and "somebody
// forgot to resolve the tenant" were indistinguishable — and both got
// unrestricted access.
//
// Marking system work explicitly is what lets the missing case become a denial.
// Apply it at the point where background work begins, never inside a request.
func WithSystemContext(ctx context.Context) context.Context {
	return context.WithValue(ctx, systemContextKey{}, true)
}

// IsSystemContext reports whether this context was marked as system work.
func IsSystemContext(ctx context.Context) bool {
	marked, ok := ctx.Value(systemContextKey{}).(bool)
	return ok && marked
}
