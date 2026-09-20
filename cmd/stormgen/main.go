// Command stormgen regenerates the typed store from the model layer.
//
//	go run ./cmd/stormgen
//
// Storm builds SQL at compile time, so the query surface is generated code
// rather than something assembled at run time. That has one consequence worth
// stating: the generated store is checked in, and it is stale the moment a
// model changes. Running this is part of changing a model, not a separate
// chore — `make generate` does it, and the build fails loudly if the two
// disagree, because a store that no longer matches its model produces SQL for
// columns that are not there.
package main

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"

	"github.com/gsoultan/metis/server/repositories/model"
	"github.com/gsoultan/storm"
	"github.com/gsoultan/storm/codegen"
)

const (
	outputDir     = "server/repositories/store"
	packageName   = "store"
	packageImport = "github.com/gsoultan/metis/server/repositories/store"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "stormgen:", err)
		os.Exit(1)
	}
}

func run() error {
	schema, err := storm.Build(model.All()...)
	if err != nil {
		return fmt.Errorf("the model layer does not build:\n%w", err)
	}

	files, err := codegen.Package(schema, codegen.PackageOptions{
		Dir:           outputDir,
		Import:        "github.com/gsoultan/storm",
		Package:       packageName,
		PackageImport: packageImport,
	})
	if err != nil {
		return fmt.Errorf("generate: %w", err)
	}

	// Written fresh each time rather than merged: a file left behind by a model
	// that no longer exists still compiles, and would keep answering queries
	// against a table nothing creates.
	if err := os.RemoveAll(outputDir); err != nil {
		return fmt.Errorf("clear %s: %w", outputDir, err)
	}

	paths := make([]string, 0, len(files))
	for path := range files {
		paths = append(paths, path)
	}
	sort.Strings(paths)

	var bytes int
	for _, rel := range paths {
		full := filepath.Join(outputDir, rel)
		if err := os.MkdirAll(filepath.Dir(full), 0o750); err != nil {
			return err
		}
		if err := os.WriteFile(full, files[rel], 0o600); err != nil {
			return err
		}
		bytes += len(files[rel])
	}
	fmt.Printf("stormgen: %d files, %d KB from %d tables\n", len(paths), bytes/1024, len(model.All()))
	return nil
}
