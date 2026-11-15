package entities

import "time"

// RetryAdvisor is a failure that says whether trying again could work, and how
// long the partner asked to be left alone first.
//
// The job engine retries every other failure on its own schedule. A failure the
// partner has already called final — a 400, a declined card, a refused
// credential — is not helped by three more attempts, and a 429 that came with a
// Retry-After is helped only by waiting at least that long.
type RetryAdvisor interface {
	error
	// RetryAdvice reports whether another attempt could succeed and, when the
	// partner said, the least time to wait before it. A zero wait means the
	// partner named none.
	RetryAdvice() (retryable bool, wait time.Duration)
}
