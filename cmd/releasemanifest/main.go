// Command releasemanifest writes release.json: what a release does to a
// database, read from the migration list compiled into this build.
//
// GoReleaser runs it before the build and lists the file in checksums.txt, so
// the manifest is covered by the same signature as the packages.
//
//	go run ./cmd/releasemanifest -release v0.9.0 -out dist-extra/release.json
package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"

	"github.com/gsoultan/metis/internal/pkg/releasemanifest"
	"github.com/gsoultan/metis/server/repositories/migrations"
)

func main() {
	release := flag.String("release", "", "the tag being released, e.g. v0.9.0")
	out := flag.String("out", "release.json", "where to write the manifest")
	flag.Parse()

	if err := run(*release, *out); err != nil {
		fmt.Fprintln(os.Stderr, "releasemanifest:", err)
		os.Exit(1)
	}
}

func run(release, out string) error {
	steps := migrations.Steps()
	list := make([]releasemanifest.Migration, 0, len(steps))
	for _, s := range steps {
		list = append(list, releasemanifest.Migration{Version: s.Version, Name: s.Name})
	}
	m, err := releasemanifest.Build(releasemanifest.Inputs{
		Release:      release,
		Converted:    migrations.ConvertedVersion,
		OldestSchema: migrations.OldestConvertible,
		ViaRelease:   migrations.LastGORMRelease,
		Migrations:   list,
	})
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(out), 0o755); err != nil {
		return err
	}
	f, err := os.Create(out)
	if err != nil {
		return err
	}
	if err := releasemanifest.Write(f, m); err != nil {
		f.Close()
		return err
	}
	return f.Close()
}
