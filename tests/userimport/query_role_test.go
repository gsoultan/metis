package userimport_test

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"
	"github.com/gsoultan/metis/internal/pkg/apierr"
	pkgauth "github.com/gsoultan/metis/internal/pkg/auth"
	"github.com/gsoultan/metis/server/domains/entities"
)

// callerWith is ctx on behalf of somebody holding roles.
func callerWith(ctx context.Context, roles ...string) context.Context {
	return context.WithValue(ctx, pkgauth.UserContextKey, entities.User{ID: uuid.New(), Roles: roles})
}

// Importing people with a database query runs the caller's SQL against a
// database the caller names. A designer may import a file or an endpoint, and
// was able to do this too; it now takes what a database lookup takes.
func TestADatabaseImportNeedsTheQueryAuthorRole(t *testing.T) {
	ctx, svc, projectID := fixture(t)
	const dsn, query = "postgres://nobody:x@127.0.0.1:1/none?sslmode=disable", "SELECT 1 AS username"

	for _, refused := range [][]string{{entities.RoleDesigner}, {entities.RoleOperator}, nil} {
		_, err := svc.SyncWorkflowUsersFromPostgres(callerWith(ctx, refused...), projectID, dsn, query)
		if !errors.Is(err, apierr.ErrForbidden) {
			t.Fatalf("roles %v should be refused before anything is dialled, got %v", refused, err)
		}
	}
	// No caller at all is refused too: absent means deny.
	if _, err := svc.SyncWorkflowUsersFromPostgres(context.Background(), projectID, dsn, query); !errors.Is(err, apierr.ErrForbidden) {
		t.Fatalf("a call with nobody behind it should be refused, got %v", err)
	}

	for _, allowed := range [][]string{{entities.RoleQueryAuthor}, {entities.RoleAdmin}} {
		_, err := svc.SyncWorkflowUsersFromPostgres(callerWith(ctx, allowed...), projectID, dsn, query)
		if err == nil || errors.Is(err, apierr.ErrForbidden) {
			t.Fatalf("roles %v should reach the database and fail there, got %v", allowed, err)
		}
	}
}
