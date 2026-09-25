package impl

import (
	"context"
	"fmt"
	"strings"

	"github.com/google/uuid"
	"github.com/gsoultan/metis/internal/pkg/apierr"
	"github.com/gsoultan/metis/server/domains/entities"
	servicecontracts "github.com/gsoultan/metis/server/domains/services/contracts"
	repocontracts "github.com/gsoultan/metis/server/repositories/contracts"
	"golang.org/x/crypto/bcrypt"
)

type platformUserService struct {
	accounts repocontracts.PlatformUserRepository
}

// NewPlatformUserService returns the service that manages platform accounts.
func NewPlatformUserService(accounts repocontracts.PlatformUserRepository) servicecontracts.PlatformUserService {
	if accounts == nil {
		// Refused loudly at construction rather than at the first call: a nil
		// repository here is a wiring mistake, and discovering it from a
		// request means discovering it in production.
		panic("impl: a platform user service needs an account repository")
	}
	return &platformUserService{accounts: accounts}
}

// minPlatformPasswordLength is the floor for an account that can reconfigure
// the installation. Longer than a participant's, because a participant can
// complete a task and an administrator can repoint the production database.
const minPlatformPasswordLength = 12

func (s *platformUserService) ListPlatformUsers(ctx context.Context) ([]entities.PlatformUser, error) {
	return s.accounts.List(ctx)
}

// CreatePlatformUser adds an administrator.
//
// The password is hashed here and the plaintext never leaves this function: the
// repository takes a hash, so no layer below can log, return or store what was
// typed.
func (s *platformUserService) CreatePlatformUser(ctx context.Context, account entities.PlatformUser, password string) (uuid.UUID, error) {
	account.Username = strings.TrimSpace(account.Username)
	if account.Username == "" {
		return uuid.Nil, apierr.Invalidf("an account needs a username")
	}
	if len(password) < minPlatformPasswordLength {
		return uuid.Nil, apierr.Invalidf("a platform password must be at least %d characters", minPlatformPasswordLength)
	}
	roles, err := s.checkRoles(ctx, account.Roles)
	if err != nil {
		return uuid.Nil, err
	}
	account.Roles = roles

	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return uuid.Nil, fmt.Errorf("could not hash the password: %w", err)
	}
	return s.accounts.Create(ctx, account, string(hash))
}

func (s *platformUserService) UpdatePlatformUser(ctx context.Context, account entities.PlatformUser) error {
	if account.ID == uuid.Nil {
		return apierr.Invalidf("an account is required")
	}
	account.Username = strings.TrimSpace(account.Username)
	if account.Username == "" {
		return apierr.Invalidf("an account needs a username")
	}
	// Roles are not read from this call. Changing what somebody may do is its
	// own act with its own refusal — see SetPlatformRoles — and folding it into
	// "save the profile" would let a form that omits the field revoke silently.
	return s.accounts.Update(ctx, account)
}

func (s *platformUserService) DeletePlatformUser(ctx context.Context, id uuid.UUID) error {
	if id == uuid.Nil {
		return apierr.Invalidf("an account is required")
	}
	return s.accounts.Delete(ctx, id)
}

func (s *platformUserService) SetPlatformRoles(ctx context.Context, id uuid.UUID, roles []string) error {
	if id == uuid.Nil {
		return apierr.Invalidf("an account is required")
	}
	checked, err := s.checkRoles(ctx, roles)
	if err != nil {
		return err
	}
	return s.accounts.SetRoles(ctx, id, checked)
}

func (s *platformUserService) ListPlatformRoles(ctx context.Context) ([]entities.PlatformRole, error) {
	return s.accounts.ListRoles(ctx)
}

// checkRoles normalises and validates a set of role names.
//
// The repository refuses an unknown name too. This is the earlier, better
// message: it says which name, before anything has been written, and it drops
// the duplicates a multi-select can produce.
func (s *platformUserService) checkRoles(ctx context.Context, roles []string) ([]string, error) {
	if len(roles) == 0 {
		return nil, nil
	}
	known, err := s.accounts.ListRoles(ctx)
	if err != nil {
		return nil, err
	}
	byName := make(map[string]struct{}, len(known))
	for _, role := range known {
		byName[role.Name] = struct{}{}
	}

	seen := make(map[string]struct{}, len(roles))
	out := make([]string, 0, len(roles))
	for _, name := range roles {
		name = strings.ToUpper(strings.TrimSpace(name))
		if name == "" {
			continue
		}
		if _, ok := byName[name]; !ok {
			return nil, apierr.Invalidf("%q is not a role", name)
		}
		if _, dup := seen[name]; dup {
			continue
		}
		seen[name] = struct{}{}
		out = append(out, name)
	}
	return out, nil
}
