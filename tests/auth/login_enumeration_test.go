package auth_test

import (
	"strings"
	"testing"

	"github.com/google/uuid"
	serviceimpl "github.com/gsoultan/metis/server/domains/services/impl"
	"github.com/gsoultan/metis/server/repositories"
	"github.com/gsoultan/metis/server/repositories/models"
	"github.com/gsoultan/metis/tests/testutils"
	"golang.org/x/crypto/bcrypt"
)

// A failed login must not reveal whether the account exists.
//
// Login previously returned the repository error verbatim, so a missing account
// answered "could not get user: record not found" while a wrong password
// answered "invalid credentials". That difference is enough to enumerate every
// username on the system by reading the error text — which is the first step of
// a credential-stuffing campaign, since it turns a list of leaked emails into a
// list of accounts known to exist here.
func TestLogin_DoesNotRevealWhetherTheAccountExists(t *testing.T) {
	db := testutils.SetupTestDB(t)
	repo := repositories.NewRepository(db)
	svc := serviceimpl.NewUserService(repo, "test-jwt-secret")
	ctx := t.Context()

	hash, err := bcrypt.GenerateFromPassword([]byte("correct-horse-battery"), bcrypt.DefaultCost)
	if err != nil {
		t.Fatalf("hash password: %v", err)
	}
	if err := repo.User().Create(ctx, models.UserModel{
		Base:     models.Base{ID: models.UUID(uuid.Must(uuid.NewV7()))},
		Username: "alice",
		FullName: "Alice",
	}, string(hash)); err != nil {
		t.Fatalf("seed user: %v", err)
	}

	_, _, wrongPassword := svc.Login(ctx, "alice", "not-the-password")
	_, _, noSuchUser := svc.Login(ctx, "mallory", "not-the-password")

	if wrongPassword == nil || noSuchUser == nil {
		t.Fatal("a bad login succeeded")
	}
	if wrongPassword.Error() != noSuchUser.Error() {
		t.Fatalf("the two failures are distinguishable:\n  wrong password: %v\n  no such user:   %v",
			wrongPassword, noSuchUser)
	}
	// Guard the specific leak: the repository's wording must not reach the caller.
	if strings.Contains(noSuchUser.Error(), "record not found") {
		t.Fatalf("the repository error leaked to the caller: %v", noSuchUser)
	}
}

func TestLogin_SucceedsWithTheCorrectPassword(t *testing.T) {
	db := testutils.SetupTestDB(t)
	repo := repositories.NewRepository(db)
	svc := serviceimpl.NewUserService(repo, "test-jwt-secret")
	ctx := t.Context()

	hash, _ := bcrypt.GenerateFromPassword([]byte("correct-horse-battery"), bcrypt.DefaultCost)
	if err := repo.User().Create(ctx, models.UserModel{
		Base:     models.Base{ID: models.UUID(uuid.Must(uuid.NewV7()))},
		Username: "alice",
		FullName: "Alice",
	}, string(hash)); err != nil {
		t.Fatalf("seed user: %v", err)
	}

	user, token, err := svc.Login(ctx, "alice", "correct-horse-battery")
	if err != nil {
		t.Fatalf("valid credentials were rejected: %v", err)
	}
	if token == "" {
		t.Fatal("no token issued for a valid login")
	}
	if user.Username != "alice" {
		t.Fatalf("got user %q, want alice", user.Username)
	}
}
