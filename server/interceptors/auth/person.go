package auth

import (
	"context"

	"github.com/go-kit/kit/endpoint"
	"github.com/gsoultan/metis/internal/pkg/auth"
	"github.com/gsoultan/metis/server/domains/entities"
	"github.com/gsoultan/metis/server/interceptors/contracts"
)

// personRequired refuses a request that carries no signed-in person.
//
// The transport puts one there only on People routes and only from a People
// token. Checked again at the endpoint so that a People endpoint wired onto
// another route, or reached some other way, finds nobody rather than trusting
// that the route was right.
type personRequired struct{}

// NewRequirePerson admits only a signed-in person.
func NewRequirePerson() contracts.EndpointInterceptor {
	return personRequired{}
}

func (personRequired) Intercept(next endpoint.Endpoint) endpoint.Endpoint {
	return func(ctx context.Context, request any) (any, error) {
		if _, ok := PersonFrom(ctx); !ok {
			return nil, auth.ErrUnauthorized
		}
		return next(ctx, request)
	}
}

// PersonFrom returns the person signed in on this request, if there is one.
func PersonFrom(ctx context.Context) (entities.Person, bool) {
	person, ok := ctx.Value(auth.PersonContextKey).(entities.Person)
	return person, ok
}
