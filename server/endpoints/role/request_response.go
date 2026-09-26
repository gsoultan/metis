package role

import "github.com/gsoultan/metis/server/domains/entities"

// ListRolesResponse carries every role with the actions it is required for.
type ListRolesResponse struct {
	Roles []entities.RoleAccess `json:"roles"`
	Err   error                 `json:"err,omitzero"`
}

func (r ListRolesResponse) Failed() error { return r.Err }
