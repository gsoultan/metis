package participant

import "github.com/gsoultan/metis/server/domains/entities"

// ListParticipantsRequest asks for a project's participants.
type ListParticipantsRequest struct {
	ProjectID string `json:"project_id"`
	// Limit is the most to answer with, alphabetically by username; zero means
	// everybody. One is how to ask whether the project has anybody without
	// downloading its whole directory.
	Limit int `json:"limit,omitzero"`
}

type ListParticipantsResponse struct {
	Participants []entities.WorkflowUser `json:"participants"`
	Err          error                   `json:"err,omitzero"`
}

func (r ListParticipantsResponse) Failed() error { return r.Err }

// ImportParticipantsRequest names where a directory should be read from.
//
// One request for all three sources, because the caller is answering one
// question — where does this list come from — and the reply is the same summary
// whichever they chose. The CSV body arrives separately, as an upload.
type ImportParticipantsRequest struct {
	ProjectID string `json:"project_id"`
	// Kind is csv, http or postgres.
	Kind string `json:"kind"`

	// CSV is the uploaded file's contents, read by the transport.
	CSV []byte `json:"-"`

	// HTTP.
	URL    string `json:"url,omitzero"`
	Method string `json:"method,omitzero"`

	// PostgreSQL. The DSN carries a password, so it is accepted but never
	// returned.
	DSN   string `json:"dsn,omitzero"`
	Query string `json:"query,omitzero"`
}

// ImportParticipantsResponse is the summary, flattened so a caller reads
// created/updated/problems without unwrapping.
type ImportParticipantsResponse struct {
	Created  int                      `json:"created"`
	Updated  int                      `json:"updated"`
	Groups   int                      `json:"groups"`
	Problems []entities.ImportProblem `json:"problems,omitzero"`
	Err      error                    `json:"err,omitzero"`
}

func (r ImportParticipantsResponse) Failed() error { return r.Err }

// RemoveParticipantRequest names somebody to take out of a project's directory.
//
// The project comes with it rather than being inferred from the id, so the
// service can check the two agree — an id on its own would let somebody who
// manages one project's directory remove a person from another's.
type RemoveParticipantRequest struct {
	ProjectID string `json:"project_id"`
	ID        string `json:"id"`
}

type RemoveParticipantResponse struct {
	Err error `json:"err,omitzero"`
}

func (r RemoveParticipantResponse) Failed() error { return r.Err }
