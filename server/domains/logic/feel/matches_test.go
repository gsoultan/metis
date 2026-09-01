package feel

import (
	"strings"
	"testing"
	"time"
)

// matches() is specified as a regular-expression match. It was implemented as
// strings.Contains to avoid catastrophic backtracking on a pattern that comes
// from a deployed definition.
//
// That trade was not available to make: Go's regexp is RE2, which does not
// backtrack. The trade cost correctness and bought nothing — a rule written
// with a real pattern did not error, it answered false for every input, and the
// process took the other branch. A rule that never fires is a wrong answer, not
// a missing feature.
func TestMatchesIsARegularExpression(t *testing.T) {
	cases := []struct {
		name    string
		input   string
		pattern string
		want    bool
	}{
		{"anchored pattern that fits", "ERR-42", "^ERR-[0-9]+$", true},
		{"anchored pattern that does not", "XERR-42", "^ERR-[0-9]+$", false},
		{"any character", "abc", "a.c", true},
		{"alternation", "warn", "^(warn|error)$", true},
		{"character class", "A7", "^[A-Z][0-9]$", true},
		{"quantifier", "aaa", "^a{3}$", true},
		{"quantifier that does not fit", "aa", "^a{3}$", false},

		// A pattern with no metacharacters is a substring test either way, so
		// definitions written against the old behaviour keep their meaning.
		{"a literal pattern still matches a substring", "the code is ERR", "ERR", true},
		{"a literal pattern that is absent", "all fine", "ERR", false},
	}

	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			got, err := biMatches([]Value{Str(testCase.input), Str(testCase.pattern)})
			if err != nil {
				t.Fatalf("matches(%q, %q): %v", testCase.input, testCase.pattern, err)
			}
			if got.Bool != testCase.want {
				t.Errorf("matches(%q, %q) = %v, want %v", testCase.input, testCase.pattern, got.Bool, testCase.want)
			}
		})
	}
}

// A pattern that cannot compile is a mistake in the definition. Answering false
// would hide it exactly the way the old implementation hid everything.
func TestMatchesRefusesAPatternItCannotCompile(t *testing.T) {
	if _, err := biMatches([]Value{Str("anything"), Str("[unclosed")}); err == nil {
		t.Fatal("an invalid pattern was accepted, so a broken rule would silently answer false")
	}
}

// The pattern is untrusted input, so its length is bounded before it reaches
// the compiler. RE2 costs nothing to run, but compiling is work.
func TestMatchesRefusesAnOversizedPattern(t *testing.T) {
	huge := strings.Repeat("a", maxPatternLength+1)
	if _, err := biMatches([]Value{Str("aaa"), Str(huge)}); err == nil {
		t.Fatal("a pattern past the length bound was compiled")
	}
}

// The reason the old implementation existed, held to the claim that replaced
// it: RE2 does not backtrack, so the textbook exponential case is linear.
func TestTheTextbookBacktrackingCaseIsFast(t *testing.T) {
	input := strings.Repeat("a", 40) + "!"

	start := time.Now()
	got, err := biMatches([]Value{Str(input), Str("^(a+)+$")})
	elapsed := time.Since(start)

	if err != nil {
		t.Fatalf("matches: %v", err)
	}
	if got.Bool {
		t.Error("the mismatching input matched")
	}
	// Generous by three orders of magnitude against the measured 119µs: this
	// is asserting "not exponential", not a benchmark.
	if elapsed > time.Second {
		t.Fatalf("took %v, which means something backtracking is evaluating this", elapsed)
	}
}
