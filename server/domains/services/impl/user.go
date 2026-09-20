package impl

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
	"github.com/gsoultan/metis/internal/pkg/apierr"
	"github.com/gsoultan/metis/internal/pkg/auth"
	"github.com/gsoultan/metis/internal/pkg/loginthrottle"
	"github.com/gsoultan/metis/server/domains/adapters"
	"github.com/gsoultan/metis/server/domains/entities"
	"github.com/gsoultan/metis/server/domains/services/contracts"
	"github.com/gsoultan/metis/server/repositories"
	"golang.org/x/crypto/bcrypt"
)

type userService struct {
	repo      repositories.Repository
	jwtSecret []byte
	// principals is a short-lived cache of who a token belongs to. Validating
	// one read the account twice, each read preloading organizations and
	// projects — about six queries before a request reached its handler. See
	// principal_cache.go for why the lifetime is seconds rather than minutes.
	principals *principalCache
	// throttle slows repeated failed sign-ins for one account. The HTTP rate
	// limiter bounds requests per address, which credential stuffing spreads
	// across; this bounds them per account, which it cannot.
	throttle *loginthrottle.Throttle
}

func NewUserService(repo repositories.Repository, jwtSecret string) contracts.UserService {
	return &userService{
		repo:       repo,
		jwtSecret:  []byte(jwtSecret),
		principals: newPrincipalCache(),
		throttle:   loginthrottle.New(),
	}
}

func (s *userService) GetUser(ctx context.Context, id uuid.UUID) (entities.User, error) {
	m, err := s.repo.User().Get(ctx, id)
	if err != nil {
		return entities.User{}, err
	}
	return adapters.UserEntityAdapter{Model: m}.ToEntity(), nil
}

func (s *userService) GetUserByUsername(ctx context.Context, username string) (entities.User, error) {
	m, err := s.repo.User().GetByUsername(ctx, username)
	if err != nil {
		return entities.User{}, err
	}
	return adapters.UserEntityAdapter{Model: m}.ToEntity(), nil
}

func (s *userService) ListUsers(ctx context.Context, organizationID uuid.UUID) ([]entities.User, error) {
	ms, err := s.repo.User().ListByOrganization(ctx, organizationID)
	if err != nil {
		return nil, err
	}
	res := make([]entities.User, len(ms))
	for i, m := range ms {
		res[i] = adapters.UserEntityAdapter{Model: m}.ToEntity()
	}
	return res, nil
}

func (s *userService) CreateUser(ctx context.Context, u entities.User, password string) error {
	// The organizations arrive in the request body, so an administrator of one
	// tenant could otherwise mint an account inside another. Being an admin
	// grants authority over your own organization, not over every organization.
	if err := requireOwnOrganizations(ctx, u.Organizations); err != nil {
		return err
	}

	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return fmt.Errorf("failed to hash password: %w", err)
	}

	if u.ID == uuid.Nil {
		u.ID = uuid.Must(uuid.NewV7())
	}
	if u.CreatedAt.IsZero() {
		u.CreatedAt = time.Now()
	}

	return s.repo.User().Create(ctx, adapters.UserModelAdapter{User: u}.ToModel(), string(hash))
}

// dummyHash is compared against when no user matches, so that a login attempt
// costs the same whether or not the account exists.
//
// bcrypt of "" at the default cost; the value is irrelevant, only the work is.
var dummyHash = []byte("$2a$10$N9qo8uLOickgx2ZMRZoMyeIjZAgcfl7p92ldGxad68LJZdL17lhWy")

