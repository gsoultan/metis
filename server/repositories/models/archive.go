package models

import "time"

// ArchivedInstanceModel is what the archive's list shows of one archived
// instance: everything but the record, which is opened one at a time.
//
// ID is the instance's own id, and CreatedAt when it was archived.
type ArchivedInstanceModel struct {
	Base
	ProjectID      UUID          `json:"project_id,omitzero"`
	DefinitionID   UUID          `json:"definition_id,omitzero"`
	ProcessKey     string        `json:"process_key"`
	ProcessName    string        `json:"process_name"`
	ProcessVersion int           `json:"process_version"`
	BusinessKey    string        `json:"business_key,omitzero"`
	Status         ProcessStatus `json:"status"`
	StartedAt      time.Time     `json:"started_at"`
	FinishedAt     time.Time     `json:"finished_at"`
}

// ArchiveRecordVersion is the shape of the record this release writes. A
// record says which shape it is, so a later release can still read one
// written now.
const ArchiveRecordVersion = 1

// ArchiveRecord is everything kept of an archived instance, as it last stood
// in the live tables: the values, not the sealed or offloaded forms they were
// stored in, so the record reads the same whatever happens to the payload
// directory afterwards.
type ArchiveRecord struct {
	Version         int                     `json:"version"`
	Instance        ProcessInstanceModel    `json:"instance"`
	Tasks           []TaskModel             `json:"tasks"`
	ServiceCalls    []ServiceCallModel      `json:"service_calls"`
	Incidents       []IncidentModel         `json:"incidents"`
	Notifications   []NotificationModel     `json:"notifications"`
	VariableHistory []VariableSnapshotModel `json:"variable_history"`
	AuditTrail      []AuditModel            `json:"audit_trail"`
}
