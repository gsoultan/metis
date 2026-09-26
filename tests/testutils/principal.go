package testutils

import (
	"context"

	pkgauth "github.com/gsoultan/metis/internal/pkg/auth"
	"github.com/gsoultan/metis/server/domains/entities"
)

// AsOperator returns ctx carrying a signed-in operator with the username, the
// way the auth interceptor leaves a request.
//
// A task nobody was named for — no assignee, no candidates — is the
// administrators' and operators' to take, and the service reads who is asking
// from the context, as every role check does. A scenario test that takes such a
// task to move its process on takes it as an operator: the least anybody needs
// to take it, and the person who would in the business.
func AsOperator(ctx context.Context, username string) context.Context {
	return context.WithValue(ctx, pkgauth.UserContextKey,
		entities.User{Username: username, Roles: []string{entities.RoleOperator}})
}
