package impl

import (
	"testing"
	"time"
)

// The schedule grows, so a downstream that has fallen over gets more room each
// time rather than the same fixed cadence for as long as it is down.
func TestRetryDelayGrows(t *testing.T) {
	// Jitter makes any single pair a coin flip, so compare the middles of many
	// draws rather than two samples.
	median := func(retries int) time.Duration {
		const draws = 201
		samples := make([]time.Duration, draws)
		for i := range samples {
			samples[i] = retryDelay(retries)
		}
		// A mean is enough here and needs no sort: the jitter is symmetric.
		var total time.Duration
		for _, s := range samples {
			total += s
		}
		return total / draws
	}

	previous := median(1)
	for attempt := 2; attempt <= 5; attempt++ {
		current := median(attempt)
		if current <= previous {
			t.Errorf("attempt %d waits %v, no longer than attempt %d's %v", attempt, current, attempt-1, previous)
		}
		previous = current
	}
}

// Growth has to stop somewhere: past a quarter of an hour the delay is no
// longer giving a service room to recover, it is losing a business commitment
// in a queue.
func TestRetryDelayIsBounded(t *testing.T) {
	for _, retries := range []int{10, 32, 64, 1000} {
		got := retryDelay(retries)
		if got > maxRetryDelay+time.Duration(float64(maxRetryDelay)*jitterFraction) {
			t.Errorf("attempt %d waits %v, past the bound", retries, got)
		}
		if got <= 0 {
			t.Errorf("attempt %d waits %v; a non-positive delay is a hot loop", retries, got)
		}
	}
}

// The reason jitter exists. Two thousand instances that failed against the same
// outage must not come back at the same moment — that is a synchronised wave of
// traffic arriving when the service is least able to take it.
func TestRetryDelayIsSpreadOut(t *testing.T) {
	const instances = 500
	seen := map[time.Duration]int{}
	var min, max time.Duration

	for i := 0; i < instances; i++ {
		delay := retryDelay(1)
		seen[delay]++
		if min == 0 || delay < min {
			min = delay
		}
		if delay > max {
			max = delay
		}
	}

	if len(seen) < instances/2 {
		t.Errorf("%d instances produced only %d distinct delays; they would retry in step", instances, len(seen))
	}
	if spread := max - min; spread < time.Second {
		t.Errorf("the delays span %v; that is not enough to break a thundering herd", spread)
	}
}

// A first retry is soon, because most failures are over inside a minute.
func TestFirstRetryIsSoon(t *testing.T) {
	for i := 0; i < 100; i++ {
		if got := retryDelay(1); got > time.Minute {
			t.Fatalf("the first retry waits %v; a connection reset does not need a minute", got)
		}
	}
}

// retries is the count already spent, and a caller that has not counted yet
// must not be handed a zero delay.
func TestRetryDelayTreatsZeroAsTheFirstAttempt(t *testing.T) {
	if got := retryDelay(0); got < time.Second {
		t.Errorf("delay for a zero count = %v, want the first attempt's wait", got)
	}
}
