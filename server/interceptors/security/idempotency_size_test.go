package security

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"
)

// recordingStore counts completed records.
type recordingStore struct {
	IdempotencyStore
	completed atomic.Int32
}

func (s *recordingStore) Complete(ctx context.Context, key string, response StoredResponse) error {
	s.completed.Add(1)
	return s.IdempotencyStore.Complete(ctx, key, response)
}

// A reply too large to keep is sent, not stored, and a retry with the same
// key is refused rather than run again.
//
// Every reply was buffered whole and stored as one record with no cap, so a
// large reply behind an Idempotency-Key sat in memory twice and went into the
// records table as one row of whatever size the handler produced. The cap
// fixed that by releasing the key, which let a retry run the request a second
// time; now the key records that it ran.
func TestAResponseTooLargeToKeepIsSentAndARetryIsRefused(t *testing.T) {
	t.Parallel()

	chunk := bytes.Repeat([]byte("x"), 64<<10)
	const chunks = (maxIdempotentResponseBytes / (64 << 10)) + 8
	var calls atomic.Int32
	store := &recordingStore{IdempotencyStore: NewMemoryIdempotencyStore(time.Minute)}
	handler := NewIdempotencyInterceptorWithStore(store, time.Minute).Wrap(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		calls.Add(1)
		w.Header().Set("Content-Type", "application/octet-stream")
		w.WriteHeader(http.StatusCreated)
		for range chunks {
			_, _ = w.Write(chunk)
		}
	}))

	send := func() *httptest.ResponseRecorder {
		request := httptest.NewRequestWithContext(t.Context(), http.MethodPost, "/api/v1/exports", bytes.NewReader([]byte(`{"all":true}`)))
		request.Header.Set(idempotencyKeyHeader, "export-1")
		recorder := httptest.NewRecorder()
		handler.ServeHTTP(recorder, request)
		return recorder
	}

	first := send()
	if first.Code != http.StatusCreated {
		t.Fatalf("status = %d, want %d", first.Code, http.StatusCreated)
	}
	if got, want := first.Body.Len(), chunks*len(chunk); got != want {
		t.Fatalf("the client got %d bytes of a %d-byte reply", got, want)
	}
	if got := first.Header().Get("Content-Type"); got != "application/octet-stream" {
		t.Fatalf("the handler's headers did not reach the client: Content-Type = %q", got)
	}

	second := send()
	if calls.Load() != 1 {
		t.Fatalf("handler ran %d times for one Idempotency-Key, want 1", calls.Load())
	}
	if second.Code != http.StatusConflict || second.Header().Get(idempotencyReplayHeader) != "true" {
		t.Fatalf("retry: status = %d, replayed = %q; want 409, replayed", second.Code, second.Header().Get(idempotencyReplayHeader))
	}
	if got := second.Header().Get(idempotencyOriginalStatus); got != "201" {
		t.Fatalf("retry: %s = %q, want the 201 the request ran with", idempotencyOriginalStatus, got)
	}
	if second.Body.Len() > 1024 {
		t.Fatalf("the refusal carried %d bytes; the large reply was stored after all", second.Body.Len())
	}
}

// A reply within the cap is still recorded and replayed exactly.
func TestAResponseWithinTheCapIsStillReplayed(t *testing.T) {
	t.Parallel()

	body := bytes.Repeat([]byte("y"), maxIdempotentResponseBytes)
	var calls atomic.Int32
	handler := NewIdempotencyInterceptor(time.Minute).Wrap(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		calls.Add(1)
		_, _ = w.Write(body)
	}))

	send := func() *httptest.ResponseRecorder {
		request := httptest.NewRequestWithContext(t.Context(), http.MethodPost, "/api/v1/exports", nil)
		request.Header.Set(idempotencyKeyHeader, "export-2")
		recorder := httptest.NewRecorder()
		handler.ServeHTTP(recorder, request)
		return recorder
	}

	send()
	replay := send()
	if replay.Header().Get(idempotencyReplayHeader) != "true" || !bytes.Equal(replay.Body.Bytes(), body) {
		t.Fatalf("a reply at the cap was not replayed whole: replayed=%q len=%d",
			replay.Header().Get(idempotencyReplayHeader), replay.Body.Len())
	}
	if calls.Load() != 1 {
		t.Fatalf("handler ran %d times, want 1", calls.Load())
	}
}
