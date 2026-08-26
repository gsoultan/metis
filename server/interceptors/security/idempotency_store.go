package security

import (
	"context"
	"net/http"
	"time"
)

// StoredResponse is a completed response, kept so a retry can be answered
// without doing the work again.
type StoredResponse struct {
	StatusCode int
	Header     http.Header
	Body       []byte
}

// ClaimOutcome is what a caller learns when it presents a key.
//
// The four states are distinct on purpose. "Nobody has this" and "somebody is
// running it right now" look the same to a naive cache and must not be: the
// first means execute, the second means wait. Collapsing them is how a
// duplicate write happens under concurrency.
type ClaimOutcome struct {
	// Owned means this caller reserved the key and must execute, then Complete.
	Owned bool

	// Conflict means the key is held for a *different* request. A client
	// reusing one key for two payloads is a bug, and serving either answer
	// would hide it.
	Conflict bool

	// Response is set when a completed record already exists: replay it.
	Response *StoredResponse
}

// InFlight reports the remaining case — the key is claimed by somebody else and
// no answer exists yet, so the caller must wait rather than execute.
func (o ClaimOutcome) InFlight() bool {
	return !o.Owned && !o.Conflict && o.Response == nil
}

// IdempotencyStore records what a key has already produced.
//
// It is an interface because the answer differs by deployment: one replica can
// hold this in memory and coordinate with channels, while more than one needs a
// record both can see. The in-memory implementation is not a stub — it is the
// right choice for a single replica, which is the supported topology.
type IdempotencyStore interface {
	// Claim reserves key for this caller, or reports what is already known.
	Claim(ctx context.Context, key, requestHash string) (ClaimOutcome, error)

	// Complete records the response for a key this caller owns.
	Complete(ctx context.Context, key string, response StoredResponse) error

	// Abandon releases a claim that will never complete, so a caller whose
	// handler panicked does not leave every retry waiting for an answer that is
	// never coming.
	Abandon(ctx context.Context, key string) error

	// Await blocks until key has a response, ctx ends, or the wait budget is
	// spent. A nil response with a nil error means "still not finished" — the
	// caller decides what to tell the client.
	Await(ctx context.Context, key string) (*StoredResponse, error)
}

// idempotencyWaitBudget bounds how long a duplicate request waits for the
// original to finish.
//
// It is deliberately shorter than a typical client timeout: a caller that waits
// the full budget and gets an answer is served, and one that does not is told
// to retry, which is safe precisely because the key makes the retry idempotent.
const idempotencyWaitBudget = 10 * time.Second

// idempotencyPollInterval is how often a waiter re-reads a claimed record.
//
// Only the database-backed store polls; the in-memory one closes a channel.
// Polling is the price of coordinating across processes without a broker, and
// 50ms keeps the added latency below what a caller would notice while leaving
// the database mostly alone.
const idempotencyPollInterval = 50 * time.Millisecond
