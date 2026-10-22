package payloads_test

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/gsoultan/metis/internal/pkg/crypto"
	"github.com/gsoultan/metis/server/repositories/payloads"
)

func configured(t *testing.T) string {
	t.Helper()
	crypto.ResetForTest()
	if err := crypto.Configure("payload-test-key-that-is-long-enough"); err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	if err := payloads.Configure(dir, payloads.MinThreshold); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		payloads.ResetForTest()
		crypto.ResetForTest()
	})
	return dir
}

func kept(t *testing.T, dir string, instance uuid.UUID) []string {
	t.Helper()
	entries, err := os.ReadDir(filepath.Join(dir, "main", instance.String()))
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, entry := range entries {
		names = append(names, entry.Name())
	}
	return names
}

// A value above the threshold is kept once, sealed, and read back as it was;
// one below it, and a value with a store key of its own, stay in the row and
// read back as themselves.
func TestALargeValueIsKeptOnceAndReadBackAsItWas(t *testing.T) {
	dir := configured(t)
	ctx := context.Background()
	instance := uuid.New()
	document := strings.Repeat("contract clause ", 200)
	values := map[string]any{
		"document":  document,
		"amount":    12.5,
		"lookalike": map[string]any{"$metisPayload": strings.Repeat("a", 64), "bytes": 1.0},
	}

	stored, err := payloads.Offload(ctx, instance, values)
	if err != nil {
		t.Fatal(err)
	}
	if values["document"] != document {
		t.Fatal("Offload changed the map it was given")
	}
	ref, ok := stored["document"].(map[string]any)
	if !ok || ref["$metisPayload"] == nil {
		t.Fatalf("the document is stored as %v, want a reference", stored["document"])
	}
	if stored["amount"] != 12.5 {
		t.Errorf("a small value is stored as %v", stored["amount"])
	}
	files := kept(t, dir, instance)
	if len(files) != 1 {
		t.Fatalf("kept %v, want one file", files)
	}
	sealed, err := readUnder(t, dir, filepath.Join("main", instance.String(), files[0]))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(sealed), "contract clause") {
		t.Error("the kept value is in the clear")
	}

	// Kept again, unchanged, as every step does: no second file.
	if _, err := payloads.Offload(ctx, instance, values); err != nil {
		t.Fatal(err)
	}
	if files := kept(t, dir, instance); len(files) != 1 {
		t.Errorf("an unchanged value was kept again: %v", files)
	}

	read, err := payloads.Resolve(ctx, instance, stored)
	if err != nil {
		t.Fatal(err)
	}
	if read["document"] != document || read["amount"] != 12.5 {
		t.Errorf("read back %v", read)
	}
	lookalike, _ := read["lookalike"].(map[string]any)
	if lookalike["$metisPayload"] != strings.Repeat("a", 64) || len(lookalike) != 2 {
		t.Errorf("a value shaped like a reference read back as %v, want itself", read["lookalike"])
	}
}

// A reference that cannot be read is an error: never a missing variable, and
// never the reference itself handed on as if it were the value.
func TestAReferenceThatCannotBeReadIsAnError(t *testing.T) {
	dir := configured(t)
	ctx := context.Background()
	instance := uuid.New()
	stored, err := payloads.Offload(ctx, instance, map[string]any{"document": strings.Repeat("x", 4096)})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.RemoveAll(filepath.Join(dir, "main", instance.String())); err != nil {
		t.Fatal(err)
	}
	if _, err := payloads.Resolve(ctx, instance, stored); err == nil {
		t.Error("a reference whose file is gone was read without an error")
	}

	payloads.ResetForTest()
	if _, err := payloads.Resolve(ctx, instance, stored); !errors.Is(err, payloads.ErrNotConfigured) {
		t.Errorf("reading a reference with no directory set answered %v, want ErrNotConfigured", err)
	}
	// Off, a value is left in the row whatever its size.
	plain, err := payloads.Offload(ctx, instance, map[string]any{"document": strings.Repeat("x", 4096)})
	if err != nil || plain["document"] != strings.Repeat("x", 4096) {
		t.Errorf("with no directory a large value was stored as %v (%v)", plain["document"], err)
	}
}

// A key rotation moves the kept values with the columns; a reference still
// names its value afterwards.
func TestResealMovesKeptValuesToTheCurrentKey(t *testing.T) {
	configured(t)
	ctx := context.Background()
	instance := uuid.New()
	document := strings.Repeat("y", 4096)
	stored, err := payloads.Offload(ctx, instance, map[string]any{"document": document})
	if err != nil {
		t.Fatal(err)
	}
	crypto.ResetForTest()
	if err := crypto.Configure("payload-test-new-key-that-is-long-enough"); err != nil {
		t.Fatal(err)
	}
	if err := crypto.ConfigurePrevious("payload-test-key-that-is-long-enough"); err != nil {
		t.Fatal(err)
	}
	if count, err := payloads.Reseal(true); err != nil || count.Moved != 1 {
		t.Fatalf("a check found %+v (%v), want one to move", count, err)
	}
	if count, err := payloads.Reseal(false); err != nil || count.Moved != 1 {
		t.Fatalf("the reseal moved %+v (%v), want one", count, err)
	}
	crypto.ResetForTest()
	if err := crypto.Configure("payload-test-new-key-that-is-long-enough"); err != nil {
		t.Fatal(err)
	}
	read, err := payloads.Resolve(ctx, instance, stored)
	if err != nil || read["document"] != document {
		t.Errorf("after the reseal and without the old key, read %v (%v)", read["document"], err)
	}
}

// The sweep removes the directory of an instance that has no row, once it is
// old enough, and leaves the rest.
func TestTheSweepRemovesOnlyGoneInstancesDirectories(t *testing.T) {
	dir := configured(t)
	ctx := context.Background()
	live, gone, fresh := uuid.New(), uuid.New(), uuid.New()
	for _, id := range []uuid.UUID{live, gone, fresh} {
		if _, err := payloads.Offload(ctx, id, map[string]any{"document": strings.Repeat("z", 4096)}); err != nil {
			t.Fatal(err)
		}
	}
	old := time.Now().Add(-2 * time.Hour)
	for _, id := range []uuid.UUID{live, gone} {
		if err := os.Chtimes(filepath.Join(dir, "main", id.String()), old, old); err != nil {
			t.Fatal(err)
		}
	}
	existing := func(_ context.Context, ids []uuid.UUID) (map[uuid.UUID]bool, error) {
		return map[uuid.UUID]bool{live: true}, nil
	}
	removed, err := payloads.SweepGone(ctx, time.Now().Add(-time.Hour), existing)
	if err != nil || removed != 1 {
		t.Fatalf("the sweep removed %d (%v), want the one gone instance", removed, err)
	}
	if kept(t, dir, gone) != nil {
		t.Error("the gone instance's values are still there")
	}
	if kept(t, dir, live) == nil || kept(t, dir, fresh) == nil {
		t.Error("the sweep removed a live instance's values, or a fresh one's")
	}
}

// readUnder reads name inside dir, and nothing outside it.
func readUnder(t *testing.T, dir, name string) ([]byte, error) {
	t.Helper()
	root, err := os.OpenRoot(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := root.Close(); err != nil {
			t.Error(err)
		}
	}()
	return root.ReadFile(name)
}
