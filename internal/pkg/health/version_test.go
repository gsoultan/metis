package health

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

// "Which version is running" is the first question of an incident and the last
// question of a deploy. Until the liveness probe answered it, the only source
// was a startup log line that may have rotated away.
func TestLivenessReportsTheBuildVersion(t *testing.T) {
	original := buildVersion
	t.Cleanup(func() { buildVersion = original })

	SetBuildVersion("v1.4.2")
	handler := Wrap(http.NotFoundHandler(), nil)

	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, httptest.NewRequestWithContext(t.Context(), http.MethodGet, LivenessPath, nil))

	if recorder.Code != http.StatusOK {
		t.Fatalf("/healthz = %d, want 200", recorder.Code)
	}
	var body response
	if err := json.Unmarshal(recorder.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode probe body: %v", err)
	}
	if body.Version != "v1.4.2" {
		t.Fatalf("version = %q, want v1.4.2", body.Version)
	}
}

// An empty stamp must not erase the honest default: a probe reporting no
// version at all is harder to act on than one saying "dev".
func TestSetBuildVersionIgnoresAnEmptyStamp(t *testing.T) {
	original := buildVersion
	t.Cleanup(func() { buildVersion = original })

	SetBuildVersion("v2.0.0")
	SetBuildVersion("")
	if buildVersion != "v2.0.0" {
		t.Fatalf("an empty stamp overwrote the version: %q", buildVersion)
	}
}

// Readiness is polled every few seconds by every replica and answers a
// different question — is this instance able to serve *now*. The build it is
// running is not part of that answer.
func TestReadinessDoesNotCarryTheVersion(t *testing.T) {
	original := buildVersion
	t.Cleanup(func() { buildVersion = original })
	SetBuildVersion("v1.4.2")

	handler := Wrap(http.NotFoundHandler(), map[string]Checker{})
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, httptest.NewRequestWithContext(t.Context(), http.MethodGet, ReadinessPath, nil))

	var body response
	if err := json.Unmarshal(recorder.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode probe body: %v", err)
	}
	if body.Version != "" {
		t.Fatalf("readiness carried version %q; that belongs on liveness", body.Version)
	}
}
