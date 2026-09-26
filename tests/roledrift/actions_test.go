package roledrift

import (
	"os"
	"path/filepath"
	"regexp"
	"testing"
)

// The legend beside each role lists the actions the role is required for. The
// server words each one from its gate's method name, in English; the catalogues
// word it in the interface's language under the same name, and the legend
// falls back to the server's words for a method they do not know yet. So a key
// naming a method the server does not gate is a translation nobody will see —
// a typo, or a gate renamed since — and the action it was written for is shown
// in English without anyone noticing.

// translatedAction matches the method in each `'access.action.<Method>':` key.
var translatedAction = regexp.MustCompile(`'access\.action\.([A-Za-z0-9]+)':`)

func TestEveryTranslatedActionIsOneTheServerGates(t *testing.T) {
	gated := map[string]bool{}
	for _, methods := range servedLegend(t) {
		for _, method := range methods {
			gated[method] = true
		}
	}

	for _, path := range catalogues {
		source, err := os.ReadFile(filepath.Clean(path))
		if err != nil {
			t.Fatalf("could not read %s: %v", path, err)
		}
		matches := translatedAction.FindAllStringSubmatch(string(source), -1)
		if len(matches) == 0 {
			// No matches means the file changed shape, not that it is clean.
			t.Fatalf("found no action words in %s; the file's shape changed and this check no longer reads it", path)
		}
		for _, match := range matches {
			if method := match[1]; !gated[method] {
				t.Errorf("%s words the action %q, which no gate the server builds is named; it is never shown", path, method)
			}
		}
	}
}
