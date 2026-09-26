package role

import (
	"context"

	"github.com/go-kit/kit/endpoint"
	"github.com/gsoultan/metis/server/domains/entities"
)

type Endpoints struct {
	// ListRoles answers what each role is required for.
	ListRoles endpoint.Endpoint
}

// MakeEndpoints serves the legend it is given.
//
// The legend is read from the gates once, when they have all been built, and
// is the same for every caller: nothing in it depends on who asks or which
// organization they are in.
func MakeEndpoints(access []entities.RoleAccess) Endpoints {
	return Endpoints{
		ListRoles: MakeListRolesEndpoint(access),
	}
}

func MakeListRolesEndpoint(access []entities.RoleAccess) endpoint.Endpoint {
	return func(context.Context, any) (any, error) {
		return ListRolesResponse{Roles: access}, nil
	}
}
