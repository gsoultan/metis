package people

import (
	"time"

	"github.com/gsoultan/metis/server/domains/entities"
)

// SignInRequest is a person signing in to their project.
type SignInRequest struct {
	ProjectID string `json:"project_id"`
	Username  string `json:"username"`
	Password  string `json:"password"`
}

type SignInResponse struct {
	Person    *entities.Person `json:"person,omitzero"`
	Token     string           `json:"token,omitzero"`
	ExpiresAt time.Time        `json:"expires_at,omitzero"`
	Err       error            `json:"err,omitzero"`
}

func (r SignInResponse) Failed() error { return r.Err }

// MeRequest asks who is signed in. Nothing in it: the token says.
type MeRequest struct{}

type MeResponse struct {
	Person entities.Person `json:"person"`
	Err    error           `json:"err,omitzero"`
}

func (r MeResponse) Failed() error { return r.Err }

// PersonTasksRequest asks for a page of the signed-in person's work.
type PersonTasksRequest struct {
	Page     int `json:"page,omitzero"`
	PageSize int `json:"page_size,omitzero"`
}

type PersonTasksResponse struct {
	Tasks    []entities.Task `json:"tasks"`
	Total    int64           `json:"total"`
	Page     int             `json:"page"`
	PageSize int             `json:"page_size"`
	HasMore  bool            `json:"has_more"`
	Err      error           `json:"err,omitzero"`
}

func (r PersonTasksResponse) Failed() error { return r.Err }

// PersonTaskRequest names one task, from the path.
type PersonTaskRequest struct {
	ID string `json:"-"`
}

type PersonTaskResponse struct {
	Task *entities.Task `json:"task,omitzero"`
	Err  error          `json:"err,omitzero"`
}

func (r PersonTaskResponse) Failed() error { return r.Err }

// CompletePersonTaskRequest is the form a person submits. Who submits it is
// the token's, never the body's.
type CompletePersonTaskRequest struct {
	ID        string         `json:"-"`
	Variables map[string]any `json:"variables,omitzero"`
}

// PersonTaskActionResponse answers a claim, a release or a completion.
type PersonTaskActionResponse struct {
	Err error `json:"err,omitzero"`
}

func (r PersonTaskActionResponse) Failed() error { return r.Err }

// TaskLinkRequest carries a task link's token in the body, never the address:
// an address is written to access logs, a body is not.
type TaskLinkRequest struct {
	Token string `json:"token"`
}

// TaskLinkResponse is what opening a link shows.
type TaskLinkResponse struct {
	Task      *entities.Task   `json:"task,omitzero"`
	Person    *entities.Person `json:"person,omitzero"`
	ExpiresAt time.Time        `json:"expires_at,omitzero"`
	Err       error            `json:"err,omitzero"`
}

func (r TaskLinkResponse) Failed() error { return r.Err }

// CompleteByTaskLinkRequest is the form submitted through a link. Who submits
// it is the link's person, never anything in the body.
type CompleteByTaskLinkRequest struct {
	Token     string         `json:"token"`
	Variables map[string]any `json:"variables,omitzero"`
}

type APIKeysRequest struct{}

type APIKeysResponse struct {
	Keys []entities.PersonAPIKey `json:"keys"`
	Err  error                   `json:"err,omitzero"`
}

func (r APIKeysResponse) Failed() error { return r.Err }

type CreateAPIKeyRequest struct {
	Name string `json:"name"`
}

// CreateAPIKeyResponse carries the key itself, the one time it is shown.
type CreateAPIKeyResponse struct {
	Key   *entities.PersonAPIKey `json:"key,omitzero"`
	Token string                 `json:"token,omitzero"`
	Err   error                  `json:"err,omitzero"`
}

func (r CreateAPIKeyResponse) Failed() error { return r.Err }

type RevokeAPIKeyRequest struct {
	ID string `json:"-"`
}
