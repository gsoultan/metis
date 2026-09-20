package auth_test

import (
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/gsoultan/metis/internal/pkg/loginthrottle"
	serviceimpl "github.com/gsoultan/metis/server/domains/services/impl"
	"github.com/gsoultan/metis/server/repositories"
	"github.com/gsoultan/metis/server/repositories/models"
	"github.com/gsoultan/metis/tests/testutils"
	"golang.org/x/crypto/bcrypt"
)

// Repeated guesses against one account must get slower, through the real
// service rather than the throttle in isolation.
//
// The HTTP rate limiter bounds requests per client address, which stops one
// source hammering the server and does nothing about the same account being
// tried from many. bcrypt's cost is a throughput argument; credential stuffing
// is a patience argument. Nothing counted failures per account at all.
//
// Note what this test does NOT pass in: an address. That is the property —
// the penalty follows the account, so changing source does not reset it, and
// changing source is exactly what a stuffing campaign does.
func TestLogin_SlowsRepeatedFailuresForOneAccount(t *testing.T) {
	db := testutils.SetupTestDB(t)
	repo := repositories.NewRepository(testutils.StormConn(db))
	svc := serviceimpl.NewUserService(repo, "test-jwt-secret")
	ctx := t.Context()

	seedUser(t, repo, "alice", "correct-horse-battery")

	// The free attempts, which a person mistyping must not notice.
	for i := range loginthrottle.FreeAttempts {
		_, _, err := svc.Login(ctx, "alice", "wrong")
		if err == nil {
			t.Fatalf("attempt %d with a wrong password succeeded", i+1)
		}
		if isThrottled(err) {
			t.Fatalf("attempt %d was throttled; the first %d are meant to be free: %v",
				i+1, loginthrottle.FreeAttempts, err)
		}
	}

	// Past them, the account is made to wait.
	_, _, err := svc.Login(ctx, "alice", "wrong")
	if err == nil {
		t.Fatal("a wrong password succeeded")
	}
	_, _, err = svc.Login(ctx, "alice", "wrong")
	if !isThrottled(err) {
		t.Errorf("guesses against one account are never slowed: %v", err)
	}
}

// The throttle must not become a way to lock somebody out: an attacker who
// knows a username could otherwise deny its owner access by guessing badly.
// The correct password has to work even while the account is being guessed at.
func TestLogin_ThrottlingIsNotALockout(t *testing.T) {
	db := testutils.SetupTestDB(t)
	repo := repositories.NewRepository(testutils.StormConn(db))
	svc := serviceimpl.NewUserService(repo, "test-jwt-secret")
	ctx := t.Context()

	seedUser(t, repo, "alice", "correct-horse-battery")

	// Only the free attempts, so nothing is waiting yet — the point here is
	// that a correct password clears the count rather than inheriting it.
	for range loginthrottle.FreeAttempts {
		_, _, _ = svc.Login(ctx, "alice", "wrong")
	}

	if _, _, err := svc.Login(ctx, "alice", "correct-horse-battery"); err != nil {
		t.Fatalf("the owner could not sign in after some failed guesses: %v", err)
	}

	// And the count is cleared, so the next mistype starts from the beginning
	// rather than from where the attacker left off.
	if _, _, err := svc.Login(ctx, "alice", "wrong"); isThrottled(err) {
		t.Errorf("a successful sign-in did not clear the failure count: %v", err)
	}
}

// An unrelated account must be unaffected, or one guessed username degrades
// sign-in for everybody.
func TestLogin_ThrottlingIsPerAccount(t *testing.T) {
	db := testutils.SetupTestDB(t)
	repo := repositories.NewRepository(testutils.StormConn(db))
	svc := serviceimpl.NewUserService(repo, "test-jwt-secret")
	ctx := t.Context()

	seedUser(t, repo, "alice", "correct-horse-battery")
	seedUser(t, repo, "grace", "hopper-1906")

	for range loginthrottle.FreeAttempts + 3 {
		_, _, _ = svc.Login(ctx, "alice", "wrong")
	}

	if _, _, err := svc.Login(ctx, "grace", "hopper-1906"); err != nil {
		t.Errorf("guessing at one account stopped another signing in: %v", err)
	}
}

// Failures against a username that does not exist are counted too. Not
// counting them would let an attacker probe names for free and only start
// paying once they found a real one — which is the enumeration the login path
// already goes to some trouble to prevent.
func TestLogin_CountsFailuresForAccountsThatDoNotExist(t *testing.T) {
	db := testutils.SetupTestDB(t)
	repo := repositories.NewRepository(testutils.StormConn(db))
	svc := serviceimpl.NewUserService(repo, "test-jwt-secret")
	ctx := t.Context()

	for range loginthrottle.FreeAttempts + 1 {
		_, _, _ = svc.Login(ctx, "mallory", "wrong")
	}

	_, _, err := svc.Login(ctx, "mallory", "wrong")
	if !isThrottled(err) {
		t.Errorf("guesses at a non-existent username are never slowed, so names can be probed for free: %v", err)
	}
}

func seedUser(t *testing.T, repo repositories.Repository, username, password string) {
	t.Helper()
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.MinCost)
	if err != nil {
		t.Fatalf("hash password: %v", err)
	}
	if err := repo.User().Create(t.Context(), models.UserModel{
		Base:     models.Base{ID: models.UUID(uuid.Must(uuid.NewV7()))},
		Username: username,
		FullName: username,
	}, string(hash)); err != nil {
		t.Fatalf("seed %s: %v", username, err)
	}
}

// isThrottled distinguishes "wrong password" from "slow down". Both are
// authentication failures to a caller, which is deliberate — the difference is
// in the message, so it is read from there.
func isThrottled(err error) bool {
	return err != nil && strings.Contains(err.Error(), "too many failed attempts")
}