func (s *userService) Login(ctx context.Context, username, password string) (entities.User, string, error) {
	// A failed login must not reveal whether the account exists.
	//
	// This previously returned the repository's error verbatim, so a missing
	// account answered "could not get user: record not found" while a wrong
	// password answered "invalid credentials" — enough to enumerate every
	// username on the system by reading the error text.
	//
	// The comparison still runs when no user is found, against a fixed hash, so
	// the two paths also cost roughly the same amount of time. Returning early
	// would leave a timing signal saying the same thing more quietly.
	// Checked before the account is looked up, so a throttled attempt costs the
	// same whether the account exists or not — the same reason the dummy hash
	// below exists.
	if retry, wait := s.throttle.RetryAfter(username, time.Now()); wait {
		return entities.User{}, "", fmt.Errorf("%w: too many failed attempts, try again in %s",
			auth.ErrAuthenticationFailed, retry.Round(time.Second))
	}

	mu, hash, err := s.repo.User().GetWithPasswordByUsername(ctx, username)
	if err != nil {
		// The result is deliberately unused: this compare exists only so that a
		// missing account costs the same time as a wrong password. Checking it
		// would be checking that a fake hash failed to match, which it always
		// does.
		//nolint:errcheck // deliberate: equalises timing, result is meaningless
		bcrypt.CompareHashAndPassword(dummyHash, []byte(password))
		// Counted even though no account matched: not counting would let an
		// attacker probe usernames for free and only pay once they found a
		// real one, which is the enumeration this path already guards against.
		s.throttle.Failed(username, time.Now())
		return entities.User{}, "", fmt.Errorf("%w: invalid credentials", auth.ErrAuthenticationFailed)
	}
	u := adapters.UserEntityAdapter{Model: mu}.ToEntity()

	if err := bcrypt.CompareHashAndPassword([]byte(hash), []byte(password)); err != nil {
		s.throttle.Failed(username, time.Now())
		return entities.User{}, "", fmt.Errorf("%w: invalid credentials", auth.ErrAuthenticationFailed)
	}
	// The correct password ends the sequence, whatever came before it.
	s.throttle.Succeeded(username)

	// Generate JWT token
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, jwt.MapClaims{
		"sub":      u.ID.String(),
		"username": u.Username,
		"roles":    u.Roles,
		"exp":      time.Now().Add(time.Hour * 24).Unix(), // 24 hours
		"iat":      time.Now().Unix(),
	})

	tokenString, err := token.SignedString(s.jwtSecret)
	if err != nil {
		return entities.User{}, "", fmt.Errorf("failed to sign token: %w", err)
	}

	return u, tokenString, nil
}

func (s *userService) ValidateToken(ctx context.Context, tokenString string) (entities.User, error) {
	token, err := jwt.Parse(tokenString, func(token *jwt.Token) (any, error) {
		if _, ok := token.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, fmt.Errorf("unexpected signing method: %v", token.Header["alg"])
		}
		return s.jwtSecret, nil
	})

	if err != nil {
		return entities.User{}, fmt.Errorf("invalid token: %w", err)
	}

	if claims, ok := token.Claims.(jwt.MapClaims); ok && token.Valid {
		sub, ok := claims["sub"].(string)
		if !ok {
			return entities.User{}, fmt.Errorf("invalid token: missing subject")
		}

		userID, err := uuid.Parse(sub)
		if err != nil {
			return entities.User{}, fmt.Errorf("invalid token: invalid user id")
		}

		principal, err := s.resolvePrincipal(ctx, userID)
		if err != nil {
			return entities.User{}, err
		}
		if err := rejectIfIssuedBeforeCredentialsChanged(principal.tokensValidFrom, claims); err != nil {
			return entities.User{}, err
		}
		return principal.user, nil
	}

	return entities.User{}, fmt.Errorf("invalid token")
}

// UpdateUser applies an edit to an existing user without disturbing what the
// edit did not mention.
//
// The request carries a whole entities.User and the repository writes it with
// GORM's Save, which sets every column. Applying that directly meant a profile
// edit — which sends no username, because it is not editable, and no password,
// because that has its own screen — blanked both, along with anything else the
// caller left out. The account was then unreachable: no username to log in
// with and no password hash to check, and if it was the only administrator the
// installation was locked out for good.
//
// So the incoming user is merged onto the stored one. A field that carries a
// value replaces the stored one, including an empty string where clearing is a
// legitimate edit — a name can be removed. The exceptions are the two that
// cannot be recovered from: an empty username is read as "not supplied" rather
// than as a request to remove the login identity, and the password hash is not
// reachable from here at all. Roles distinguish absent from empty, since JSON
// decodes a missing list to nil and an explicit [] to an empty one, so a client
// that knows nothing about roles cannot strip them.
func (s *userService) UpdateUser(ctx context.Context, u entities.User) error {
	if u.ID == uuid.Nil {
		return fmt.Errorf("user id is required")
	}

	stored, err := s.repo.User().Get(ctx, u.ID)
	if err != nil {
		return fmt.Errorf("could not load the user being updated: %w", err)
	}

	stored.FullName = u.FullName
	stored.DisplayName = u.DisplayName
	stored.Email = u.Email
	if u.Username != "" {
		stored.Username = u.Username
	}
	if u.Roles != nil {
		stored.Roles = u.Roles
	}
	if u.Organization != nil {
		stored.Organization = u.Organization.Name
	}

	if err := s.repo.User().Update(ctx, stored); err != nil {
		return err
	}
	// An update can change roles, which is an authorization decision; a cached
	// caller would keep the ones they had.
	s.principals.forget(u.ID)
	return nil
}

