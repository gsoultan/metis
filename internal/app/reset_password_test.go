package app

import (
	"strings"
	"testing"

	"github.com/gsoultan/metis/server/domains/observers/impl"
	"github.com/gsoultan/metis/server/domains/services"
	"github.com/gsoultan/metis/server/repositories"
	"github.com/gsoultan/metis/server/repositories/models"
	"github.com/gsoultan/metis/tests/testutils"
	"golang.org/x/crypto/bcrypt"
)

// --reset-password sets a password on the account an operator names. On an
// account that signs in through an identity provider, that password is a
// second way in which the provider does not control: it keeps working after
// the provider disables the person, and it skips whatever the provider asks
// for at sign-in. So it is refused, the refusal names the provider to reset
// the password at, and nothing about the account changes.
func TestResetPasswordRefusesAnAccountLinkedToAnIdentityProvider(t *testing.T) {
	const (
		issuer = "https://id.example.com/realms/acme"
		chosen = "a-password-the-provider-never-sees"
	)
	gormDB := testutils.SetupTestDB(t)
	repo := repositories.NewRepository(testutils.StormConn(gormDB))
	a := &App{repo: repo, svc: services.NewServiceFacade(repo, impl.NewEventDispatcher(), impl.NewSSEObserver(),
		"reset-password-test-secret", nil, nil, nil)}
	ctx := t.Context()
	if _, err := repo.User().CreateLinked(ctx, models.UserModel{Username: "ada", Email: "ada@acme.example"},
		issuer, "subject-ada"); err != nil {
		t.Fatalf("create the account a first sign-in through the provider creates: %v", err)
	}
	t.Setenv(envResetPassword, chosen)

	err := a.handleResetPassword(ctx, "ada")

	if err == nil {
		t.Error("--reset-password set a password on an account that signs in through " + issuer)
	} else if msg := err.Error(); !strings.Contains(msg, issuer) || !strings.Contains(msg, "reset it there") {
		t.Errorf("the refusal does not name the provider and send the operator there: %s", msg)
	}
	stored, hash, readErr := repo.User().GetWithPasswordByUsername(ctx, "ada")
	if readErr != nil {
		t.Fatalf("read the account back: %v", readErr)
	}
	if hash != "" || stored.TokensValidFrom != nil {
		t.Errorf("the account changed: password set %v, credential cutoff %v; want neither",
			bcrypt.CompareHashAndPassword([]byte(hash), []byte(chosen)) == nil, stored.TokensValidFrom)
	}
	if _, _, loginErr := a.svc.Login(ctx, "ada", chosen); loginErr == nil {
		t.Error("the password --reset-password was given signs the account in")
	}
}

// The command is how an operator gets back into an installation nobody can
// sign in to, so a local account's reset is untouched.
func TestResetPasswordStillResetsALocalAccount(t *testing.T) {
	const chosen = "the-break-glass-password"
	gormDB := testutils.SetupTestDB(t)
	repo := repositories.NewRepository(testutils.StormConn(gormDB))
	a := &App{repo: repo, svc: services.NewServiceFacade(repo, impl.NewEventDispatcher(), impl.NewSSEObserver(),
		"reset-password-test-secret", nil, nil, nil)}
	ctx := t.Context()
	hash, err := bcrypt.GenerateFromPassword([]byte("the-forgotten-password"), bcrypt.MinCost)
	if err != nil {
		t.Fatalf("hash: %v", err)
	}
	if err := repo.User().Create(ctx, models.UserModel{Username: "admin", Roles: []string{"ADMIN"}}, string(hash)); err != nil {
		t.Fatalf("create the local administrator: %v", err)
	}
	t.Setenv(envResetPassword, chosen)

	if err := a.handleResetPassword(ctx, "admin"); err != nil {
		t.Fatalf("reset the local administrator's password: %v", err)
	}
	if _, _, err := a.svc.Login(ctx, "admin", chosen); err != nil {
		t.Fatalf("the new password does not sign the administrator in: %v", err)
	}
}
