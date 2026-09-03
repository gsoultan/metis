package user_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	pkgauth "github.com/gsoultan/metis/internal/pkg/auth"
	"github.com/gsoultan/metis/server/domains/entities"
	serviceimpl "github.com/gsoultan/metis/server/domains/services/impl"
	"github.com/gsoultan/metis/server/repositories"
	"github.com/gsoultan/metis/tests/testutils"
)

// Changing one's own password had no answer before this: SetPassword exists,
// but it asks for no proof, so it is only safe from a shell on the server.
// That left every password change on a running installation needing operator
// access to the machine — including the administrator's own, which the setup
// wizard sets once and could never rotate.
func TestChangePassword_ReplacesTheOldOne(t *testing.T) {
	repo := repositories.NewRepository(testutils.SetupTestDB(t))
	svc := serviceimpl.NewUserService(repo, "test-jwt-secret")
	ctx := t.Context()
	id := seedUser(t, repo, "alice", "the-old-password")

	if err := svc.ChangePassword(ctx, id, "the-old-password", "a-brand-new-password"); err != nil {
		t.Fatalf("change password: %v", err)
	}

	if _, _, err := svc.Login(ctx, "alice", "a-brand-new-password"); err != nil {
		t.Fatalf("the new password was rejected: %v", err)
	}
	if _, _, err := svc.Login(ctx, "alice", "the-old-password"); err == nil {
		t.Fatal("the old password still works, so it was added rather than replaced")
	}
}

// The current-password check is the reason this is not just SetPassword.
//
// Without it a stolen session token stops being a temporary compromise: the
// attacker changes the password and the real owner is locked out of their own
// account permanently. Session theft should cost access until the token
// expires, not the account.
func TestChangePassword_RefusesWithoutTheCurrentPassword(t *testing.T) {
	repo := repositories.NewRepository(testutils.SetupTestDB(t))
	svc := serviceimpl.NewUserService(repo, "test-jwt-secret")
	ctx := t.Context()
	id := seedUser(t, repo, "alice", "the-old-password")

	err := svc.ChangePassword(ctx, id, "not-the-old-password", "attacker-chosen")
	if err == nil {
		t.Fatal("a wrong current password was accepted: a stolen token is now a stolen account")
	}
	if !errors.Is(err, pkgauth.ErrAuthenticationFailed) {
		t.Errorf("refused with %v, want an authentication failure", err)
	}

	if _, _, err := svc.Login(ctx, "alice", "the-old-password"); err != nil {
		t.Errorf("the real password stopped working after a failed attempt: %v", err)
	}
	if _, _, err := svc.Login(ctx, "alice", "attacker-chosen"); err == nil {
		t.Fatal("the attacker's password works")
	}
}

// Rotating to the same value reports success and changes nothing, which is
// exactly wrong after a suspected compromise: the user believes they have
// locked the attacker out.
func TestChangePassword_RefusesANoOpRotation(t *testing.T) {
	repo := repositories.NewRepository(testutils.SetupTestDB(t))
	svc := serviceimpl.NewUserService(repo, "test-jwt-secret")
	ctx := t.Context()
	id := seedUser(t, repo, "alice", "the-old-password")

	if err := svc.ChangePassword(ctx, id, "the-old-password", "the-old-password"); err == nil {
		t.Fatal("rotating to the same password succeeded, so the user was told they were safe when nothing changed")
	}
}

// A short password is refused on the way in, not stored and discovered later.
func TestChangePassword_EnforcesTheMinimumLength(t *testing.T) {
	repo := repositories.NewRepository(testutils.SetupTestDB(t))
	svc := serviceimpl.NewUserService(repo, "test-jwt-secret")
	ctx := t.Context()
	id := seedUser(t, repo, "alice", "the-old-password")

	if err := svc.ChangePassword(ctx, id, "the-old-password", "short"); err == nil {
		t.Fatal("a password below the minimum length was accepted")
	}
	if _, _, err := svc.Login(ctx, "alice", "the-old-password"); err != nil {
		t.Errorf("the original password stopped working after a rejected change: %v", err)
	}
}

// The account is named by the session, never by the request. This pins the
// extraction that guarantees it.
func TestLocalUserIDFromContext_TakesTheSignedInAccount(t *testing.T) {
	id := uuid.New()
	ctx := context.WithValue(t.Context(), pkgauth.UserContextKey, entities.User{ID: id})

	got, err := serviceimpl.LocalUserIDFromContext(ctx)
	if err != nil {
		t.Fatalf("a signed-in user was not recognised: %v", err)
	}
	if got != id {
		t.Errorf("got %v, want the session's own id %v", got, id)
	}
}

