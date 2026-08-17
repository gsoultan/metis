// Package idempotency carries the key that makes a repeated outbound call safe.
//
// It sits here rather than beside the job service because the two ends are far
// apart: the job service knows what a unit of work is, and the HTTP connector —
// three layers down, in another package — is the one that has to put a header on
// a request. A shared package is the only way to join them without one importing
// the other.
package idempotency

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"fmt"

	"github.com/google/uuid"
)

// contextKey carries the key for one outbound call.
//
// It travels in the context rather than through every signature because the
// connectors and the HTTP runner are three layers below the job service and
// neither knows nor should know what a job attempt is. What they need is one
// string, on the way out, and only when there is one.
type contextKey struct{}

// WithKey marks a context as belonging to one outbound call.
func WithKey(ctx context.Context, key string) context.Context {
	if key == "" {
		return ctx
	}
	return context.WithValue(ctx, contextKey{}, key)
}

// KeyFrom returns the key for the call being made, if there is one.
func KeyFrom(ctx context.Context) (string, bool) {
	key, ok := ctx.Value(contextKey{}).(string)
	return key, ok && key != ""
}

// ForServiceCall is what the downstream sees, twice, if a call is
// repeated.
//
// It is derived rather than random, and derived from nothing that changes
// between attempts: one service task, in one instance, on one iteration of a
// multi-instance node, is one unit of work however many times the engine tries
// it. A random key would make every retry look like a new request, which is the
// behaviour this exists to prevent.
//
// Hashed rather than concatenated because a node ID comes from a deployed BPMN
// file — untrusted input, of no bounded length, and free to contain anything a
// header value cannot. The hash is truncated to 32 characters: this identifies a
// request within one deployment's traffic, not a document within a corpus, and
// 160 bits is far past any collision that matters here.
func ForServiceCall(instanceID uuid.UUID, nodeID, iterationID string) string {
	sum := sha256.Sum256(fmt.Appendf(nil, "%s\x00%s\x00%s", instanceID, nodeID, iterationID))
	return "gobpm-" + base64.RawURLEncoding.EncodeToString(sum[:])[:32]
}

// Header is the name an idempotent HTTP API is expected to read. Stripe made it
// the convention and the industry followed; there is no standard, and this is
// the closest thing to one.
const Header = "Idempotency-Key"
