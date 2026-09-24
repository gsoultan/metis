// Package secretdrift asserts that the browser and the server agree on which
// configuration keys hold a credential.
//
// They did not. The server masks a key by the fragments in
// internal/pkg/configsecret; the browser decides whether to draw a password box
// from its own copy in ui/src/domain/connectorSecrets.ts. The browser's copy had
// fallen four fragments behind — passwd, credential, private, signature — so a
// field the server treated as a secret was typed into a plain text box, in view
// of anybody behind the person typing it.
//
// The check reads the UI's own list rather than a copy of it, because a copy is
// the thing that drifted. Same approach as tests/roledrift.
package secretdrift

import (
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"testing"

	"github.com/gsoultan/metis/internal/pkg/configsecret"
)

// secretsFile is the browser's list, relative to the repository root.
const secretsFile = "../../ui/src/domain/connectorSecrets.ts"

var (
	// block is the SENSITIVE_FRAGMENTS array literal, however it is wrapped.
	block = regexp.MustCompile(`(?s)SENSITIVE_FRAGMENTS[^=]*=\s*\[(.*?)\]`)
	// quoted is one entry in it.
	quoted = regexp.MustCompile(`'([^']+)'`)
)

func browserFragments(t *testing.T) []string {
	t.Helper()
	source, err := os.ReadFile(filepath.Clean(secretsFile))
	if err != nil {
		t.Fatalf("could not read %s: %v", secretsFile, err)
	}
	found := block.FindSubmatch(source)
	if found == nil {
		// No match means the file changed shape, not that it is clean. A
		// regression test that silently stops testing is worse than none.
		t.Fatalf("found no SENSITIVE_FRAGMENTS array in %s; the file's shape changed and this check no longer reads it", secretsFile)
	}
	var fragments []string
	for _, entry := range quoted.FindAllSubmatch(found[1], -1) {
		fragments = append(fragments, string(entry[1]))
	}
	if len(fragments) == 0 {
		t.Fatalf("SENSITIVE_FRAGMENTS in %s is empty or no longer written as quoted strings", secretsFile)
	}
	return fragments
}

func TestEveryFragmentTheServerMasksTheBrowserHides(t *testing.T) {
	browser := browserFragments(t)
	for _, fragment := range configsecret.SensitiveFragments() {
		if !slices.Contains(browser, fragment) {
			t.Errorf("the server masks keys containing %q and the browser draws them as plain text", fragment)
		}
	}
}

func TestEveryFragmentTheBrowserHidesTheServerMasks(t *testing.T) {
	server := configsecret.SensitiveFragments()
	for _, fragment := range browserFragments(t) {
		if !slices.Contains(server, fragment) {
			t.Errorf("the browser treats %q as a secret and the server returns it in clear", fragment)
		}
	}
}
