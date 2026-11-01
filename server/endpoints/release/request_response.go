package release

import (
	"encoding/json"
	"time"

	"github.com/gsoultan/metis/server/domains/entities"
)

// CutRequest asks for a release of a project's live models.
type CutRequest struct {
	ProjectID string   `json:"-"`
	Name      string   `json:"name"`
	Processes []string `json:"processes"`
	Decisions []string `json:"decisions"`
}

// ImportRequest names a release in another environment of the project. An
// empty environment is the main runtime.
type ImportRequest struct {
	ProjectID   string `json:"-"`
	Environment string `json:"from_environment"`
	Name        string `json:"name"`
}

// ImportFileRequest carries a release file another installation exported.
type ImportFileRequest struct {
	ProjectID string          `json:"-"`
	File      json.RawMessage `json:"file"`
}

// ExportResponse is a release file, signed, as another installation imports
// it.
type ExportResponse struct {
	File json.RawMessage `json:"file,omitzero"`
	Err  error           `json:"err,omitzero"`
}

func (r ExportResponse) Failed() error { return r.Err }

// NamedRequest names one release of a project.
type NamedRequest struct {
	ProjectID string `json:"-"`
	Name      string `json:"-"`
}

// ProjectRequest names a project.
type ProjectRequest struct {
	ProjectID string `json:"-"`
}

// ReleaseResponse is one release.
type ReleaseResponse struct {
	Release *View `json:"release,omitzero"`
	Err     error `json:"err,omitzero"`
}

func (r ReleaseResponse) Failed() error { return r.Err }

// ListResponse is a project's releases, newest first.
type ListResponse struct {
	Releases []View `json:"releases"`
	Err      error  `json:"err,omitzero"`
}

func (r ListResponse) Failed() error { return r.Err }

// View is a release as the API shows it: what it carries, not the models
// themselves.
type View struct {
	ID          string                 `json:"id"`
	ProjectID   string                 `json:"project_id"`
	Name        string                 `json:"name"`
	Hash        string                 `json:"hash"`
	Origin      string                 `json:"origin"`
	State       string                 `json:"state"`
	Items       []entities.ReleaseItem `json:"items"`
	Connections []string               `json:"connections"`
	CutAt       time.Time              `json:"cut_at"`
	CutBy       string                 `json:"cut_by"`
	ActedBy     string                 `json:"acted_by"`
	ImportedBy  string                 `json:"imported_by,omitzero"`
	ApprovedBy  string                 `json:"approved_by,omitzero"`
	LiveAt      *time.Time             `json:"live_at,omitzero"`
	Previous    string                 `json:"previous,omitzero"`
	CreatedAt   time.Time              `json:"created_at"`
}

// ViewOf is the API's view of a release.
func ViewOf(r entities.Release) View {
	v := View{
		ID: r.ID.String(), Name: r.Name, Hash: r.Hash,
		Origin: string(r.Origin), State: string(r.State),
		Items: r.Items, Connections: r.Bundle.Connections,
		CutAt: r.Bundle.CutAt, CutBy: r.CutBy, ActedBy: r.ActedBy, ImportedBy: r.ImportedBy, ApprovedBy: r.ApprovedBy,
		LiveAt: r.LiveAt, Previous: r.Previous, CreatedAt: r.CreatedAt,
	}
	if r.Project != nil {
		v.ProjectID = r.Project.ID.String()
	}
	if v.Items == nil {
		v.Items = []entities.ReleaseItem{}
	}
	if v.Connections == nil {
		v.Connections = []string{}
	}
	return v
}
