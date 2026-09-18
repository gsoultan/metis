package models

import "time"

// ProcessDefinitionReleaseModel is one entry in a process key's release
// timeline: from ActivateAt onwards, new instances start on Version.
//
// Without it "the live version" was simply whichever sorted highest, so saving a
// model and promoting it were the same act: there was no way to stage a version
// for review, and no way to roll a bad one back except to deploy the old model
// again under a higher number.
//
// A timeline rather than a single current-version row, because that is what
// makes a cutover something you can arrange in advance. The live version is a
// pure function of (rows, now) — the newest row whose time has come — so no
// scheduler has to wake up and change anything, nothing is missed because a
// replica was restarting at the wrong moment, and every replica agrees without
// coordinating. The rows left behind are also the honest audit of when each
// version took over.
//
// It says nothing about instances already running. Those pin their definition by
// ID and drain on the version they started on whatever this table says — see
// ProcessInstanceModel.DefinitionID.
type ProcessDefinitionReleaseModel struct {
	Base
	ProjectID UUID `gorm:"index:idx_definition_release_timeline,unique,priority:2" json:"project_id,omitzero"`
	// The column is process_key, not key: `key` is reserved in MySQL, and every
	// query that touches such a column has to be written as a map so GORM will
	// quote it per dialect. Naming it out of the way costs nothing here.
	//
	// 191 rather than 255 so the composite unique index fits MySQL's index key
	// length under utf8mb4 without needing a prefix length.
	//
	// It leads the unique index, ahead of project_id, because of how the index
	// is read rather than what it enforces. Uniqueness is over the whole triple
	// either way; but the hot lookup is "which version of this key is live",
	// which runs on every process start and filters on process_key first — the
	// tenant arrives as a join, not as a predicate on this table. Leading with
	// project_id would leave that query no usable prefix and make it a scan.
	ProcessKey string `gorm:"size:191;index:idx_definition_release_timeline,unique,priority:1" json:"process_key"`
	// ActivateAt is when this entry takes over, in UTC. A time in the past means
	// it already has; a time in the future is a cutover somebody has arranged.
	//
	// Part of the unique key so that two versions cannot claim the same instant,
	// and so that promoting again is a new entry rather than an overwrite of when
	// the last change happened.
	ActivateAt time.Time `gorm:"index:idx_definition_release_timeline,unique,priority:3;not null" json:"activate_at"`
	// Version is the version number rather than a definition ID so a release can
	// be read against the same (project_id, key, version) series the definitions
	// are indexed by, in one lookup.
	//
	// A release naming a version that has since been deleted is not an error:
	// the reader falls back to the highest version, which is what the engine did
	// before releases existed.
	Version int `gorm:"not null;default:0" json:"version"`
}

// TableName overrides the table name for ProcessDefinitionReleaseModel.
func (ProcessDefinitionReleaseModel) TableName() string {
	return "process_definition_releases"
}

// Scheduled reports whether this entry is a cutover that has not happened yet,
// as of now. Only these can be cancelled: an entry whose time has come is
// history, and deleting it would silently rewrite which version was in force.
func (r ProcessDefinitionReleaseModel) Scheduled(now time.Time) bool {
	return r.ActivateAt.After(now)
}
