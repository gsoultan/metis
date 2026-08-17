package health

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestWrap(t *testing.T) {
	errDown := errors.New("dial tcp: connection refused, dsn=user:hunter2@tcp(db:3306)/gobpm")

	tests := []struct {
		name       string
		path       string
		readiness  map[string]Checker
		wantStatus int
		wantBody   string
		wantDetail string
	}{
		{
			name:       "liveness ignores a broken dependency",
			path:       LivenessPath,
			readiness:  map[string]Checker{"database": failing(errDown)},
			wantStatus: http.StatusOK,
			wantBody:   "ok",
		},
		{
			name:       "readiness passes when every dependency answers",
			path:       ReadinessPath,
			readiness:  map[string]Checker{"database": healthy()},
			wantStatus: http.StatusOK,
			wantBody:   "ok",
		},
		{
			name:       "readiness fails when a dependency is down",
			path:       ReadinessPath,
			readiness:  map[string]Checker{"database": failing(errDown)},
			wantStatus: http.StatusServiceUnavailable,
			wantBody:   "unavailable",
			wantDetail: "database is not reachable",
		},
		{
			name:       "readiness with nothing to check passes",
			path:       ReadinessPath,
			readiness:  nil,
			wantStatus: http.StatusOK,
			wantBody:   "ok",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			handler := Wrap(unreachable(t), tc.readiness)

			rec := httptest.NewRecorder()
			handler.ServeHTTP(rec, httptest.NewRequestWithContext(t.Context(), http.MethodGet, tc.path, nil))

			if rec.Code != tc.wantStatus {
				t.Fatalf("status = %d, want %d", rec.Code, tc.wantStatus)
			}

			var got response
			if err := json.NewDecoder(rec.Body).Decode(&got); err != nil {
				t.Fatalf("decode body: %v", err)
			}
			if got.Status != tc.wantBody {
				t.Errorf("status field = %q, want %q", got.Status, tc.wantBody)
			}
			if got.Detail != tc.wantDetail {
				t.Errorf("detail = %q, want %q", got.Detail, tc.wantDetail)
			}
			if rec.Header().Get("Cache-Control") != "no-store" {
				t.Error("probe answers must not be cacheable")
			}
		})
	}
}

// TestWrapDoesNotLeakDependencyErrors pins the redaction: a driver error can
// carry the DSN, and a probe endpoint is unauthenticated.
func TestWrapDoesNotLeakDependencyErrors(t *testing.T) {
	secret := "user:hunter2@tcp(db:3306)/gobpm"
	handler := Wrap(unreachable(t), map[string]Checker{
		"database": failing(errors.New("dial failed, dsn=" + secret)),
	})

	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequestWithContext(t.Context(), http.MethodGet, ReadinessPath, nil))

	if body := rec.Body.String(); strings.Contains(body, "hunter2") || strings.Contains(body, secret) {
		t.Fatalf("readiness body leaked the dependency error: %s", body)
	}
}

// TestWrapPassesEverythingElseThrough keeps the probe routes from swallowing the
// application.
func TestWrapPassesEverythingElseThrough(t *testing.T) {
	var reached bool
	handler := Wrap(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { reached = true }), nil)

	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/api/v1/tasks", nil))

	if !reached {
		t.Fatal("a non-probe request did not reach the wrapped handler")
	}
}

func healthy() Checker { return CheckerFunc(func(context.Context) error { return nil }) }

func failing(err error) Checker { return CheckerFunc(func(context.Context) error { return err }) }

// unreachable fails the test if the wrapped handler is called, which is how the
// probe routes prove they short-circuit.
func unreachable(t *testing.T) http.Handler {
	t.Helper()
	return http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		t.Error("probe request reached the wrapped handler")
	})
}
