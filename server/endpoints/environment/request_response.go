package environment

import (
	"github.com/google/uuid"
	"github.com/gsoultan/metis/server/domains/entities"
)

// ListEnvironmentsRequest asks for a project's runtimes.
type ListEnvironmentsRequest struct {
	ProjectID string `json:"project_id"`
}

type ListEnvironmentsResponse struct {
	Environments []entities.Environment `json:"environments"`
	Err          error                  `json:"err,omitzero"`
}

func (r ListEnvironmentsResponse) Failed() error { return r.Err }

// SaveEnvironmentRequest creates a runtime, or updates one when ID is set.
//
// One shape for both because the form is the same: everything a create needs is
// everything an update needs, and splitting them would mean keeping two field
// lists in step.
type SaveEnvironmentRequest struct {
	ID        string `json:"id,omitzero"`
	ProjectID string `json:"project_id"`
	Name      string `json:"name"`
	Port      int    `json:"port"`
	Driver    string `json:"driver"`
	// Connection carries host, port, username, password, db_name, ssl_enabled.
	// A credential sent back as the masking sentinel means "keep what is
	// stored"; see internal/pkg/configsecret.
	Connection map[string]any `json:"connection,omitzero"`
	Enabled    bool           `json:"enabled"`
	// Protected marks a runtime where only a releaser or an administrator may
	// change the models. Left out, an update keeps what is stored: a client
	// written before the field existed must not quietly unprotect production.
	Protected *bool `json:"protected,omitzero"`
}

type SaveEnvironmentResponse struct {
	ID  uuid.UUID `json:"id,omitzero"`
	Err error     `json:"err,omitzero"`
}

func (r SaveEnvironmentResponse) Failed() error { return r.Err }

// DeleteEnvironmentRequest removes a runtime from the registry.
type DeleteEnvironmentRequest struct {
	ID string `json:"id"`
}

type DeleteEnvironmentResponse struct {
	Err error `json:"err,omitzero"`
}

func (r DeleteEnvironmentResponse) Failed() error { return r.Err }

// TestEnvironmentConnectionRequest describes a database to try reaching.
//
// The same shape as a save, because it is the same form: an administrator
// presses Test with whatever they have typed so far. An id, when present, lets a
// masked password be resolved against the stored one.
type TestEnvironmentConnectionRequest struct {
	ID         string         `json:"id,omitzero"`
	Driver     string         `json:"driver"`
	Connection map[string]any `json:"connection,omitzero"`
}

type TestEnvironmentConnectionResponse struct {
	Reachable bool   `json:"reachable"`
	Detail    string `json:"detail,omitzero"`
	Err       error  `json:"err,omitzero"`
}

func (r TestEnvironmentConnectionResponse) Failed() error { return r.Err }
