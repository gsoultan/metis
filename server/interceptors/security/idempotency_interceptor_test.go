package security

import (
	"bytes"
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"
)

func TestNewIdempotencyInterceptor(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name        string
		ttl         time.Duration
		expectedTTL time.Duration
	}{
		{
			name:        "uses default ttl when input is not positive",
			ttl:         0,
			expectedTTL: defaultIdempotencyTTL,
		},
		{
			name:        "uses explicit ttl when provided",
			ttl:         45 * time.Second,
			expectedTTL: 45 * time.Second,
		},
	}

	for _, testCase := range testCases {

		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			interceptor := NewIdempotencyInterceptor(testCase.ttl).(*idempotencyInterceptor)
			if interceptor.ttl != testCase.expectedTTL {
				t.Fatalf("expected ttl %s, got %s", testCase.expectedTTL, interceptor.ttl)
			}
		})
	}
}

func TestIdempotencyInterceptorPassThroughWithoutKey(t *testing.T) {
	t.Parallel()

	var calls atomic.Int32
	handler := NewIdempotencyInterceptor(time.Minute).Wrap(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		sequence := calls.Add(1)
		w.WriteHeader(http.StatusCreated)
		_, _ = fmt.Fprintf(w, "call-%d", sequence)
	}))

	requestOne := httptest.NewRequestWithContext(t.Context(), http.MethodPost, "/api/v1/tasks", bytes.NewReader([]byte(`{"task":"a"}`)))
	responseOne := httptest.NewRecorder()
	handler.ServeHTTP(responseOne, requestOne)

	requestTwo := httptest.NewRequestWithContext(t.Context(), http.MethodPost, "/api/v1/tasks", bytes.NewReader([]byte(`{"task":"a"}`)))
	responseTwo := httptest.NewRecorder()
	handler.ServeHTTP(responseTwo, requestTwo)

	if got := calls.Load(); got != 2 {
		t.Fatalf("expected handler to be called twice without idempotency key, got %d", got)
	}

	if responseTwo.Body.String() != "call-2" {
		t.Fatalf("expected second response body to be from second call, got %q", responseTwo.Body.String())
	}
}

func TestIdempotencyInterceptorReplaysSuccessfulResponse(t *testing.T) {
	t.Parallel()

	var calls atomic.Int32
	handler := NewIdempotencyInterceptor(time.Minute).Wrap(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		sequence := calls.Add(1)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		_, _ = fmt.Fprintf(w, `{"call":%d}`, sequence)
	}))

	requestOne := httptest.NewRequestWithContext(t.Context(), http.MethodPost, "/api/v1/processes", bytes.NewReader([]byte(`{"name":"demo"}`)))
	requestOne.Header.Set(idempotencyKeyHeader, "create-process-1")
	requestOne.Header.Set("Content-Type", "application/json")
	responseOne := httptest.NewRecorder()
	handler.ServeHTTP(responseOne, requestOne)

	requestTwo := httptest.NewRequestWithContext(t.Context(), http.MethodPost, "/api/v1/processes", bytes.NewReader([]byte(`{"name":"demo"}`)))
	requestTwo.Header.Set(idempotencyKeyHeader, "create-process-1")
	requestTwo.Header.Set("Content-Type", "application/json")
	responseTwo := httptest.NewRecorder()
	handler.ServeHTTP(responseTwo, requestTwo)

	if got := calls.Load(); got != 1 {
		t.Fatalf("expected handler to be called once for replayed idempotent request, got %d", got)
	}

	if responseTwo.Code != http.StatusCreated {
		t.Fatalf("expected status %d, got %d", http.StatusCreated, responseTwo.Code)
	}

	if responseOne.Body.String() != responseTwo.Body.String() {
		t.Fatalf("expected replayed body %q, got %q", responseOne.Body.String(), responseTwo.Body.String())
	}

	if got := responseTwo.Header().Get(idempotencyReplayHeader); got != "true" {
		t.Fatalf("expected replay header to be true, got %q", got)
	}
}

func TestIdempotencyInterceptorRejectsKeyReuseForDifferentRequest(t *testing.T) {
	t.Parallel()

	var calls atomic.Int32
	handler := NewIdempotencyInterceptor(time.Minute).Wrap(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		calls.Add(1)
		w.WriteHeader(http.StatusAccepted)
	}))

	requestOne := httptest.NewRequestWithContext(t.Context(), http.MethodPost, "/api/v1/tasks/claim", bytes.NewReader([]byte(`{"taskId":"a"}`)))
	requestOne.Header.Set(idempotencyKeyHeader, "claim-task")
	responseOne := httptest.NewRecorder()
	handler.ServeHTTP(responseOne, requestOne)

	requestTwo := httptest.NewRequestWithContext(t.Context(), http.MethodPost, "/api/v1/tasks/claim", bytes.NewReader([]byte(`{"taskId":"b"}`)))
	requestTwo.Header.Set(idempotencyKeyHeader, "claim-task")
	responseTwo := httptest.NewRecorder()
	handler.ServeHTTP(responseTwo, requestTwo)

	if responseTwo.Code != idempotencyConflictStatusCode {
		t.Fatalf("expected status %d, got %d", idempotencyConflictStatusCode, responseTwo.Code)
	}

	if got := calls.Load(); got != 1 {
		t.Fatalf("expected handler to be called once before conflict, got %d", got)
	}
}

func TestIdempotencyInterceptorReturnsTimeoutWhenWaitingRequestIsCanceled(t *testing.T) {
	t.Parallel()

	start := make(chan struct{})
	release := make(chan struct{})
	handler := NewIdempotencyInterceptor(time.Minute).Wrap(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		close(start)
		<-release
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte("done"))
	}))

	firstDone := make(chan struct{})
	go func() {
		defer close(firstDone)
		first := httptest.NewRequestWithContext(t.Context(), http.MethodPost, "/api/v1/processes/execute", bytes.NewReader([]byte(`{"id":"1"}`)))
		first.Header.Set(idempotencyKeyHeader, "run-process-1")
		handler.ServeHTTP(httptest.NewRecorder(), first)
	}()

	select {
	case <-start:
	case <-time.After(time.Second):
		t.Fatalf("timed out waiting for first request to start")
	}

	ctx, cancel := context.WithTimeout(t.Context(), 20*time.Millisecond)
	defer cancel()

	second := httptest.NewRequestWithContext(t.Context(), http.MethodPost, "/api/v1/processes/execute", bytes.NewReader([]byte(`{"id":"1"}`))).WithContext(ctx)
	second.Header.Set(idempotencyKeyHeader, "run-process-1")
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, second)

	if response.Code != http.StatusRequestTimeout {
		t.Fatalf("expected status %d, got %d", http.StatusRequestTimeout, response.Code)
	}

	close(release)
	select {
	case <-firstDone:
	case <-time.After(time.Second):
		t.Fatalf("timed out waiting for first request completion")
	}
}
