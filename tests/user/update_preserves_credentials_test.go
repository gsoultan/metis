package user_test

import (
	"testing"

	"github.com/google/uuid"
	"github.com/gsoultan/metis/server/domains/entities"
	serviceimpl "github.com/gsoultan/metis/server/domains/services/impl"
	"github.com/gsoultan/metis/server/repositories"
	"github.com/gsoultan/metis/server/repositories/models"
	"github.com/gsoultan/metis/tests/testutils"
	"golang.org/x/crypto/bcrypt"
)

// Editing a user must not destroy the account.
//
// UpdateUserRequest carries a whole entities.User and the repository writes it
// with GORM's Save, which sets every column — so any field the caller did not
// send became the zero value. A profile edit does not send a username (it is
// not editable) or a password (it has its own screen), so both were wiped:
//
//	PUT /users/{id} {"user":{"full_name":"","display_name":"Ada","email":"a@b.c"}}
//
//	username      'admin'   -> ''
//	full_name     'Ada …'   -> ''
//	password_hash '$2a$…'   -> ''
//
// The account could then never be logged into again, and if it was the only
// administrator the installation was locked out for good. Observed on a running
// server; "admin/admin no longer works" is the symptom that reaches the user,
// which says nothing about a save having caused it.
func TestUpdateUser_KeepsTheUsernameAndPasswordItWasNotGiven(t *testing.T) {
	repo := repositories.NewRepository(testutils.SetupTestDB(t))
	svc := serviceimpl.NewUserService(repo, "test-jwt-secret")
	ctx := t.Context()

	hash, err := bcrypt.GenerateFromPassword([]byte("correct-horse-battery"), bcrypt.DefaultCost)
	if err != nil {
		t.Fatalf("hash password: %v", err)
	}
	id := uuid.Must(uuid.NewV7())
	if err := repo.User().Create(ctx, models.UserModel{
		Base:     models.Base{ID: models.UUID(id)},
		Username: "admin",
		FullName: "Ada Lovelace",
		Email:    "ada@example.com",
	}, string(hash)); err != nil {
		t.Fatalf("seed user: %v", err)
	}

	// Exactly what the edit form submits: no username, no password.
	if err := svc.UpdateUser(ctx, entities.User{
		ID:          id,
		FullName:    "Ada King",
		DisplayName: "Ada",
		Email:       "ada@example.com",
	}); err != nil {
		t.Fatalf("update user: %v", err)
	}

	// The edit itself must still take effect.
	updated, err := repo.User().Get(ctx, id)
	if err != nil {
		t.Fatalf("reload user: %v", err)
	}
	if updated.FullName != "Ada King" {
		t.Errorf("full name = %q, want \"Ada King\" — the edit did not apply", updated.FullName)
	}
	if updated.Username != "admin" {
		t.Errorf("username = %q, want \"admin\" — a profile edit cleared the login identity", updated.Username)
	}

	// The account must still be usable, which is the part that cannot be undone.
	if _, _, err := svc.Login(ctx, "admin", "correct-horse-battery"); err != nil {
		t.Fatalf("the account can no longer be logged into after an edit: %v", err)
	}
}

// Clearing a name is a legitimate edit and must still work; the rule is that
// fields the request did not carry are left alone, not that nothing changes.
func TestUpdateUser_StillAppliesTheFieldsItWasGiven(t *testing.T) {
	repo := repositories.NewRepository(testutils.SetupTestDB(t))
	svc := serviceimpl.NewUserService(repo, "test-jwt-secret")
	ctx := t.Context()

	hash, _ := bcrypt.GenerateFromPassword([]byte("pw"), bcrypt.DefaultCost)
	id := uuid.Must(uuid.NewV7())
	if err := repo.User().Create(ctx, models.UserModel{
		Base:     models.Base{ID: models.UUID(id)},
		Username: "bob",
		FullName: "Bob Barker",
		Email:    "bob@example.com",
		Roles:    []string{"ADMIN"},
	}, string(hash)); err != nil {
		t.Fatalf("seed user: %v", err)
	}

	if err := svc.UpdateUser(ctx, entities.User{
		ID:       id,
		FullName: "",
		Email:    "robert@example.com",
		Roles:    []string{"USER"},
	}); err != nil {
		t.Fatalf("update user: %v", err)
	}

	updated, _ := repo.User().Get(ctx, id)
	if updated.FullName != "" {
		t.Errorf("full name = %q, want it cleared", updated.FullName)
	}
	if updated.Email != "robert@example.com" {
		t.Errorf("email = %q, want the new address", updated.Email)
	}
	if len(updated.Roles) != 1 || updated.Roles[0] != "USER" {
		t.Errorf("roles = %v, want [USER]", updated.Roles)
	}
}

// Roles omitted entirely is different from roles set to none: a client that
// does not know about roles must not silently strip them.
func TestUpdateUser_LeavesRolesAloneWhenTheyAreNotSupplied(t *testing.T) {
	repo := repositories.NewRepository(testutils.SetupTestDB(t))
	svc := serviceimpl.NewUserService(repo, "test-jwt-secret")
	ctx := t.Context()

	hash, _ := bcrypt.GenerateFromPassword([]byte("pw"), bcrypt.DefaultCost)
	id := uuid.Must(uuid.NewV7())
	if err := repo.User().Create(ctx, models.UserModel{
		Base:     models.Base{ID: models.UUID(id)},
		Username: "carol",
		Roles:    []string{"ADMIN"},
	}, string(hash)); err != nil {
		t.Fatalf("seed user: %v", err)
	}

	if err := svc.UpdateUser(ctx, entities.User{ID: id, FullName: "Carol"}); err != nil {
		t.Fatalf("update user: %v", err)
	}

	updated, _ := repo.User().Get(ctx, id)
	if len(updated.Roles) != 1 || updated.Roles[0] != "ADMIN" {
		t.Errorf("roles = %v, want [ADMIN] — an edit that said nothing about roles removed them", updated.Roles)
	}
}

func TestUpdateUser_RejectsAnUpdateWithNoID(t *testing.T) {
	repo := repositories.NewRepository(testutils.SetupTestDB(t))
	svc := serviceimpl.NewUserService(repo, "test-jwt-secret")

	if err := svc.UpdateUser(t.Context(), entities.User{FullName: "Nobody"}); err == nil {
		t.Fatal("an update with no id was accepted; it has no row to apply to")
	}
}
