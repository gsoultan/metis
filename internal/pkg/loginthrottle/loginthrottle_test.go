package loginthrottle

import (
	"testing"
	"time"
)

var start = time.Date(2026, 9, 20, 12, 0, 0, 0, time.UTC)

func TestTypingBadlyCostsNothing(t *testing.T) {
	th := New()
	now := start

	// The whole point of the free attempts: somebody who mistypes twice and
	// gets it right on the third go must never meet this. FreeAttempts
	// failures cost nothing, so the attempt straight after them is still free.
	for range FreeAttempts {
		if _, wait := th.RetryAfter("ada", now); wait {
			t.Fatalf("made a person wait within the first %d attempts", FreeAttempts)
		}
		th.Failed("ada", now)
	}
	if _, wait := th.RetryAfter("ada", now); wait {
		t.Errorf("the attempt after %d failures was slowed; that many are meant to be free", FreeAttempts)
	}

	// The one after that is where it starts.
	th.Failed("ada", now)
	if _, wait := th.RetryAfter("ada", now); !wait {
		t.Error("the attempt after the free ones was not slowed at all")
	}
}

func TestEachFailureCostsMoreThanTheLast(t *testing.T) {
	th := New()
	now := start
	for range FreeAttempts + 1 {
		th.Failed("ada", now)
	}

	first, wait := th.RetryAfter("ada", now)
	if !wait {
		t.Fatal("expected a wait")
	}

	th.Failed("ada", now)
	second, _ := th.RetryAfter("ada", now)

	if second <= first {
		t.Errorf("the delay did not grow: %s then %s", first, second)
	}
}

func TestTheDelayIsCapped(t *testing.T) {
	th := New()
	now := start
	for range 40 {
		th.Failed("ada", now)
	}
	got, _ := th.RetryAfter("ada", now)
	if got > MaxDelay {
		t.Errorf("delay %s exceeds the %s cap; a typo should not lock somebody out for a day", got, MaxDelay)
	}
}

// The property the whole package exists for: the penalty follows the account,
// not the connection. A caller changing address must not reset it, because
// changing address is exactly what credential stuffing does.
func TestThePenaltyFollowsTheAccountNotTheCaller(t *testing.T) {
	th := New()
	now := start
	for range FreeAttempts + 2 {
		th.Failed("ada", now)
	}

	// Nothing in this package takes an address, which is the point — but assert
	// the behaviour rather than the signature, so a later change that threads
	// one through cannot quietly make the penalty per-connection.
	if _, wait := th.RetryAfter("ada", now); !wait {
		t.Error("an account with five consecutive failures was not slowed")
	}
	if _, wait := th.RetryAfter("grace", now); wait {
		t.Error("an unrelated account was slowed; the penalty is not account-scoped")
	}
}

// A hard lockout would be a denial of service against any guessable username.
// Waiting the delay must always let the owner back in.
func TestWaitingIsEnoughToBeLetBackIn(t *testing.T) {
	th := New()
	now := start
	for range FreeAttempts + 1 {
		th.Failed("ada", now)
	}

	delay, wait := th.RetryAfter("ada", now)
	if !wait {
		t.Fatal("expected a wait")
	}
	if _, stillWaiting := th.RetryAfter("ada", now.Add(delay)); stillWaiting {
		t.Error("the account was still refused after serving its delay; this is a lockout, not a backoff")
	}
}

// Checking must not extend the penalty, or an attacker who keeps knocking can
// hold an account shut indefinitely.
func TestCheckingDoesNotExtendThePenalty(t *testing.T) {
	th := New()
	now := start
	for range FreeAttempts + 1 {
		th.Failed("ada", now)
	}

	first, _ := th.RetryAfter("ada", now)
	for range 20 {
		th.RetryAfter("ada", now)
	}
	after, _ := th.RetryAfter("ada", now)

	if after != first {
		t.Errorf("repeated checks changed the wait from %s to %s", first, after)
	}
}

func TestSuccessClearsTheCount(t *testing.T) {
	th := New()
	now := start
	for range FreeAttempts + 3 {
		th.Failed("ada", now)
	}
	th.Succeeded("ada")

	if _, wait := th.RetryAfter("ada", now); wait {
		t.Error("an account that signed in successfully is still being slowed")
	}
}

func TestAnIdleAccountIsForgotten(t *testing.T) {
	th := New()
	for range FreeAttempts + 3 {
		th.Failed("ada", start)
	}

	later := start.Add(Forget + time.Minute)
	if _, wait := th.RetryAfter("ada", later); wait {
		t.Errorf("an account idle for %s is still being slowed", Forget)
	}
}

// The key is attacker-supplied, so the map must not grow without bound.
func TestTheMapIsBounded(t *testing.T) {
	th := New()
	for i := range Capacity * 2 {
		th.Failed(string(rune('a'+i%26))+time.Duration(i).String(), start)
	}
	if got := th.accounts.Len(); got > Capacity {
		t.Errorf("tracking %d accounts against a capacity of %d", got, Capacity)
	}
}
