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
