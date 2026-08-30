package envvar

import (
	"bytes"
	"strings"
	"sync"
	"testing"

	"github.com/rs/zerolog"
	zlog "github.com/rs/zerolog/log"
)

// forgetWarnings clears the once-per-variable memory so tests do not depend on
// each other's order.
func forgetWarnings(t *testing.T) {
	t.Helper()
	warned = sync.Map{}
	t.Cleanup(func() { warned = sync.Map{} })
}

func captureLog(t *testing.T) *bytes.Buffer {
	t.Helper()
	var buf bytes.Buffer
	original := zlog.Logger
	zlog.Logger = zerolog.New(&buf)
	t.Cleanup(func() { zlog.Logger = original })
	return &buf
}

func TestTheCurrentNameIsPreferred(t *testing.T) {
	forgetWarnings(t)
	logs := captureLog(t)
	t.Setenv("METIS_HTTP_ADDRESS", ":9000")
	t.Setenv("GOBPM_HTTP_ADDRESS", ":8080")

	if got := Get("METIS_HTTP_ADDRESS"); got != ":9000" {
		t.Fatalf("got %q, want the METIS_ value to win", got)
	}
	if logs.Len() != 0 {
		t.Fatalf("warned about a legacy name that was not used: %s", logs.String())
	}
}

// The point of the whole package: an installation that has not been touched
// keeps working after the rename.
func TestTheOldNameStillWorks(t *testing.T) {
	forgetWarnings(t)
	captureLog(t)
	t.Setenv("GOBPM_GRPC_ADDRESS", ":8081")

	value, ok := Lookup("METIS_GRPC_ADDRESS")
	if !ok || value != ":8081" {
		t.Fatalf("Lookup = (%q, %v), want the GOBPM_ value to be found", value, ok)
	}
}

// Silence would make the fallback permanent by accident — nobody migrates off a
// name nothing ever mentions.
func TestUsingTheOldNameSaysSo(t *testing.T) {
	forgetWarnings(t)
	logs := captureLog(t)
	t.Setenv("GOBPM_SCRIPT_TIMEOUT", "10s")

	Get("METIS_SCRIPT_TIMEOUT")

	out := logs.String()
	for _, want := range []string{"GOBPM_SCRIPT_TIMEOUT", "METIS_SCRIPT_TIMEOUT", "removed in a future release"} {
		if !strings.Contains(out, want) {
			t.Errorf("the warning does not mention %q: %s", want, out)
		}
	}
}

// Several of these are read on every outbound request. A line per read would
// bury the message it exists to deliver.
func TestTheWarningIsOncePerVariable(t *testing.T) {
	forgetWarnings(t)
	logs := captureLog(t)
	t.Setenv("GOBPM_HTTP_TIMEOUT", "5s")

	for range 100 {
		Get("METIS_HTTP_TIMEOUT")
	}

	if lines := strings.Count(strings.TrimSpace(logs.String()), "\n") + 1; lines != 1 {
		t.Fatalf("100 reads produced %d warnings, want 1", lines)
	}
}

// "Set to empty" and "not set" are different answers, and several callers rely
// on telling them apart.
func TestAnEmptyValueIsStillASetting(t *testing.T) {
	forgetWarnings(t)
	captureLog(t)
	t.Setenv("GOBPM_CORS_ORIGINS", "")

	value, ok := Lookup("METIS_CORS_ORIGINS")
	if !ok || value != "" {
		t.Fatalf("Lookup = (%q, %v), want (\"\", true) — an empty setting is deliberate", value, ok)
	}
}

func TestNeitherSpellingSetReportsAbsent(t *testing.T) {
	forgetWarnings(t)
	logs := captureLog(t)

	if value, ok := Lookup("METIS_DOES_NOT_EXIST"); ok || value != "" {
		t.Fatalf("Lookup = (%q, %v), want an absent setting", value, ok)
	}
	if logs.Len() != 0 {
		t.Fatalf("warned about a variable nobody set: %s", logs.String())
	}
}

// ENCRYPTION_KEY, JWT_SECRET and DATABASE_URL were never prefixed, so they have
// no predecessor to look for and must be read exactly as given.
func TestAnUnprefixedNameIsReadAsGiven(t *testing.T) {
	forgetWarnings(t)
	logs := captureLog(t)
	t.Setenv("ENCRYPTION_KEY", "abc")

	if got := Get("ENCRYPTION_KEY"); got != "abc" {
		t.Fatalf("got %q, want abc", got)
	}
	if _, ok := legacyNameFor("ENCRYPTION_KEY"); ok {
		t.Error("an unprefixed name was given a GOBPM_ predecessor")
	}
	if logs.Len() != 0 {
		t.Fatalf("warned about an unprefixed variable: %s", logs.String())
	}
}
