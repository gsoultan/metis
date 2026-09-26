package roledrift

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gsoultan/metis/server/domains/entities"
)

// The legend beside each role on the Platform access page groups what the role
// is required for under a heading per area. The server names the area by a
// key; the catalogues give it words. A key the catalogues do not know shows on
// screen as "access.area.billing", so every area the server can place an
// action in must have words in every language the interface offers.

// catalogues are the translations the interface ships, relative to the
// repository root.
var catalogues = []string{
	"../../ui/src/i18n/catalogues/en.ts",
	"../../ui/src/i18n/catalogues/id.ts",
}

// unplacedArea is the heading the interface shows an action under when the
// server could not place it.
const unplacedArea = "other"

func TestEveryAreaTheLegendUsesHasAHeadingInEveryCatalogue(t *testing.T) {
	areas := append(entities.RoleActionAreas(), unplacedArea)
	for _, path := range catalogues {
		source, err := os.ReadFile(filepath.Clean(path))
		if err != nil {
			t.Fatalf("could not read %s: %v", path, err)
		}
		for _, area := range areas {
			if key := "'access.area." + area + "':"; !strings.Contains(string(source), key) {
				t.Errorf("%s has no heading for the %q area; its actions would sit under the bare key", path, area)
			}
		}
	}
}
