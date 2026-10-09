package roledrift

import (
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"

	"github.com/gsoultan/metis/server/domains/entities"
)

// A migration says why it passed an instance over as a cause, one of a closed
// set, beside an English sentence. The interface says it in its own language
// from the cause, and falls back to the server's English for a cause its
// catalogue has no words for — which reads as a translated screen with one
// English sentence in it. So every cause the server can give has words in
// every language the interface ships, and no catalogue has words for a cause
// the server never gives.

const (
	// passedOverKey is what the catalogue key of a cause begins with.
	passedOverKey = "migration.passedOver."
	// passedOverStepsMoreKey is the words for the steps a cause is about that
	// its list leaves out: a cause lists ten of them beside how many there
	// were, and the interface ends its list with these.
	passedOverStepsMoreKey = "migration.passedOverStepsMore"
)

// catalogueEntry finds an entry of a catalogue: a quoted key at the start of
// a line, and the words after its colon, in either kind of quote, on that
// line or the next.
var catalogueEntry = regexp.MustCompile(`(?m)^\s*'([^'\n]+)':\s*\n?\s*(?:'((?:[^'\\\n]|\\.)*)'|"((?:[^"\\\n]|\\.)*)"),?\s*$`)

// entriesOf is the entries a catalogue defines, by key: what the interface
// would find there. A key in a comment — one commented out, or one a comment
// mentions — defines nothing and is not among them.
func entriesOf(t *testing.T, path string) map[string]string {
	t.Helper()
	source, err := os.ReadFile(filepath.Clean(path))
	if err != nil {
		t.Fatalf("could not read %s: %v", path, err)
	}
	var code []string
	inBlock := false
	for line := range strings.SplitSeq(string(source), "\n") {
		trimmed := strings.TrimSpace(line)
		switch {
		case inBlock:
			inBlock = !strings.Contains(trimmed, "*/")
		case strings.HasPrefix(trimmed, "/*"):
			inBlock = !strings.Contains(trimmed, "*/")
		case strings.HasPrefix(trimmed, "//"):
		default:
			code = append(code, line)
		}
	}
	entries := map[string]string{}
	for _, found := range catalogueEntry.FindAllStringSubmatch(strings.Join(code, "\n"), -1) {
		entries[found[1]] = found[2] + found[3]
	}
	if len(entries) < 100 {
		t.Fatalf("%s was read as %d entries; the catalogue is not being read as it is written", path, len(entries))
	}
	return entries
}

func TestEveryPassedOverCauseHasWordsInEveryCatalogue(t *testing.T) {
	causes := entities.PassedOverCauses()
	if len(causes) == 0 {
		t.Fatal("the server names no cause for passing an instance over")
	}
	if !slices.IsSorted(causes) {
		t.Errorf("the causes are not sorted: %v", causes)
	}
	for _, cause := range causes {
		if !cause.Valid() {
			t.Errorf("the cause %q is listed and is not valid", cause)
		}
	}
	for _, path := range catalogues {
		entries := entriesOf(t, path)
		for _, cause := range causes {
			if words := entries[passedOverKey+string(cause)]; strings.TrimSpace(words) == "" {
				t.Errorf("%s has no words for the cause %q; the dialog would fall back to the server's English", path, cause)
			}
		}
		// Whatever follows the prefix has to be a cause, to the letter: a key
		// written another way (leftTheStep) is words nothing asks for.
		for key := range entries {
			if after, isCause := strings.CutPrefix(key, passedOverKey); isCause && !entities.PassedOverCause(after).Valid() {
				t.Errorf("%s has words under %q, and %q is no cause the server gives", path, key, after)
			}
		}
	}
}

// A cause that names steps is worded with them, and one that names none is
// not: a sentence that says "at {steps}" for a cause that carries no step
// would show the placeholder. Which causes name steps is the cause's own to
// say (PassedOverCause.NamesSteps), and the service's constructors are held
// to the same answer (TestEveryWayOfBeingPassedOverHasACauseAndNamesItsSteps).
//
// A cause lists ten of its steps beside how many there were, so the
// interface needs words for the rest: "and 15 more", a plural of its own, as
// the catalogue words what any other cut list leaves out.
func TestACausesWordsNameItsStepsOnlyWhenItHasAny(t *testing.T) {
	for _, path := range catalogues {
		entries := entriesOf(t, path)
		for _, cause := range entities.PassedOverCauses() {
			words, has := entries[passedOverKey+string(cause)]
			if !has {
				continue // the test above says so
			}
			if strings.Contains(words, "{steps}") != cause.NamesSteps() {
				t.Errorf("%s words %q with {steps}: %v, and the server names steps for it: %v",
					path, cause, strings.Contains(words, "{steps}"), cause.NamesSteps())
			}
		}
		more, has := entries[passedOverStepsMoreKey]
		if !has || !strings.HasPrefix(more, "{count, plural,") || !strings.Contains(more, "#") {
			t.Errorf("%s has no words for the steps a cause leaves out of its list (%s, a plural of {count}): %q", path, passedOverStepsMoreKey, more)
		}
	}
}
