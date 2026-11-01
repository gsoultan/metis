package auth

import (
	"context"

	"github.com/go-kit/kit/endpoint"
	"github.com/gsoultan/metis/internal/pkg/apierr"
	pkgauth "github.com/gsoultan/metis/internal/pkg/auth"
	"github.com/gsoultan/metis/server/domains/entities"
	"github.com/gsoultan/metis/server/interceptors/contracts"
)

// RuntimeProtection reports whether the runtime a request works in is
// protected. An error means it could not be told.
type RuntimeProtection func(ctx context.Context) (bool, error)

// protectedRuntimeInterceptor decides who may change a runtime's models by
// whether that runtime is protected.
//
// The role gate in front of it admits the union — an administrator, a
// designer or a releaser — because which of them counts is not known until
// the runtime is: in an unprotected one a designer may and a releaser who is
// not also a designer may not, and in a protected one it is the other way
// round. An administrator may in both.
type protectedRuntimeInterceptor struct {
	protected RuntimeProtection
}

// NewProtectedRuntimeInterceptor returns the gate for changing models: in a
// protected runtime it takes a releaser or an administrator, elsewhere a
// designer or an administrator. Belongs after the tenant resolver, because the
// roles that count are the ones held in the organization the request is for.
//
// When the runtime cannot be told — its row unreadable, the setting
// misspelled — the request is refused: failing open here is how production
// gets edited by accident.
func NewProtectedRuntimeInterceptor(protected RuntimeProtection) contracts.EndpointInterceptor {
	return &protectedRuntimeInterceptor{protected: protected}
}

func (i *protectedRuntimeInterceptor) Intercept(next endpoint.Endpoint) endpoint.Endpoint {
	return func(ctx context.Context, request any) (any, error) {
		caller, err := callerFromContext(ctx)
		if err != nil {
			return nil, pkgauth.ErrUnauthorized
		}
		protected, err := i.protected(ctx)
		if err != nil {
			return nil, apierr.Forbiddenf("could not tell whether this runtime is protected, so its models stay as they are: %v", err)
		}
		organization := entities.ActingOrganization(ctx)
		holds := func(role string) bool { return caller.HoldsRoleIn(organization, role) }

		if holds(entities.RoleAdmin) {
			return next(ctx, request)
		}
		if protected {
			if holds(entities.RoleReleaser) {
				return next(ctx, request)
			}
			return nil, apierr.Forbiddenf("this environment is protected: its models change only as a release, " +
				"made live by a RELEASER or an ADMIN; design and test it in an unprotected environment first")
		}
		if holds(entities.RoleDesigner) {
			return next(ctx, request)
		}
		return nil, apierr.Forbiddenf("this needs the ADMIN or DESIGNER role, which your account does not hold in this organization; " +
			"a RELEASER changes models only in a protected environment")
	}
}