// MinPasswordLength is the shortest password SetPassword will store.
//
// Short enough not to be an obstacle, long enough that a reset does not hand
// back something worse than what it replaced.
const MinPasswordLength = 8

// SetPassword replaces an account's password.
//
// There was no way to change one at all: the only path that wrote a hash was
// Create. A forgotten password therefore had no answer for the person who
// forgot it or for an administrator, and since there is no default account by
// design, an installation with one administrator became unreachable for good.
// ChangePassword rotates one account's password after checking the current one.
//
// The current-password check is the whole point, and it is why this does not
// simply call SetPassword: without it, a stolen session token is not a
// temporary compromise but a permanent one, because the attacker can lock the
// owner out of their own account. Session theft should cost the attacker
// access until the token expires, not the account.
//
// The user is identified by ID rather than username: the ID comes from the
// authenticated session, while a username comes from the request body. Taking
// the name from the caller would let any signed-in user change anybody's
// password by naming them.
func (s *userService) ChangePassword(ctx context.Context, userID uuid.UUID, currentPassword, newPassword string) error {
	if len(strings.TrimSpace(newPassword)) < MinPasswordLength {
		return apierr.Invalidf("password must be at least %d characters", MinPasswordLength)
	}
	// Rotating to the same value reads as success but changes nothing, which is
	// exactly wrong after a suspected compromise: the user believes they have
	// locked the attacker out.
	if currentPassword == newPassword {
		return apierr.Invalidf("the new password must be different from the current one")
	}

	mu, hash, err := s.repo.User().GetWithPasswordByID(ctx, userID)
	if err != nil {
		// The ID came from a validated session, so this is a deleted account
		// rather than a guess. Nothing to disclose, and nothing to equalise.
		return fmt.Errorf("%w: invalid credentials", auth.ErrAuthenticationFailed)
	}

	if err := bcrypt.CompareHashAndPassword([]byte(hash), []byte(currentPassword)); err != nil {
		return fmt.Errorf("%w: invalid credentials", auth.ErrAuthenticationFailed)
	}

	newHash, err := bcrypt.GenerateFromPassword([]byte(newPassword), bcrypt.DefaultCost)
	if err != nil {
		return fmt.Errorf("could not hash the new password: %w", err)
	}
	if err := s.repo.User().SetPasswordHash(ctx, uuid.UUID(mu.ID), string(newHash)); err != nil {
		return err
	}
	// Ending the old sessions is the point of the change. A cached credential
	// cutoff would keep honouring them for the life of the entry.
	s.principals.forget(uuid.UUID(mu.ID))
	return nil
}

func (s *userService) SetPassword(ctx context.Context, username, newPassword string) error {
	username = strings.TrimSpace(username)
	if username == "" {
		return fmt.Errorf("a username is required")
	}
	if len(strings.TrimSpace(newPassword)) < MinPasswordLength {
		return fmt.Errorf("password must be at least %d characters", MinPasswordLength)
	}

	user, err := s.repo.User().GetByUsername(ctx, username)
	if err != nil {
		return fmt.Errorf("no such user %q", username)
	}

	hash, err := bcrypt.GenerateFromPassword([]byte(newPassword), bcrypt.DefaultCost)
	if err != nil {
		return fmt.Errorf("could not hash the new password: %w", err)
	}
	if err := s.repo.User().SetPasswordHash(ctx, uuid.UUID(user.ID), string(hash)); err != nil {
		return err
	}
	s.principals.forget(uuid.UUID(user.ID))
	return nil
}

