package user_test

import (
	"context"
	"errors"
	"testing"

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
