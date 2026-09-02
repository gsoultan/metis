package secrets

import (
	"errors"
	"strings"
	"testing"
)

// The dangerous case is not a short secret — it is the evaluation key from
// docker-compose.yml. It is thirty-two characters, so every length rule accepts
// it, and it is published in a public repository, so it protects nothing. A
// compose file is the easiest thing to carry from an evaluation into
// production, and it is precisely where a length check cannot help.
func TestAPublishedPlaceholderIsRefusedDespiteBeingLongEnough(t *testing.T) {
	const fromCompose = "0123456789abcdef0123456789abcdef"

	if len(fromCompose) < MinLength {
		t.Fatalf("this test no longer proves what it claims: the value is %d characters, under the %d minimum, so length alone would catch it", len(fromCompose), MinLength)
	}

	err := Validate("ENCRYPTION_KEY", fromCompose)
	if err == nil {
		t.Fatal("the published evaluation key was accepted; anyone with the repository can read every encrypted variable")
	}
	if !errors.Is(err, ErrWeak) {
		t.Errorf("refused with %v, want ErrWeak", err)
	}
}

func TestValidateRefusesWeakSecrets(t *testing.T) {
	cases := []struct {
		name  string
		value string
		why   string
	}{
		{"empty", "", "no secret at all"},
		{"whitespace only", "   ", "trims to nothing"},
		{"short", "hunter2", "brute-forced offline in moments"},
		{"a known placeholder", "changeme", "the first thing anybody tries"},
		{"the compose JWT secret", "evaluation-only-change-me", "published in this repository"},
		{"the CI smoke-test secret", "ci-only-not-a-real-secret", "published in this repository"},
		// Long by repetition. Passes any length rule and is worth nothing.
		{"long but repetitive", strings.Repeat("a", 64), "no harder to guess than one character"},
	}

	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			if err := Validate("JWT_SECRET", testCase.value); err == nil {
				t.Fatalf("accepted %q — %s", testCase.value, testCase.why)
			}
		})
	}
}

// The threshold has to accept what the documentation tells operators to run.
// A rule nobody can satisfy gets overridden, and then it protects nothing.
func TestValidateAcceptsWhatTheDocumentationRecommends(t *testing.T) {
	cases := map[string]string{
		// openssl rand -hex 16
		"openssl rand -hex 16":    "9f2c4a1e8b7d0f36a5c9e2b481d7f30a",
		"openssl rand -hex 24":    "3b8e1d7a2f9c04e6b5182d7fa3c60e94b7d215af8c0e36d2",
		"openssl rand -base64 48": "Qk9wYnRlc3RzZWNyZXQxMjM0NTY3ODkwYWJjZGVmZ2hpamtsbW5vcHFy",
	}

	for name, value := range cases {
		t.Run(name, func(t *testing.T) {
			if err := Validate("ENCRYPTION_KEY", value); err != nil {
				t.Fatalf("refused a secret from the documented command %s: %v", name, err)
			}
		})
	}
}

// The message has to say which setting, because an installation has several and
// the operator is reading a failed boot.
func TestTheMessageNamesTheSetting(t *testing.T) {
	err := Validate("JWT_SECRET", "short")
	if err == nil {
		t.Fatal("a short secret was accepted")
	}
	if !strings.Contains(err.Error(), "JWT_SECRET") {
		t.Errorf("the message does not name the setting: %v", err)
	}
	if !strings.Contains(err.Error(), "openssl") {
		t.Errorf("the message does not say how to generate one: %v", err)
	}
}
