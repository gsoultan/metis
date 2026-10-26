package userimport_test

import (
	"errors"
	"strings"
	"testing"

	"github.com/gsoultan/metis/internal/pkg/auth"
	"github.com/gsoultan/metis/internal/pkg/peopletoken"
	"github.com/gsoultan/metis/server/domains/services/impl"
	"github.com/gsoultan/metis/server/repositories/pg"
	"golang.org/x/crypto/bcrypt"
)

// TestAPersonSignsInAgainstTheDirectoryAsStored runs the People sign-in on
// the table itself: the hash an administrator set is what is checked, a
// removed person is not found by the soft-delete predicate, and a password
// change ends the sessions issued before it.
func TestAPersonSignsInAgainstTheDirectoryAsStored(t *testing.T) {
	ctx, svc, projectID := fixture(t)
	conn := connOf(t)

	if _, err := svc.ImportWorkflowUsers(ctx, projectID, strings.NewReader("username,email\nada,ada@example.com\nbob,bob@example.com\n")); err != nil {
		t.Fatalf("import: %v", err)
	}
	hash, err := bcrypt.GenerateFromPassword([]byte("correct horse"), bcrypt.MinCost)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := conn.Main().Exec(ctx,
		"UPDATE workflow_users SET password_hash = $1 WHERE project_id = $2 AND username = 'ada'", string(hash), projectID); err != nil {
		t.Fatalf("set the password: %v", err)
	}

	keys, err := peopletoken.NewKeys("people-key-one-7c1f0e9a4b2d8f63e5a0c9b1d7e3f2a4", "")
	if err != nil {
		t.Fatal(err)
	}
	signIn := impl.NewPeopleSignInService(pg.NewPersonSignInRepository(conn), keys)

	if _, _, _, err := signIn.SignInPerson(ctx, projectID, "bob", "correct horse"); !errors.Is(err, auth.ErrAuthenticationFailed) {
		t.Fatalf("bob has no password and signed in: %v", err)
	}
	person, token, _, err := signIn.SignInPerson(ctx, projectID, "ada", "correct horse")
	if err != nil {
		t.Fatalf("ada signs in: %v", err)
	}
	if person.Username != "ada" || person.ProjectID != projectID || person.Email != "ada@example.com" {
		t.Fatalf("signed in as %+v", person)
	}
	if _, err := signIn.AuthenticatePerson(ctx, token); err != nil {
		t.Fatalf("ada's token: %v", err)
	}

	// A password change a minute from now: every token issued before it stops
	// signing her in.
	if _, err := conn.Main().Exec(ctx,
		"UPDATE workflow_users SET tokens_valid_from = now() + interval '1 minute' WHERE id = $1", person.ID); err != nil {
		t.Fatalf("change the credentials: %v", err)
	}
	if _, err := signIn.AuthenticatePerson(ctx, token); !errors.Is(err, auth.ErrUnauthorized) {
		t.Fatalf("a token from before the password change still signs in: %v", err)
	}

	if _, err := conn.Main().Exec(ctx,
		"UPDATE workflow_users SET tokens_valid_from = NULL, deleted_at = now() WHERE id = $1", person.ID); err != nil {
		t.Fatalf("remove ada: %v", err)
	}
	if _, err := signIn.AuthenticatePerson(ctx, token); !errors.Is(err, auth.ErrUnauthorized) {
		t.Fatalf("a removed person's token still signs in: %v", err)
	}
	if _, _, _, err := signIn.SignInPerson(ctx, projectID, "ada", "correct horse"); !errors.Is(err, auth.ErrAuthenticationFailed) {
		t.Fatalf("a removed person signed in: %v", err)
	}
}