func (s *userService) DeleteUser(ctx context.Context, id uuid.UUID) error {
	if err := s.repo.User().Delete(ctx, id); err != nil {
		return err
	}
	s.principals.forget(id)
	return nil
}

// Membership changes decide which tenant's data a caller can see, so a stale
// entry is an access decision made against a grant that has been withdrawn.
// Each of these drops the account rather than waiting out the entry's lifetime.

func (s *userService) AssignOrganization(ctx context.Context, userID, organizationID uuid.UUID) error {
	if err := s.repo.User().AddOrganization(ctx, userID, organizationID); err != nil {
		return err
	}
	s.principals.forget(userID)
	return nil
}

func (s *userService) UnassignOrganization(ctx context.Context, userID, organizationID uuid.UUID) error {
	if err := s.repo.User().RemoveOrganization(ctx, userID, organizationID); err != nil {
		return err
	}
	s.principals.forget(userID)
	return nil
}

func (s *userService) AssignProject(ctx context.Context, userID, projectID uuid.UUID) error {
	if err := s.repo.User().AddProject(ctx, userID, projectID); err != nil {
		return err
	}
	s.principals.forget(userID)
	return nil
}

func (s *userService) UnassignProject(ctx context.Context, userID, projectID uuid.UUID) error {
	if err := s.repo.User().RemoveProject(ctx, userID, projectID); err != nil {
		return err
	}
	s.principals.forget(userID)
	return nil
}

// rejectIfIssuedBeforeCredentialsChanged refuses a token minted before this
// account's password last changed.
//
// A signature check alone says the token was issued by us, not that it should
// still be honoured. Without this, changing a password stopped the old password
// working and left every token created with it valid for the rest of its
// 24-hour life — so somebody changing their password *because they believe they
// are compromised* achieved nothing against the attacker already holding a
// session. That is the one thing they were trying to do.
//
// Reads the cutoff from the database rather than trusting a claim, because a
// claim is exactly what an attacker holding a token already controls the
// contents of.
// resolvePrincipal reads the account behind a token, through a short-lived
// cache.
//
// One read now serves both the credential cutoff and the caller identity; it
// used to be two, each preloading the same associations.
func (s *userService) resolvePrincipal(ctx context.Context, userID uuid.UUID) (cachedPrincipal, error) {
	if cached, ok := s.principals.get(userID); ok {
		return cached, nil
	}
	mu, _, err := s.repo.User().GetWithPasswordByID(ctx, userID)
	if err != nil {
		return cachedPrincipal{}, fmt.Errorf("invalid token: no such user")
	}
	user := adapters.UserEntityAdapter{Model: mu}.ToEntity()
	s.principals.put(userID, user, mu.TokensValidFrom)
	return cachedPrincipal{user: user, tokensValidFrom: mu.TokensValidFrom}, nil
}

// rejectIfIssuedBeforeCredentialsChanged refuses a token minted before the
// account's credentials last changed. Pure, over what resolvePrincipal read.
func rejectIfIssuedBeforeCredentialsChanged(tokensValidFrom *time.Time, claims jwt.MapClaims) error {
	if tokensValidFrom == nil {
		// An account whose password has not changed since the column was
		// added. Nothing to compare against, and filling it in on upgrade
		// would have signed everybody out.
		return nil
	}

	issued, err := claims.GetIssuedAt()
	if err != nil || issued == nil {
		// Every token this service mints carries iat. One that does not is
		// either older than this field or not ours to reason about, and an
		// account that has had a credential change is not the place to be
		// generous.
		return fmt.Errorf("invalid token: no issued-at to check against the credential change")
	}

	// Strictly before: a token minted in the same second as the change is the
	// one the user is about to be handed, and refusing it would sign them out
	// of the session they just re-authenticated.
	if issued.Unix() < tokensValidFrom.Unix() {
		return fmt.Errorf("invalid token: issued before the password was changed")
	}
	return nil
}
