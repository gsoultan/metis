// Command stormgen regenerates the typed store from the model layer.
//
//	STORM_DSN=postgres://... go run ./cmd/stormgen
//
// Storm builds SQL at compile time, so the query surface is generated code
// rather than something assembled at run time. That has one consequence worth
// stating: the generated store is checked in, and it is stale the moment a
// model changes. Running this is part of changing a model, not a separate
// chore — `make generate` does it, and the build fails loudly if the two
// disagree, because a store that no longer matches its model produces SQL for
// columns that are not there.
//
// It needs a PostgreSQL server, though not a schema: every statement in
// model.Queries is PREPAREd against the model, applied to a scratch schema
// that is dropped afterwards, and only a statement that prepared is let run.
// $STORM_DSN names the server, or -dsn does.
package main

import (
	"errors"
	"fmt"
	"os"

	"github.com/gsoultan/metis/server/repositories/model"
	"github.com/gsoultan/storm/tool"
)

const outputDir = "server/repositories/store"

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, "stormgen:", err)
		os.Exit(1)
	}
}

func run(args []string) error {
	// Written fresh each time rather than merged: a file left behind by a model
	// that no longer exists still compiles, and would keep answering queries
	// against a table nothing creates. The old store is set aside rather than
	// removed, so a statement that does not prepare leaves it as it was.
	previous := outputDir + ".previous"
	if err := os.RemoveAll(previous); err != nil {
		return fmt.Errorf("clear %s: %w", previous, err)
	}
	if err := os.Rename(outputDir, previous); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("set %s aside: %w", outputDir, err)
	}
	tool.Models, tool.RawQueries = model.All(), model.Queries()
	if err := tool.Run(append([]string{"generate", outputDir}, args...)); err != nil {
		return errors.Join(err, restore(previous))
	}
	return os.RemoveAll(previous)
}

// restore puts the store set aside back, in place of whatever a failed run
// wrote.
func restore(previous string) error {
	if err := os.RemoveAll(outputDir); err != nil {
		return fmt.Errorf("clear %s: %w", outputDir, err)
	}
	if err := os.Rename(previous, outputDir); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("restore %s: %w", outputDir, err)
	}
	return nil
}
