package user_test

import (
	"strings"
	"testing"

	"github.com/google/uuid"
	serviceimpl "github.com/gsoultan/gobpm/server/domains/services/impl"
	"github.com/gsoultan/gobpm/server/repositories"
	"github.com/gsoultan/gobpm/server/repositories/models"
	"github.com/gsoultan/gobpm/tests/testutils"
	"golang.org/x/crypto/bcrypt"
)

// A password must be changeable.
//
// The only way to set one was Create, so a forgotten password had no answer:
// not for the person who forgot it, and not for an administrator either. On an
// installation with a single administrator — the shape the setup wizard
// produces — that made the whole thing unreachable, permanently. There is no
// default account to fall back to, by design.
func TestSetPassword_ReplacesTheOldOne(t *testing.T) {
	repo := repositories.NewRepository(testutils.SetupTestDB(t))
	svc := serviceimpl.NewUserService(repo, "test-jwt-secret")
	ctx := t.Context()

	seedUser(t, repo, "alice", "the-old-password")

	if err := svc.SetPassword(ctx, "alice", "a-brand-new-password"); err != nil {
		t.Fatalf("set password: %v", err)
	}

	if _, _, err := svc.Login(ctx, "alice", "a-brand-new-password"); err != nil {
		t.Fatalf("the new password was rejected: %v", err)
	}
	if _, _, err := svc.Login(ctx, "alice", "the-old-password"); err == nil {
		t.Fatal("the old password still works, so it was added rather than replaced")
	}
}

// Everything else about the account stays as it was: a reset is not an edit.
func TestSetPassword_LeavesTheRestOfTheAccountAlone(t *testing.T) {
	repo := repositories.NewRepository(testutils.SetupTestDB(t))
	svc := serviceimpl.NewUserService(repo, "test-jwt-secret")
	ctx := t.Context()

	id := seedUser(t, repo, "alice", "old")

	if err := svc.SetPassword(ctx, "alice", "a-brand-new-password"); err != nil {
		t.Fatalf("set password: %v", err)
	}

	got, err := repo.User().Get(ctx, id)
	if err != nil {
		t.Fatalf("reload: %v", err)
	}
	if got.Username != "alice" || got.FullName != "Alice Example" || got.Email != "alice@example.com" {
		t.Errorf("the account changed: %+v", got)
	}
	if len(got.Roles) != 1 || got.Roles[0] != "ADMIN" {
		t.Errorf("roles = %v, want [ADMIN]", got.Roles)
	}
}

func TestSetPassword_RejectsAPasswordTooShortToBeWorthHaving(t *testing.T) {
	repo := repositories.NewRepository(testutils.SetupTestDB(t))
	svc := serviceimpl.NewUserService(repo, "test-jwt-secret")
	ctx := t.Context()

	seedUser(t, repo, "alice", "the-old-password")

	for _, pw := range []string{"", "   ", "short"} {
		if err := svc.SetPassword(ctx, "alice", pw); err == nil {
			t.Errorf("password %q was accepted", pw)
		}
	}

	// And the old one still works, so a rejected attempt has not half-applied.
	if _, _, err := svc.Login(ctx, "alice", "the-old-password"); err != nil {
		t.Fatalf("a rejected reset disturbed the existing password: %v", err)
	}
}

func TestSetPassword_ReportsAnUnknownUser(t *testing.T) {
	repo := repositories.NewRepository(testutils.SetupTestDB(t))
	svc := serviceimpl.NewUserService(repo, "test-jwt-secret")

	err := svc.SetPassword(t.Context(), "nobody", "a-brand-new-password")
	if err == nil {
		t.Fatal("setting a password for an unknown user reported success")
	}
	if !strings.Contains(err.Error(), "nobody") {
		t.Errorf("the error does not name the account asked for: %v", err)
	}
}

func seedUser(t *testing.T, repo repositories.Repository, username, password string) uuid.UUID {
	t.Helper()
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		t.Fatalf("hash: %v", err)
	}
	id := uuid.Must(uuid.NewV7())
	if err := repo.User().Create(t.Context(), models.UserModel{
		Base:     models.Base{ID: id},
		Username: username,
		FullName: "Alice Example",
		Email:    "alice@example.com",
		Roles:    []string{"ADMIN"},
	}, string(hash)); err != nil {
		t.Fatalf("seed user: %v", err)
	}
	return id
}
