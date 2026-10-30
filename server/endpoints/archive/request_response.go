package archive

import (
	"time"

	"github.com/gsoultan/metis/server/repositories/models"
)

// ListArchivedRequest names the project whose archive is listed, and narrows
// it. Every field comes from the address.
type ListArchivedRequest struct {
	ProjectID   string `json:"-"`
	ProcessKey  string `json:"-"`
	BusinessKey string `json:"-"`
	// Cursor is the next_cursor of the page before.
	Cursor string `json:"-"`
	Limit  string `json:"-"`
}

// ListArchivedResponse is one page of a project's archive, most recently
// finished first. NextCursor is set while there may be more.
type ListArchivedResponse struct {
	Instances  []ArchivedView `json:"instances"`
	NextCursor string         `json:"next_cursor,omitzero"`
	Err        error          `json:"err,omitzero"`
}

func (r ListArchivedResponse) Failed() error { return r.Err }

// GetArchivedRequest names the archived instance to open.
type GetArchivedRequest struct {
	InstanceID string `json:"-"`
}

// GetArchivedResponse is an archived instance and everything kept of it.
type GetArchivedResponse struct {
	Instance *ArchivedView         `json:"instance,omitzero"`
	Record   *models.ArchiveRecord `json:"record,omitzero"`
	Err      error                 `json:"err,omitzero"`
}

func (r GetArchivedResponse) Failed() error { return r.Err }

// ArchivedView is an archived instance as the archive's list shows it.
type ArchivedView struct {
	ID             string    `json:"id"`
	ProjectID      string    `json:"project_id"`
	DefinitionID   string    `json:"definition_id"`
	ProcessKey     string    `json:"process_key"`
	ProcessName    string    `json:"process_name"`
	ProcessVersion int       `json:"process_version"`
	BusinessKey    string    `json:"business_key,omitzero"`
	Status         string    `json:"status"`
	StartedAt      time.Time `json:"started_at"`
	FinishedAt     time.Time `json:"finished_at"`
	ArchivedAt     time.Time `json:"archived_at"`
}

// ViewOf maps an archived instance to its view.
func ViewOf(m models.ArchivedInstanceModel) ArchivedView {
	return ArchivedView{
		ID:             m.ID.String(),
		ProjectID:      m.ProjectID.String(),
		DefinitionID:   m.DefinitionID.String(),
		ProcessKey:     m.ProcessKey,
		ProcessName:    m.ProcessName,
		ProcessVersion: m.ProcessVersion,
		BusinessKey:    m.BusinessKey,
		Status:         string(m.Status),
		StartedAt:      m.StartedAt,
		FinishedAt:     m.FinishedAt,
		ArchivedAt:     m.CreatedAt,
	}
}
