package https

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// A panic must become a status, not a closed connection.
//
// Without this the caller gets EOF and the metrics middleware — which records
// after ServeHTTP returns — never counts the request, so the failure spends no
// error budget and fires no alert.
func TestAPanicBecomesA500(t *testing.T) {
	handler := RecoverPanics(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		panic("the database said something surprising")
	}))

	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/api/v1/tasks", nil))

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("answered %d, want 500", rec.Code)
	}
	if got := rec.Header().Get("Content-Type"); got != "application/json; charset=utf-8" {
		t.Errorf("Content-Type %q, want JSON", got)
	}
	// The panic value can carry a row, a token or a process variable. It goes
	// to the log, where an operator reads it, and not to whoever sent the
	// request.
	if strings.Contains(rec.Body.String(), "surprising") {
		t.Errorf("the panic value reached the caller: %s", rec.Body.String())
	}
}

// http.ErrAbortHandler is the documented way for a handler to abandon a
// response without being reported. Recovering it would turn a deliberate abort
// into a 500 and break the contract net/http and ReverseProxy rely on.
func TestAnAbortIsNotRecovered(t *testing.T) {
	handler := RecoverPanics(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		panic(http.ErrAbortHandler)
	}))

	defer func() {
		err, _ := recover().(error)
		if !errors.Is(err, http.ErrAbortHandler) {
			t.Errorf("recovered %v, want http.ErrAbortHandler to propagate", err)
		}
	}()
	handler.ServeHTTP(httptest.NewRecorder(), httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/", nil))
	t.Error("ErrAbortHandler was swallowed")
}

// startedWriter reports that the response is already in flight.
type startedWriter struct{ http.ResponseWriter }

func (startedWriter) ResponseStarted() bool { return true }

// A response that has already begun cannot be replaced, only corrupted: the
// status is sent, and an SSE stream would get a JSON object appended to it.
// Leave it truncated and let the client's read fail.
func TestAPanicMidResponseDoesNotAppendToIt(t *testing.T) {
	handler := RecoverPanics(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("data: first\n\n"))
		panic("halfway through the stream")
	}))

	rec := httptest.NewRecorder()
	handler.ServeHTTP(startedWriter{rec}, httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/api/v1/events", nil))

	if body := rec.Body.String(); body != "data: first\n\n" {
		t.Errorf("the partial response was written over: %q", body)
	}
}
