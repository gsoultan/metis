// Package people is the API People use: signing in to their project and, from
// there, the work processes give them. Users have their own endpoints and
// cannot reach these, as People cannot reach theirs.
package people

import (
	"context"
	"fmt"

	"github.com/go-kit/kit/endpoint"
	"github.com/google/uuid"
	"github.com/gsoultan/metis/internal/pkg/apierr"
	"github.com/gsoultan/metis/internal/pkg/auth"
	"github.com/gsoultan/metis/server/domains/services"
	authinterceptor "github.com/gsoultan/metis/server/interceptors/auth"
)

type Endpoints struct {
	SignIn endpoint.Endpoint
	Me     endpoint.Endpoint
}

func MakeEndpoints(s services.ServiceFacade) Endpoints {
	return Endpoints{
		SignIn: MakeSignInEndpoint(s),
		Me:     MakeMeEndpoint(),
	}
}

func MakeSignInEndpoint(s services.ServiceFacade) endpoint.Endpoint {
	return func(ctx context.Context, request any) (any, error) {
		req, ok := request.(SignInRequest)
		if !ok {
			return nil, fmt.Errorf("people: expected a SignInRequest, got %T", request)
		}
		projectID, err := uuid.Parse(req.ProjectID)
		if err != nil {
			return SignInResponse{Err: apierr.Invalidf("project_id %q is not a valid identifier", req.ProjectID)}, nil
		}
		person, token, expires, err := s.SignInPerson(ctx, projectID, req.Username, req.Password)
		if err != nil {
			return SignInResponse{Err: err}, nil
		}
		return SignInResponse{Person: &person, Token: token, ExpiresAt: expires}, nil
	}
}

// MakeMeEndpoint answers who the token signs in, as the directory has them.
func MakeMeEndpoint() endpoint.Endpoint {
	return func(ctx context.Context, _ any) (any, error) {
		person, ok := authinterceptor.PersonFrom(ctx)
		if !ok {
			return MeResponse{Err: auth.ErrUnauthorized}, nil
		}
		return MeResponse{Person: person}, nil
	}
}
