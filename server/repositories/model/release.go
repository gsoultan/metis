package model

import (
	"time"

	"github.com/gsoultan/storm"
)

// Release is a set of process and decision versions that move between
// environments together, unchanged: cut once where they were designed and
// tested, then imported, checked and made live in each environment they
// reach.
//
// A row is the release as one runtime knows it. Where it was cut, Origin is
// "cut" and Items are the versions it was cut from. Where it was imported,
// Origin is "imported" and Items are the versions the import wrote there,
// which have that runtime's own numbers. Bundle is the release itself, whole,
// and Hash is the SHA-256 of it, so two runtimes can tell they hold the same
// bytes.
type Release struct {
	storm.Model

	Project Project
	// Name is what people call it, unique within the project in this runtime.
	Name string
	Hash string
	// Origin is "cut" or "imported".
	Origin string
	// State is "cut", "staged" (imported, not live), "live" or "superseded".
	State string
	// Bundle is the release as it travels: its processes, decisions and
	// simulation cases, with steps naming connections by name.
	Bundle storm.JSON
	// Items are the versions this release is in this runtime.
	Items storm.JSON
	// CutBy is who cut it; ActedBy who last imported it, made it live or
	// rolled it back here.
	CutBy   string
	ActedBy string
	LiveAt  *time.Time
	// Previous is the name of the release that was live before this one went
	// live here, which a rollback makes live again.
	Previous string
}

func (r *Release) Schema(t *storm.Table) {
	t.Name("releases")
	t.Col(&r.Name).Size(255)
	t.Col(&r.Hash).Size(64)
	t.Col(&r.Origin).Size(16)
	t.Col(&r.State).Size(16)
	t.Col(&r.CutBy).Size(255)
	t.Col(&r.ActedBy).Size(255)
	t.Col(&r.Previous).Size(255)
	t.Unique(&r.Project, &r.Name)
	// A project's releases, newest first.
	t.Index(&r.Project, &r.CreatedAt, &r.ID).Named("ix_releases_project_created")
}
