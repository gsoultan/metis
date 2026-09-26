package models

import "time"

// DecisionReleaseModel is one entry in a decision key's release timeline: from
// ActivateAt onwards, an evaluation that names no version reads Version.
//
// Before it, "the version that evaluates" was simply the highest one, so saving
// a table and putting it into force were the same act, and the only way back
// from a bad edit was to type the old policy in again. The timeline makes the
// choice explicit and reversible, and it is the one process definitions already
// use — see ProcessDefinitionReleaseModel — so both kinds of model answer "which
// version is live" by the same rule: the newest entry whose moment has come.
//
// It says nothing about an evaluation pinned to a version, which reads that
// version whatever this table says.
type DecisionReleaseModel struct {
	Base
	ProjectID UUID `gorm:"index:idx_decision_release_timeline,unique,priority:2" json:"project_id,omitzero"`
	// Leads the unique index for the reason ProcessKey leads the process one:
	// the hot lookup filters on the key first. As long as the key it names.
	DecisionKey string `gorm:"size:255;index:idx_decision_release_timeline,unique,priority:1" json:"decision_key"`
	// ActivateAt is when this entry took over, in UTC. Part of the unique key,
	// so making a version live again is a new entry rather than an overwrite
	// of when the last change happened.
	ActivateAt time.Time `gorm:"index:idx_decision_release_timeline,unique,priority:3;not null" json:"activate_at"`
	Version    int       `gorm:"not null;default:0" json:"version"`
}

// TableName overrides the table name for DecisionReleaseModel.
func (DecisionReleaseModel) TableName() string {
	return "decision_releases"
}
