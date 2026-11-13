// Package releasemanifest describes what a release does to a database, in a
// form a host can read before it installs the release.
//
// Every tag publishes one as release.json beside the packages, and it is listed
// in checksums.txt, which the release signs. An installation that is about to
// upgrade reads it to answer the questions an operator asks first: will this
// change my database, and can it be reached from the version I run without a
// stop in between. Both answers come from the migration list compiled into the
// release, not from a file somebody keeps by hand, so they cannot drift from
// what the binary will actually do at its first boot.
package releasemanifest

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"slices"
)

// FormatVersion is the shape of the manifest. A reader that meets a format it
// does not know refuses the release rather than guessing what a field means.
//
// Adding a field does not change it: an installation reads the manifest of a
// release newer than itself, so it must ignore fields it has never heard of,
// or one added field would leave every older installation unable to upgrade.
// Bump it only when an existing field changes meaning.
const FormatVersion = 1

// Migration is one numbered schema change the release can apply.
type Migration struct {
	Version int64  `json:"version"`
	Name    string `json:"name"`
}

// Manifest is the content of release.json.
type Manifest struct {
	Format int `json:"format"`
	// Release is the tag, with its leading v, as the binary reports it.
	Release string `json:"release"`
	// SchemaVersion is the highest migration this release applies: a database
	// recorded below it will be changed at the first boot.
	SchemaVersion int64 `json:"schema_version"`
	// OldestSchema is the oldest recorded schema version this release can
	// upgrade. A database below it must first be started on ViaRelease.
	OldestSchema int64 `json:"oldest_schema"`
	// ViaRelease is the release that takes a database older than OldestSchema
	// to a version this release can start from.
	ViaRelease string `json:"via_release"`
	// Migrations lists every numbered step after the conversion, oldest first.
	Migrations []Migration `json:"migrations"`
}

// Inputs is what the release knows about its own migrations.
type Inputs struct {
	Release      string
	Converted    int64
	OldestSchema int64
	ViaRelease   string
	Migrations   []Migration
}

// Build assembles a manifest and checks it is one a reader can trust.
func Build(in Inputs) (Manifest, error) {
	if in.Release == "" {
		return Manifest{}, errors.New("release is empty: the manifest must name the tag it describes")
	}
	steps := slices.Clone(in.Migrations)
	slices.SortFunc(steps, func(a, b Migration) int { return int(a.Version - b.Version) })

	latest := in.Converted
	for i, m := range steps {
		if i > 0 && m.Version == steps[i-1].Version {
			return Manifest{}, fmt.Errorf("migration %d is listed twice", m.Version)
		}
		if m.Version <= in.Converted {
			return Manifest{}, fmt.Errorf("migration %d is not after the conversion at %d", m.Version, in.Converted)
		}
		latest = m.Version
	}
	if in.OldestSchema <= 0 || in.OldestSchema > latest {
		return Manifest{}, fmt.Errorf("oldest schema %d is outside 1..%d", in.OldestSchema, latest)
	}
	if steps == nil {
		steps = []Migration{}
	}
	return Manifest{
		Format:        FormatVersion,
		Release:       in.Release,
		SchemaVersion: latest,
		OldestSchema:  in.OldestSchema,
		ViaRelease:    in.ViaRelease,
		Migrations:    steps,
	}, nil
}

// Write encodes the manifest as indented JSON.
func Write(w io.Writer, m Manifest) error {
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(m)
}

// Read decodes a manifest and refuses one this reader does not understand.
func Read(r io.Reader) (Manifest, error) {
	var m Manifest
	if err := json.NewDecoder(io.LimitReader(r, 1<<20)).Decode(&m); err != nil {
		return Manifest{}, fmt.Errorf("read release manifest: %w", err)
	}
	if m.Format != FormatVersion {
		return Manifest{}, fmt.Errorf("release manifest format %d is not one this build reads (%d)", m.Format, FormatVersion)
	}
	if m.Release == "" || m.SchemaVersion <= 0 || m.OldestSchema <= 0 || m.OldestSchema > m.SchemaVersion {
		return Manifest{}, errors.New("release manifest is incomplete or inconsistent")
	}
	return m, nil
}

// Pending is the migrations a database recorded at version applied would run
// at this release's first boot.
func (m Manifest) Pending(applied int64) []Migration {
	var out []Migration
	for _, step := range m.Migrations {
		if step.Version > applied {
			out = append(out, step)
		}
	}
	return out
}

// Reachable says whether a database recorded at version applied can be
// started on this release directly. A database at version 0 has no schema yet
// and is created whole, so it is always reachable.
func (m Manifest) Reachable(applied int64) bool {
	return applied == 0 || applied >= m.OldestSchema
}
