package setup_test

import (
	"errors"
	"testing"

	"github.com/gsoultan/metis/internal/pkg/apierr"
	"github.com/gsoultan/metis/server/domains/services/contracts"
	"github.com/gsoultan/metis/server/domains/services/impl"
)

// The first administrator's password takes the floor every account's does.
//
// The wizard's form checked it and the API did not, so a scripted setup could
// create the administrator of the whole installation with a one-character
// password. Refused before any database is opened: the one named here does
// not answer, so reaching it would fail some other way.
func TestSetupRefusesAShortAdministratorPassword(t *testing.T) {
	t.Setenv("DATABASE_URL", "postgres://metis:metis@127.0.0.1:1/metis?sslmode=disable&connect_timeout=1")
	t.Setenv("ENCRYPTION_KEY", strongSecret(t))
	t.Setenv("JWT_SECRET", strongSecret(t))
	t.Chdir(t.TempDir())

	err := impl.NewSetupService(heldInstallation(false)).Setup(t.Context(), contracts.SetupRequest{
		AdminUsername: "admin", AdminPassword: "a",
		AdminFullName: "Admin", AdminPublicName: "Admin", AdminEmail: "admin@example.invalid",
		OrganizationName: "Example Co",
	})
	if !errors.Is(err, apierr.ErrInvalidArgument) {
		t.Fatalf("setup with a one-character administrator password: got %v, want an invalid argument", err)
	}
}
