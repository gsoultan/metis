package approval

import "github.com/gsoultan/metis/server/domains/entities"

// DefineLineRequest deploys an approval line in a project.
type DefineLineRequest struct {
	// ProjectID is taken from the address, never from the body.
	ProjectID string                `json:"-"`
	Line      entities.ApprovalLine `json:"-"`
}

// LineResponse is a line as deployed or read.
type LineResponse struct {
	Line *entities.ApprovalLine `json:"line,omitzero"`
	Err  error                  `json:"err,omitzero"`
}

func (r LineResponse) Failed() error { return r.Err }

// GetLineRequest reads the line a key's live version is.
type GetLineRequest struct {
	ProjectID string `json:"-"`
	Key       string `json:"-"`
}

// SubmitRequest starts a request on a line.
type SubmitRequest struct {
	ProjectID string         `json:"-"`
	Key       string         `json:"-"`
	Variables map[string]any `json:"variables"`
}

// RequestResponse is a request and where it is.
type RequestResponse struct {
	Request *entities.ApprovalRequest `json:"request,omitzero"`
	Err     error                     `json:"err,omitzero"`
}

func (r RequestResponse) Failed() error { return r.Err }

// GetRequestRequest reads a request.
type GetRequestRequest struct {
	InstanceID string `json:"-"`
}

// DecideRequest decides an open task of a line.
type DecideRequest struct {
	// TaskID is taken from the address, never from the body.
	TaskID    string         `json:"-"`
	Decision  string         `json:"decision"`
	Comment   string         `json:"comment"`
	Variables map[string]any `json:"variables"`
}

// ListRequestsRequest lists a line's requests.
type ListRequestsRequest struct {
	ProjectID   string `json:"-"`
	Key         string `json:"-"`
	SubmittedBy string `json:"-"`
}

// ListRequestsResponse is a line's requests, newest first.
type ListRequestsResponse struct {
	Requests []entities.ApprovalRequest `json:"requests"`
	Err      error                      `json:"err,omitzero"`
}

func (r ListRequestsResponse) Failed() error { return r.Err }

// BehindRequest asks for the open requests on a line's older versions.
type BehindRequest struct {
	ProjectID string `json:"-"`
	Key       string `json:"-"`
}

// BehindResponse is those requests, version by version, with what moving
// them would do.
type BehindResponse struct {
	Takeover *entities.ApprovalTakeover `json:"takeover,omitzero"`
	Err      error                      `json:"err,omitzero"`
}

func (r BehindResponse) Failed() error { return r.Err }

// MoveRequest moves open requests onto the line's live version.
type MoveRequest struct {
	ProjectID string   `json:"-"`
	Key       string   `json:"-"`
	Requests  []string `json:"requests"`
}

// MoveResponse is what the move did.
type MoveResponse struct {
	Result *entities.ApprovalMoveResult `json:"result,omitzero"`
	Err    error                        `json:"err,omitzero"`
}

func (r MoveResponse) Failed() error { return r.Err }
