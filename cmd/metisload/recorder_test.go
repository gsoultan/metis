package main

import (
	"errors"
	"net/http"
	"testing"
	"time"
)

func TestFailureClassTellsSheddingFromFailure(t *testing.T) {
	for _, tc := range []struct {
		err  error
		want string
	}{
		{&apiError{status: http.StatusTooManyRequests}, "429 rate limited"},
		{&apiError{status: http.StatusServiceUnavailable}, "503 shed by backpressure"},
		{&apiError{status: http.StatusInternalServerError, body: "boom"}, "5xx: boom"},
		{&apiError{status: http.StatusOK, body: "forbidden"}, "refused: forbidden"},
		{errors.New("connection reset"), "transport: connection reset"},
	} {
		if got := failureClass(tc.err); got != tc.want {
			t.Errorf("failureClass(%v) = %q, want %q", tc.err, got, tc.want)
		}
	}
}

// A shed call's latency is not a latency: a 503 answered in a microsecond would
// make an overloaded server look fast.
func TestFailedCallsDoNotCountTowardLatency(t *testing.T) {
	r := newRecorder()
	for i := 1; i <= 100; i++ {
		r.observe(opStart, time.Duration(i)*time.Millisecond, nil)
	}
	r.observe(opStart, time.Microsecond, &apiError{status: http.StatusServiceUnavailable})

	got := r.summarize(opStart)
	if got.ok != 100 || got.failed != 1 {
		t.Fatalf("ok %d failed %d, want 100 and 1", got.ok, got.failed)
	}
	if got.p50 != 50*time.Millisecond || got.p99 != 99*time.Millisecond || got.max != 100*time.Millisecond {
		t.Errorf("p50 %v p99 %v max %v, want 50ms 99ms 100ms", got.p50, got.p99, got.max)
	}
}

func TestShedIsOnlyNotNow(t *testing.T) {
	if !shed(&apiError{status: http.StatusServiceUnavailable}) || !shed(&apiError{status: http.StatusTooManyRequests}) {
		t.Error("429 and 503 should be retried")
	}
	if shed(&apiError{status: http.StatusInternalServerError}) || shed(&apiError{status: http.StatusOK, body: "forbidden"}) {
		t.Error("a failure or a refusal must not be retried")
	}
}
