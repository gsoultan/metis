package engine_test

import (
	"context"
	"testing"

	"github.com/gsoultan/metis/engine"
)

// The engine's flags are its own: asking for help prints them and is not a
// failure, and a flag it does not know is refused before anything starts,
// whatever flags the program around it defines.
func TestTheEngineParsesItsOwnFlags(t *testing.T) {
	if err := engine.New().Run(context.Background(), []string{"-help"}); err != nil {
		t.Fatalf("help: %v", err)
	}
	if err := engine.New().Run(context.Background(), []string{"--not-an-engine-flag"}); err == nil {
		t.Fatal("an unknown flag started the engine")
	}
}

func TestTheVersionIsTheBuilds(t *testing.T) {
	if engine.Version() == "" {
		t.Fatal("no version")
	}
}
