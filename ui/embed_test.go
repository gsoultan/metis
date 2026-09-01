package ui_test

import (
	"io/fs"
	"strings"
	"testing"

	"github.com/gsoultan/metis/ui"
)

func TestDistEmbedding(t *testing.T) {
	distFS := ui.Dist()

	// Check if assets folder exists
	assetsInfo, err := fs.Stat(distFS, "assets")
	if err != nil {
		t.Fatalf("assets folder not found in dist: %v", err)
	}
	if !assetsInfo.IsDir() {
		t.Fatal("assets should be a directory")
	}

	// List all files in assets and check for at least one starting with _
	foundUnderscore := false
	err = fs.WalkDir(distFS, "assets", func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !d.IsDir() && strings.HasPrefix(d.Name(), "_") {
			foundUnderscore = true
			t.Logf("Found embedded file starting with underscore: %s", path)
		}
		return nil
	})

	if err != nil {
		t.Fatalf("error walking assets: %v", err)
	}

	if !foundUnderscore {
		t.Error("No files starting with underscore found in embedded dist/assets. Files starting with _ are likely skipped by go:embed.")
	}
}
