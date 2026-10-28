package user_test

import (
	"errors"
	"strings"
	"testing"

	"github.com/gsoultan/metis/internal/pkg/apierr"
	"github.com/gsoultan/metis/server/domains/entities"
	service_impl "github.com/gsoultan/metis/server/domains/services/impl"
	"github.com/gsoultan/metis/server/repositories"
	"github.com/gsoultan/metis/tests/testutils"
)

// A new account takes the same password floor a reset and a change do.
//
// Only the UI's form refused a short password: through the API an account
// could be created with an empty one, and then signed in with an empty one.
// A password over bcrypt's 72 bytes was a 500 rather than the caller's mistake.
func TestANewAccountNeedsAPasswordItCanSignInWith(t *testing.T) {
	ctx := t.Context()
	db := testutils.SetupTestDB(t)

	repo := repositories.NewRepository(testutils.StormConn(db))
	orgSvc := service_impl.NewOrganizationService(repo, nil)
	userSvc := service_impl.NewUserService(repo, "test-jwt-secret")

	org, err := orgSvc.CreateOrganization(ctx, "Password Org", "")
	if err != nil {
		t.Fatalf("failed to create org: %v", err)
	}
	ctx = entities.WithTenantContext(ctx, entities.TenantContext{TenantID: org.ID.String()})

	refused := []struct {
		name     string
		password string
	}{
		{name: "empty", password: ""},
		{name: "one character", password: "a"},
		{name: "seven characters", password: "abcdefg"},
		{name: "eight spaces", password: "        "},
		{name: "over 72 bytes", password: strings.Repeat("x", 73)},
	}
	for _, tc := range refused {
		t.Run(tc.name, func(t *testing.T) {
			username := "refused-" + strings.ReplaceAll(tc.name, " ", "-")
			err := userSvc.CreateUser(ctx, entities.User{
				Organizations: []*entities.Organization{{ID: org.ID}},
				Username:      username,
				FullName:      "Refused",
			}, tc.password)
			if !errors.Is(err, apierr.ErrInvalidArgument) {
				t.Fatalf("creating an account with a %s password: got %v, want an invalid argument", tc.name, err)
			}
			if _, _, err := userSvc.Login(ctx, username, tc.password); err == nil {
				t.Fatalf("an account refused at creation signed in with a %s password", tc.name)
			}
		})
	}

	if err := userSvc.CreateUser(ctx, entities.User{
		Organizations: []*entities.Organization{{ID: org.ID}},
		Username:      "accepted",
		FullName:      "Accepted",
	}, "eight-ch"); err != nil {
		t.Fatalf("an eight-character password was refused: %v", err)
	}
	if _, _, err := userSvc.Login(ctx, "accepted", "eight-ch"); err != nil {
		t.Fatalf("the account created with an eight-character password cannot sign in: %v", err)
	}
}
