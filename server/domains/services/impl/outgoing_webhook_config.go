package impl

import "time"

// How the outgoing webhook worker is sized. Each knob has the job worker's
// shape: an environment variable, and a default that holds on a small
// installation.
const (
	envOutgoingWebhookWorkers = "METIS_OUTGOING_WEBHOOK_WORKERS"
	envOutgoingWebhookPoll    = "METIS_OUTGOING_WEBHOOK_POLL_INTERVAL"
	envOutgoingWebhookTimeout = "METIS_OUTGOING_WEBHOOK_TIMEOUT"

	defaultOutgoingWebhookWorkers = 8
	defaultOutgoingWebhookPoll    = 2 * time.Second
	// A receiver is expected to answer quickly and do its work afterwards;
	// one that takes longer is treated as not having answered.
	defaultOutgoingWebhookTimeout = 10 * time.Second
)

// OutgoingWebhookSettings is how the worker posts.
type OutgoingWebhookSettings struct {
	// Workers is how many deliveries are posted at once, per database.
	Workers int
	// PollInterval is how often the queue is asked for due deliveries.
	PollInterval time.Duration
	// Timeout bounds one attempt, connection to last byte.
	Timeout time.Duration
	// Lease is how long a claimed delivery is held: long enough that an
	// attempt still running is never claimed by a second worker.
	Lease time.Duration
}

// ResolveOutgoingWebhookSettings reads the worker's settings.
func ResolveOutgoingWebhookSettings() OutgoingWebhookSettings {
	settings := OutgoingWebhookSettings{
		Workers:      envPositiveInt(envOutgoingWebhookWorkers, defaultOutgoingWebhookWorkers),
		PollInterval: envPositiveDuration(envOutgoingWebhookPoll, defaultOutgoingWebhookPoll),
		Timeout:      envPositiveDuration(envOutgoingWebhookTimeout, defaultOutgoingWebhookTimeout),
	}
	// Six times the attempt's budget: an attempt that has not settled by then
	// has a worker that died, not one that is slow.
	settings.Lease = 6 * settings.Timeout
	return settings
}

// outgoingRetrySchedule is how long a delivery waits after each failed
// attempt: about a day in all, which covers a receiver's overnight outage and
// a deploy that went wrong, and is short enough that a receiver gone for good
// is given up on the same day. Attempt n waits the n-th entry; there is one
// attempt more than there are entries, and the last failure is final.
var outgoingRetrySchedule = []time.Duration{
	time.Minute,
	5 * time.Minute,
	15 * time.Minute,
	30 * time.Minute,
	time.Hour,
	2 * time.Hour,
	4 * time.Hour,
	6 * time.Hour,
	8 * time.Hour,
}

// maxOutgoingAttempts is how many times a delivery is tried.
func maxOutgoingAttempts() int { return len(outgoingRetrySchedule) + 1 }
