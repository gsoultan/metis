package impl

import (
	"context"
	"fmt"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
	"github.com/gsoultan/gobpm/internal/pkg/auth"
	"github.com/gsoultan/gobpm/server/domains/adapters"
	"github.com/gsoultan/gobpm/server/domains/entities"
	"github.com/gsoultan/gobpm/server/domains/services/contracts"
	"github.com/gsoultan/gobpm/server/repositories"
	"golang.org/x/crypto/bcrypt"
)

type userService struct {
	repo      repositories.Repository
	jwtSecret []byte
}

func NewUserService(repo repositories.Repository, jwtSecret string) contracts.UserService {
	return &userService{
		repo:      repo,
		jwtSecret: []byte(jwtSecret),
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
	mu, hash, err := s.repo.User().GetWithPasswordByUsername(ctx, username)
	if err != nil {
		_ = bcrypt.CompareHashAndPassword(dummyHash, []byte(password))
		return entities.User{}, "", fmt.Errorf("%w: invalid credentials", auth.ErrAuthenticationFailed)
	}
	u := adapters.UserEntityAdapter{Model: mu}.ToEntity()

	if err := bcrypt.CompareHashAndPassword([]byte(hash), []byte(password)); err != nil {
		return entities.User{}, "", fmt.Errorf("%w: invalid credentials", auth.ErrAuthenticationFailed)
	}

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

		return s.GetUser(ctx, userID)
	}

	return entities.User{}, fmt.Errorf("invalid token")
}

func (s *userService) UpdateUser(ctx context.Context, u entities.User) error {
	return s.repo.User().Update(ctx, adapters.UserModelAdapter{User: u}.ToModel())
}

func (s *userService) DeleteUser(ctx context.Context, id uuid.UUID) error {
	return s.repo.User().Delete(ctx, id)
}

func (s *userService) AssignOrganization(ctx context.Context, userID, organizationID uuid.UUID) error {
	return s.repo.User().AddOrganization(ctx, userID, organizationID)
}

func (s *userService) UnassignOrganization(ctx context.Context, userID, organizationID uuid.UUID) error {
	return s.repo.User().RemoveOrganization(ctx, userID, organizationID)
}

func (s *userService) AssignProject(ctx context.Context, userID, projectID uuid.UUID) error {
	return s.repo.User().AddProject(ctx, userID, projectID)
}

func (s *userService) UnassignProject(ctx context.Context, userID, projectID uuid.UUID) error {
	return s.repo.User().RemoveProject(ctx, userID, projectID)
}