// An OIDC principal has no password here — theirs lives at the identity
// provider, and Metis holds no hash that logging in consults. Rotating one
// would change a value that gates nothing while reporting success, so a user
// who thought they had locked an attacker out would not have.
func TestLocalUserIDFromContext_RefusesAnOIDCPrincipal(t *testing.T) {
	ctx := context.WithValue(t.Context(), pkgauth.UserContextKey, pkgauth.UserClaims{Subject: "sub-123"})

	if _, err := serviceimpl.LocalUserIDFromContext(ctx); err == nil {
		t.Fatal("an OIDC principal was allowed to change a local password that never gates their login")
	}
}

// No session at all is unauthorized, not a nil-UUID account.
func TestLocalUserIDFromContext_RefusesAnAnonymousCaller(t *testing.T) {
	if _, err := serviceimpl.LocalUserIDFromContext(t.Context()); err == nil {
		t.Fatal("an unauthenticated caller was given an identity")
	}
}

// Changing a password has to end the sessions that used it.
//
// It did not. The old password stopped working while every token minted with it
// stayed valid for the rest of its 24-hour life — so somebody changing their
// password *because they believed they were compromised* achieved nothing
// against the attacker already holding a session. That is the one thing they
// were trying to do, and the change reported success while not doing it.
func TestChangePassword_InvalidatesTokensIssuedBeforeIt(t *testing.T) {
	repo := repositories.NewRepository(testutils.SetupTestDB(t))
	svc := serviceimpl.NewUserService(repo, "test-jwt-secret")
	ctx := t.Context()
	id := seedUser(t, repo, "alice", "the-old-password")

	_, stolen, err := svc.Login(ctx, "alice", "the-old-password")
	if err != nil {
		t.Fatalf("login: %v", err)
	}
	if _, err := svc.ValidateToken(ctx, stolen); err != nil {
		t.Fatalf("the token did not work before the change: %v", err)
	}

	// iat has second granularity, so the change must land in a later second
	// than the token for the comparison to mean anything. A test that raced
	// this would pass on a fast machine and fail on a slow one.
	time.Sleep(1100 * time.Millisecond)

	if err := svc.ChangePassword(ctx, id, "the-old-password", "a-brand-new-password"); err != nil {
		t.Fatalf("change password: %v", err)
	}

	if _, err := svc.ValidateToken(ctx, stolen); err == nil {
		t.Fatal("a token issued before the password change still works: changing the password did not end the attacker's session")
	}
}

// The reset an operator runs from the command line has to do it too — that is
// the path used when the account holder cannot get in, which is exactly the
// situation where somebody else can.
func TestSetPassword_InvalidatesTokensIssuedBeforeIt(t *testing.T) {
	repo := repositories.NewRepository(testutils.SetupTestDB(t))
	svc := serviceimpl.NewUserService(repo, "test-jwt-secret")
	ctx := t.Context()
	seedUser(t, repo, "alice", "the-old-password")

	_, stolen, err := svc.Login(ctx, "alice", "the-old-password")
	if err != nil {
		t.Fatalf("login: %v", err)
	}
	time.Sleep(1100 * time.Millisecond)

	if err := svc.SetPassword(ctx, "alice", "an-operator-chosen-password"); err != nil {
		t.Fatalf("set password: %v", err)
	}

	if _, err := svc.ValidateToken(ctx, stolen); err == nil {
		t.Fatal("an administrator reset left the old sessions working")
	}
}

// The account holder is not locked out by their own change: a token issued
// after it is honoured.
func TestChangePassword_LeavesLaterTokensWorking(t *testing.T) {
	repo := repositories.NewRepository(testutils.SetupTestDB(t))
	svc := serviceimpl.NewUserService(repo, "test-jwt-secret")
	ctx := t.Context()
	id := seedUser(t, repo, "alice", "the-old-password")

	if err := svc.ChangePassword(ctx, id, "the-old-password", "a-brand-new-password"); err != nil {
		t.Fatalf("change password: %v", err)
	}

	_, fresh, err := svc.Login(ctx, "alice", "a-brand-new-password")
	if err != nil {
		t.Fatalf("login with the new password: %v", err)
	}
	if _, err := svc.ValidateToken(ctx, fresh); err != nil {
		t.Fatalf("a token issued after the change was refused: %v", err)
	}
}

// An account that has never changed its password since the column was added
// keeps working. Filling the cutoff in for existing rows at migration time
// would have signed out every user on the installation at the moment of an
// upgrade — a self-inflicted outage in the name of a fix.
func TestTokensSurviveWhenTheAccountHasNoRecordedChange(t *testing.T) {
	repo := repositories.NewRepository(testutils.SetupTestDB(t))
	svc := serviceimpl.NewUserService(repo, "test-jwt-secret")
	ctx := t.Context()
	seedUser(t, repo, "alice", "the-old-password")

	_, token, err := svc.Login(ctx, "alice", "the-old-password")
	if err != nil {
		t.Fatalf("login: %v", err)
	}
	if _, err := svc.ValidateToken(ctx, token); err != nil {
		t.Fatalf("a token was refused for an account that has never changed its password: %v", err)
	}
}
