package simulationcase

import (
	"time"

	"github.com/gsoultan/metis/server/domains/entities"
)

// ListCasesRequest names one process of a project.
type ListCasesRequest struct {
	ProjectID  string `json:"-"`
	ProcessKey string `json:"-"`
}

// ListCasesResponse is the process's cases, oldest first.
type ListCasesResponse struct {
	Cases []CaseView `json:"cases"`
	Err   error      `json:"err,omitzero"`
}

func (r ListCasesResponse) Failed() error { return r.Err }

// SaveCaseRequest creates a case under a process, or rewrites one by id.
//
// The project, process and id are taken from the address, never the body, so
// a case cannot be moved to another process by editing it.
type SaveCaseRequest struct {
	ProjectID  string `json:"-"`
	ProcessKey string `json:"-"`
	CaseID     string `json:"-"`

	Name     string                         `json:"name"`
	Scenario entities.SimulationScenario    `json:"scenario"`
	Expect   entities.SimulationExpectation `json:"expect"`
}

// SaveCaseResponse is the case as it was stored.
type SaveCaseResponse struct {
	Case *CaseView `json:"case,omitzero"`
	Err  error     `json:"err,omitzero"`
}

func (r SaveCaseResponse) Failed() error { return r.Err }

// DeleteCaseRequest names a case.
type DeleteCaseRequest struct {
	CaseID string `json:"-"`
}

// DeleteCaseResponse is empty on success.
type DeleteCaseResponse struct {
	Err error `json:"err,omitzero"`
}

func (r DeleteCaseResponse) Failed() error { return r.Err }

// CaseView is a case as the API shows it.
type CaseView struct {
	ID         string                         `json:"id"`
	ProjectID  string                         `json:"project_id"`
	ProcessKey string                         `json:"process_key"`
	Name       string                         `json:"name"`
	Scenario   entities.SimulationScenario    `json:"scenario"`
	Expect     entities.SimulationExpectation `json:"expect"`
	CreatedAt  time.Time                      `json:"created_at"`
	UpdatedAt  time.Time                      `json:"updated_at"`
}

// ViewOf maps a case to its view.
func ViewOf(c entities.SimulationCase) CaseView {
	view := CaseView{
		ID:         c.ID.String(),
		ProcessKey: c.ProcessKey,
		Name:       c.Name,
		Scenario:   c.Scenario,
		Expect:     c.Expect,
		CreatedAt:  c.CreatedAt,
		UpdatedAt:  c.UpdatedAt,
	}
	if c.Project != nil {
		view.ProjectID = c.Project.ID.String()
	}
	if view.Scenario.Variables == nil {
		view.Scenario.Variables = map[string]any{}
	}
	if view.Scenario.Answers == nil {
		view.Scenario.Answers = []entities.SimulationAnswer{}
	}
	return view
}
