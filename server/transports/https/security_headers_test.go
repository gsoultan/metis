package https

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestSecurityHeadersArePresent(t *testing.T) {
	next := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusOK) })
	rec := httptest.NewRecorder()
	securityHeaders(next).ServeHTTP(rec, httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/", nil))

	for _, h := range []string{
		"Content-Security-Policy",
		"X-Content-Type-Options",
		"X-Frame-Options",
		"Referrer-Policy",
	} {
		if rec.Header().Get(h) == "" {
			t.Fatalf("missing security header %s", h)
		}
	}
	csp := rec.Header().Get("Content-Security-Policy")
	if !strings.Contains(csp, "frame-ancestors 'none'") || !strings.Contains(csp, "default-src 'none'") {
		t.Fatalf("CSP %q does not deny everything; the engine serves no pages", csp)
	}
}

// The engine serves no web app any more. A browser that opens it gets a 404
// saying where the app went, not a blank page or a bare "404 page not found".
func TestThePathsOutsideTheAPISayWhereTheWebAppWent(t *testing.T) {
	for _, p := range []string{"/", "/inbox", "/assets/app.js"} {
		rec := httptest.NewRecorder()
		notAnAPIPath(rec, httptest.NewRequestWithContext(t.Context(), http.MethodGet, p, nil))
		if rec.Code != http.StatusNotFound {
			t.Fatalf("%s answered %d, want 404", p, rec.Code)
		}
		if ct := rec.Header().Get("Content-Type"); ct != "application/json" {
			t.Fatalf("%s answered %s, want JSON", p, ct)
		}
		if !strings.Contains(rec.Body.String(), "metis-server") {
			t.Fatalf("%s answered %q, want it to name metis-server", p, rec.Body.String())
		}
	}
}

func TestTheNotFoundBodyIsValidJSON(t *testing.T) {
	var body map[string]string
	if err := json.Unmarshal([]byte(notAnAPIPathBody), &body); err != nil || body["err"] == "" {
		t.Fatalf("%q is not the API's error shape: %v", notAnAPIPathBody, err)
	}
}
