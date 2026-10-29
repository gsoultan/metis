package impl

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/gsoultan/metis/internal/pkg/apierr"
	"github.com/gsoultan/metis/server/domains/adapters"
	"github.com/gsoultan/metis/server/domains/entities"
	servicecontracts "github.com/gsoultan/metis/server/domains/services/contracts"
	"github.com/gsoultan/metis/server/repositories"
)

type organizationService struct {
	repo   repositories.Repository
	forget func(uuid.UUID)
}

// NewOrganizationService creates a new OrganizationService implementation.
// forget drops an account from the cache of signed-in callers, so that one
// who has just joined an organization can reach it at once; nil forgets
// nothing.
func NewOrganizationService(repo repositories.Repository, forget func(uuid.UUID)) servicecontracts.OrganizationService {
	if forget == nil {
		forget = func(uuid.UUID) {}
	}
	return &organizationService{
		repo:   repo,
		forget: forget,
	}
}

// CreateOrganization adds an organization and makes whoever created it its
// administrator, in the same transaction.
//
// Without that nobody was in it: the creator could not add anybody to it,
// because adding a member is refused for an organization that is not the
// caller's, and it was not in their list of organizations either. A call with
// nobody signed in — a test, or the composition root — creates it empty.
func (s *organizationService) CreateOrganization(ctx context.Context, name, description string) (entities.Organization, error) {
	if name == "" {
		return entities.Organization{}, apierr.Invalidf("organization name is required")
	}
	idObj, err := uuid.NewV7()
	if err != nil {
		return entities.Organization{}, fmt.Errorf("could not generate UUID: %w", err)
	}
	o := entities.Organization{
		ID:          idObj,
		Name:        name,
		Description: description,
		CreatedAt:   time.Now(),
		UpdatedAt:   time.Now(),
	}
	creator := signedIn(ctx)
	err = s.repo.UnitOfWork().Do(ctx, func(txCtx context.Context) error {
		if err := s.repo.Organization().Create(txCtx, adapters.OrganizationModelAdapter{Organization: o}.ToModel()); err != nil {
			return err
		}
		if creator == nil {
			return nil
		}
		return s.repo.User().AddOrganizationWithRoles(txCtx, creator.ID, o.ID, []string{entities.RoleAdmin})
	})
	if err != nil {
		return entities.Organization{}, err
	}
	if creator != nil {
		s.forget(creator.ID)
	}
	return o, nil
}

func (s *organizationService) GetOrganization(ctx context.Context, id uuid.UUID) (entities.Organization, error) {
	m, err := s.repo.Organization().Get(ctx, id)
	if err != nil {
		return entities.Organization{}, err
	}
	return adapters.OrganizationEntityAdapter{Model: m}.ToEntity(), nil
}

func (s *organizationService) ListOrganizations(ctx context.Context) ([]entities.Organization, error) {
	ms, err := s.repo.Organization().List(ctx)
	if err != nil {
		return nil, err
	}
	res := make([]entities.Organization, len(ms))
	for i, m := range ms {
		res[i] = adapters.OrganizationEntityAdapter{Model: m}.ToEntity()
	}
	return res, nil
}

func (s *organizationService) UpdateOrganization(ctx context.Context, id uuid.UUID, name, description string) error {
	return s.repo.UnitOfWork().Do(ctx, func(txCtx context.Context) error {
		m, err := s.repo.Organization().Get(txCtx, id)
		if err != nil {
			return err
		}
		if name != "" {
			m.Name = name
		}
		m.Description = description
		m.UpdatedAt = time.Now()
		return s.repo.Organization().Update(txCtx, m)
	})
}

// DeleteOrganization deletes an organization nobody else is in and no project
// is in.
//
// Deleting one only hid it: its members went on signing in to it and its
// projects went on running, on one administrator's click. So it is refused,
// naming what is still there, until those are gone — the projects deleted and
// everybody but the caller taken out — and the caller leaves it with it.
func (s *organizationService) DeleteOrganization(ctx context.Context, id uuid.UUID) error {
	if id == uuid.Nil {
		return apierr.Invalidf("an organization is required")
	}
	caller := signedIn(ctx)
	err := s.repo.UnitOfWork().Do(ctx, func(txCtx context.Context) error {
		members, err := s.repo.User().ListByOrganization(txCtx, id)
		if err != nil {
			return err
		}
		others := 0
		for _, m := range members {
			if caller == nil || uuid.UUID(m.ID) != caller.ID {
				others++
			}
		}
		projects, err := s.repo.Project().ListByOrganization(txCtx, id)
		if err != nil {
			return err
		}
		if still := stillIn(others, len(projects)); still != "" {
			return apierr.Invalidf("this organization still has %s; remove them before deleting it", still)
		}
		if err := s.repo.Organization().Delete(txCtx, id); err != nil {
			return err
		}
		if caller == nil {
			return nil
		}
		return s.repo.User().RemoveOrganization(txCtx, caller.ID, id)
	})
	if err != nil {
		return err
	}
	if caller != nil {
		s.forget(caller.ID)
	}
	return nil
}

// stillIn names what an organization still holds, or nothing.
func stillIn(members, projects int) string {
	var parts []string
	if members > 0 {
		parts = append(parts, howMany(members, "other member", "other members"))
	}
	if projects > 0 {
		parts = append(parts, howMany(projects, "project", "projects"))
	}
	return strings.Join(parts, " and ")
}

// howMany is n of something, in words a sentence can use.
func howMany(n int, one, many string) string {
	if n == 1 {
		return "1 " + one
	}
	return fmt.Sprintf("%d %s", n, many)
}

func (s *organizationService) CountOrganizations(ctx context.Context) (int64, error) {
	return s.repo.Organization().Count(ctx)
}
