package user_test

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"
	"github.com/gsoultan/metis/server/domains/entities"
	service_impl "github.com/gsoultan/metis/server/domains/services/impl"
	"github.com/gsoultan/metis/server/repositories"
	"github.com/gsoultan/metis/tests/testutils"
)

// recordingCredentials stands in for an embedding program's Identity.
type recordingCredentials struct {
	failSetPassword bool
	passwords       map[uuid.UUID]string
	removed         []uuid.UUID
}

func (c *recordingCredentials) SetPassword(_ context.Context, userID uuid.UUID, plain string) error {
	if c.failSetPassword {
		return errors.New("the identity store is down")
	}
	if c.passwords == nil {
		c.passwords = map[uuid.UUID]string{}
	}
	c.passwords[userID] = plain
	return nil
}

func (c *recordingCredentials) RemoveSecondFactor(_ context.Context, userID uuid.UUID) error {
	c.removed = append(c.removed, userID)
	return nil
}

// With an Identity, the engine keeps no password of its own: a new account's
// and a reset one go to the Identity, the engine's column holds a value no
// password matches, and the person's own sign-in is refused here because the
// program in front of the engine serves it. An account the Identity could not
// give a password to is not left behind.
func TestAnIdentityHoldsEveryPasswordTheEngineSets(t *testing.T) {
	ctx := t.Context()
	db := testutils.SetupTestDB(t)
	repo := repositories.NewRepository(db)
	orgSvc := service_impl.NewOrganizationService(repo, nil)
	creds := &recordingCredentials{}
	userSvc := service_impl.NewUserService(repo, "test-jwt-secret", service_impl.WithCredentials(creds))

	org, err := orgSvc.CreateOrganization(ctx, "Identity Org", "")
	if err != nil {
		t.Fatalf("create the organization: %v", err)
	}
	ctx = entities.WithTenantContext(ctx, entities.TenantContext{TenantID: org.ID.String()})
	member := func(name string) entities.User {
		return entities.User{Organizations: []*entities.Organization{{ID: org.ID}}, Username: name, FullName: name}
	}

	if err := userSvc.CreateUser(ctx, member("ada"), "first-password"); err != nil {
		t.Fatalf("create an account: %v", err)
	}
	stored, hash, err := repo.User().GetWithPasswordByUsername(ctx, "ada")
	if err != nil {
		t.Fatalf("read the account back: %v", err)
	}
	ada := uuid.UUID(stored.ID)
	if creds.passwords[ada] != "first-password" {
		t.Fatalf("the Identity was given %q for the new account, want its password", creds.passwords[ada])
	}
	if hash != "!identity" {
		t.Fatalf("the engine stored %q as the password hash, want the unusable marker", hash)
	}
	if _, _, err := userSvc.Login(ctx, "ada", "first-password"); err == nil {
		t.Fatal("the engine signed an account in with a password only the Identity holds")
	}
	if err := userSvc.ChangePassword(ctx, ada, "first-password", "second-password"); !errors.Is(err, service_impl.ErrSignInIsElsewhere) {
		t.Fatalf("changing one's own password: got %v, want it refused as served elsewhere", err)
	}

	if err := userSvc.SetPassword(ctx, "ada", "reset-password"); err != nil {
		t.Fatalf("--reset-password: %v", err)
	}
	if creds.passwords[ada] != "reset-password" {
		t.Fatalf("--reset-password did not reach the Identity: it holds %q", creds.passwords[ada])
	}
	if _, hash, _ := repo.User().GetWithPasswordByUsername(ctx, "ada"); hash != "!identity" {
		t.Fatalf("--reset-password wrote %q into the engine's column", hash)
	}

	creds.failSetPassword = true
	if err := userSvc.CreateUser(ctx, member("grace"), "first-password"); err == nil {
		t.Fatal("an account was created although the Identity could not hold its password")
	}
	if _, err := repo.User().GetByUsername(ctx, "grace"); err == nil {
		t.Fatal("an account the Identity could not give a password to was left behind")
	}
}
