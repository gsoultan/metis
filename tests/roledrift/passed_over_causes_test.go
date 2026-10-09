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

// passedOverKey is the catalogue key of a cause.
const passedOverKey = "migration.passedOver."

// keyedCause finds the causes a catalogue has words for.
var keyedCause = regexp.MustCompile(`'migration\.passedOver\.([a-z_]+)':`)

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
		source, err := os.ReadFile(filepath.Clean(path))
		if err != nil {
			t.Fatalf("could not read %s: %v", path, err)
		}
		for _, cause := range causes {
			if key := "'" + passedOverKey + string(cause) + "':"; !strings.Contains(string(source), key) {
				t.Errorf("%s has no words for the cause %q; the dialog would fall back to the server's English", path, cause)
			}
		}
		for _, found := range keyedCause.FindAllStringSubmatch(string(source), -1) {
			if !entities.PassedOverCause(found[1]).Valid() {
				t.Errorf("%s has words for %q, a cause the server never gives", path, found[1])
			}
		}
	}
}

// A cause that names steps is worded with them, and one that names none is
// not: a sentence that says "at {steps}" for a cause that carries no step
// would show the placeholder.
func TestACausesWordsNameItsStepsOnlyWhenItHasAny(t *testing.T) {
	namesSteps := map[entities.PassedOverCause]bool{
		entities.PassedOverLeftTheStep:             true,
		entities.PassedOverNowhereToLand:           true,
		entities.PassedOverLeftWhereNothingDecides: true,
		entities.PassedOverWaitingToBeDecided:      true,
		entities.PassedOverNoLongerRunning:         false,
		entities.PassedOverNotPlannedFor:           false,
		entities.PassedOverAlreadyMoved:            false,
		entities.PassedOverCountersWouldMerge:      false,
	}
	for _, cause := range entities.PassedOverCauses() {
		if _, known := namesSteps[cause]; !known {
			t.Fatalf("this test does not know whether the cause %q names steps", cause)
		}
	}
	for _, path := range catalogues {
		source, err := os.ReadFile(filepath.Clean(path))
		if err != nil {
			t.Fatalf("could not read %s: %v", path, err)
		}
		for cause, named := range namesSteps {
			line := lineOf(string(source), "'"+passedOverKey+string(cause)+"':")
			if line == "" {
				continue // the test above says so
			}
			if strings.Contains(line, "{steps}") != named {
				t.Errorf("%s words %q with {steps}: %v, and the server names steps for it: %v", path, cause, !named, named)
			}
		}
	}
}

// lineOf is the entry of a catalogue that starts with key: the key's line
// and, where the words are on the next, that one too.
func lineOf(source, key string) string {
	at := strings.Index(source, key)
	if at < 0 {
		return ""
	}
	rest := source[at:]
	end := strings.Index(rest, "',\n")
	if end < 0 {
		return rest
	}
	return rest[:end]
}
