package security

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestNewBackpressureInterceptor(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name              string
		maxInFlight       int
		maxQueued         int
		expectedInFlight  int
		expectedQueueSize int
	}{
		{
			name:              "uses defaults when limits are not positive",
			maxInFlight:       0,
			maxQueued:         0,
			expectedInFlight:  defaultMaxInFlightRequests,
			expectedQueueSize: defaultMaxQueuedRequests,
		},
		{
			name:              "uses explicit limits when provided",
			maxInFlight:       4,
			maxQueued:         9,
			expectedInFlight:  4,
			expectedQueueSize: 9,
		},
	}

	for _, testCase := range testCases {

		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			interceptor := NewBackpressureInterceptor(testCase.maxInFlight, testCase.maxQueued).(*backpressureInterceptor)

			if got := cap(interceptor.inFlight); got != testCase.expectedInFlight {
				t.Fatalf("expected in-flight capacity %d, got %d", testCase.expectedInFlight, got)
			}

			if got := cap(interceptor.queue); got != testCase.expectedQueueSize {
				t.Fatalf("expected queue capacity %d, got %d", testCase.expectedQueueSize, got)
			}
		})
	}
}

func TestBackpressureInterceptorAllowsRequestWhenCapacityAvailable(t *testing.T) {
	t.Parallel()

	interceptor := NewBackpressureInterceptor(1, 1).(*backpressureInterceptor)
	called := false
	handler := interceptor.Wrap(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		called = true
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/api/v1/setup/status", nil)
	res := httptest.NewRecorder()
	handler.ServeHTTP(res, req)

	if res.Code != http.StatusOK {
		t.Fatalf("expected status %d, got %d", http.StatusOK, res.Code)
	}

	if !called {
		t.Fatalf("expected wrapped handler to be called")
	}

	if len(interceptor.queue) != 0 {
		t.Fatalf("expected queue to be empty after request")
	}

	if len(interceptor.inFlight) != 0 {
		t.Fatalf("expected in-flight slots to be released after request")
	}
}

func TestBackpressureInterceptorRejectsWhenQueueIsFull(t *testing.T) {
	t.Parallel()

	interceptor := NewBackpressureInterceptor(1, 1).(*backpressureInterceptor)
	interceptor.inFlight <- struct{}{}
	interceptor.queue <- struct{}{}

	handler := interceptor.Wrap(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/api/v1/setup/status", nil)
	res := httptest.NewRecorder()
	handler.ServeHTTP(res, req)

	if res.Code != http.StatusServiceUnavailable {
		t.Fatalf("expected status %d, got %d", http.StatusServiceUnavailable, res.Code)
	}

	if retryAfter := res.Header().Get("Retry-After"); retryAfter == "" {
		t.Fatalf("expected Retry-After header to be set")
	}
}

func TestBackpressureInterceptorWaitsForInFlightSlot(t *testing.T) {
	t.Parallel()

	interceptor := NewBackpressureInterceptor(1, 1).(*backpressureInterceptor)
	interceptor.inFlight <- struct{}{}

	handlerStarted := make(chan struct{}, 1)
	handler := interceptor.Wrap(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		handlerStarted <- struct{}{}
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/api/v1/setup/status", nil)
	res := httptest.NewRecorder()

	done := make(chan struct{})
	go func() {
		handler.ServeHTTP(res, req)
		close(done)
	}()

	select {
	case <-handlerStarted:
		t.Fatalf("handler should not start while in-flight slot is occupied")
	case <-time.After(30 * time.Millisecond):
	}

	<-interceptor.inFlight

	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatalf("timed out waiting for request completion")
	}

	if res.Code != http.StatusOK {
		t.Fatalf("expected status %d, got %d", http.StatusOK, res.Code)
	}

	if len(interceptor.queue) != 0 {
		t.Fatalf("expected queue to be empty after request")
	}
}

func TestBackpressureInterceptorReturnsTimeoutWhenContextCanceledInQueue(t *testing.T) {
	t.Parallel()

	interceptor := NewBackpressureInterceptor(1, 1).(*backpressureInterceptor)
	interceptor.inFlight <- struct{}{}
	defer func() { <-interceptor.inFlight }()

	called := false
	handler := interceptor.Wrap(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		called = true
		w.WriteHeader(http.StatusOK)
	}))

	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Millisecond)
	defer cancel()

	req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/api/v1/setup/status", nil).WithContext(ctx)
	res := httptest.NewRecorder()
	handler.ServeHTTP(res, req)

	if res.Code != http.StatusRequestTimeout {
		t.Fatalf("expected status %d, got %d", http.StatusRequestTimeout, res.Code)
	}

	if called {
		t.Fatalf("expected wrapped handler to not be called")
	}

	if len(interceptor.queue) != 0 {
		t.Fatalf("expected queue to be empty after context cancellation")
	}
}
