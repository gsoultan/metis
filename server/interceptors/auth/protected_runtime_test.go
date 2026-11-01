package auth

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"
	"github.com/gsoultan/metis/internal/pkg/apierr"
	pkgauth "github.com/gsoultan/metis/internal/pkg/auth"
	"github.com/gsoultan/metis/server/domains/entities"
)

func callAs(t *testing.T, roles []string, protected bool, protectionErr error) error {
	t.Helper()
	ctx := context.WithValue(context.Background(), pkgauth.UserContextKey,
		entities.User{ID: uuid.New(), Roles: roles})
	gate := NewProtectedRuntimeInterceptor(func(context.Context) (bool, error) { return protected, protectionErr })
	reached := false
	_, err := gate.Intercept(func(context.Context, any) (any, error) {
		reached = true
		return nil, nil
	})(ctx, nil)
	if err == nil && !reached {
		t.Fatal("the gate let the call through without calling the endpoint")
	}
	return err
}

func TestADesignerChangesModelsOnlyOutsideAProtectedRuntime(t *testing.T) {
	designer := []string{entities.RoleDesigner}
	if err := callAs(t, designer, false, nil); err != nil {
		t.Fatalf("a designer in an unprotected runtime was refused: %v", err)
	}
	err := callAs(t, designer, true, nil)
	if !errors.Is(err, apierr.ErrForbidden) {
		t.Fatalf("a designer in a protected runtime got %v, want forbidden", err)
	}
}

func TestAReleaserChangesModelsOnlyInAProtectedRuntime(t *testing.T) {
	releaser := []string{entities.RoleReleaser}
	if err := callAs(t, releaser, true, nil); err != nil {
		t.Fatalf("a releaser in a protected runtime was refused: %v", err)
	}
	if err := callAs(t, releaser, false, nil); !errors.Is(err, apierr.ErrForbidden) {
		t.Fatalf("a releaser alone in an unprotected runtime got %v, want forbidden", err)
	}
}

func TestAnAdministratorChangesModelsAnywhere(t *testing.T) {
	admin := []string{entities.RoleAdmin}
	for _, protected := range []bool{false, true} {
		if err := callAs(t, admin, protected, nil); err != nil {
			t.Fatalf("an administrator (protected=%v) was refused: %v", protected, err)
		}
	}
}

func TestARuntimeThatCannotBeToldIsTreatedAsClosed(t *testing.T) {
	err := callAs(t, []string{entities.RoleAdmin}, false, errors.New("row unreadable"))
	if !errors.Is(err, apierr.ErrForbidden) {
		t.Fatalf("an unknown runtime got %v, want forbidden", err)
	}
}
