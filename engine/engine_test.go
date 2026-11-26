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

// A program embedding the engine reads the role the engine will take before
// starting it, to decide what it serves itself.
func TestTheRoleIsReadBeforeTheEngineStarts(t *testing.T) {
	t.Chdir(t.TempDir())
	t.Setenv("METIS_ROLE", "worker")
	if role, err := engine.Role(); err != nil || role != "worker" {
		t.Fatalf("got %q, %v", role, err)
	}
	t.Setenv("METIS_ROLE", "nope")
	if _, err := engine.Role(); err == nil {
		t.Fatal("an unknown role was accepted")
	}
}

// A program embedding the engine reports its own version, and an invalid
// distribution stops the engine before it starts rather than having it
// compare itself with, or upgrade itself from, something it is not.
func TestTheEmbeddingProgramNamesItsBuild(t *testing.T) {
	before := engine.Version()
	t.Cleanup(func() {
		engine.New(engine.WithBuild(engine.Build{Version: before, Name: "metis", Repository: "gsoultan/metis"}))
	})

	engine.New(engine.WithBuild(engine.Build{Version: "v0.2.2", Name: "metis-server", Repository: "gsoultan/metis-server"}))
	if got := engine.Version(); got != "v0.2.2" {
		t.Fatalf("Version() = %q, want the embedding program's v0.2.2", got)
	}

	bad := engine.New(engine.WithBuild(engine.Build{Version: "v9.9.9", Name: "metis server", Repository: "gsoultan/metis-server"}))
	if err := bad.Run(context.Background(), []string{"-help"}); err == nil {
		t.Fatal("an engine with an invalid distribution ran")
	}
	if got := engine.Version(); got != "v0.2.2" {
		t.Fatalf("an invalid build changed the version to %q", got)
	}
}
