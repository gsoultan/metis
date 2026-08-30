package impl

import (
	"context"
	"time"

	"github.com/google/uuid"
	"github.com/gsoultan/metis/server/domains/adapters"
	"github.com/gsoultan/metis/server/domains/entities"
	"github.com/gsoultan/metis/server/domains/services/contracts"
	"github.com/gsoultan/metis/server/repositories"
)

type groupService struct {
	repo repositories.Repository
}

// NewGroupService creates a new GroupService.
func NewGroupService(repo repositories.Repository) contracts.GroupService {
	return &groupService{
		repo: repo,
	}
}

func (s *groupService) ListGroups(ctx context.Context, organizationID uuid.UUID) ([]entities.Group, error) {
	ms, err := s.repo.Group().List(ctx, organizationID)
	if err != nil {
		return nil, err
	}
	res := make([]entities.Group, len(ms))
	for i, m := range ms {
		res[i] = adapters.GroupEntityAdapter{Model: m}.ToEntity()
	}
	return res, nil
}

func (s *groupService) CreateGroup(ctx context.Context, g entities.Group) error {
	if g.ID == uuid.Nil {
		g.ID = uuid.Must(uuid.NewV7())
	}
	if g.CreatedAt.IsZero() {
		g.CreatedAt = time.Now()
	}
	return s.repo.Group().Create(ctx, adapters.GroupModelAdapter{Group: g}.ToModel())
}

func (s *groupService) GetGroup(ctx context.Context, id uuid.UUID) (entities.Group, error) {
	m, err := s.repo.Group().Get(ctx, id)
	if err != nil {
		return entities.Group{}, err
	}
	return adapters.GroupEntityAdapter{Model: m}.ToEntity(), nil
}

func (s *groupService) UpdateGroup(ctx context.Context, g entities.Group) error {
	return s.repo.Group().Update(ctx, adapters.GroupModelAdapter{Group: g}.ToModel())
}

func (s *groupService) DeleteGroup(ctx context.Context, id uuid.UUID) error {
	return s.repo.Group().Delete(ctx, id)
}

func (s *groupService) ListGroupMembers(ctx context.Context, groupID uuid.UUID) ([]entities.User, error) {
	ms, err := s.repo.Group().ListGroupMembers(ctx, groupID)
	if err != nil {
		return nil, err
	}
	res := make([]entities.User, len(ms))
	for i, m := range ms {
		res[i] = adapters.UserEntityAdapter{Model: m}.ToEntity()
	}
	return res, nil
}

func (s *groupService) AddMembership(ctx context.Context, userID, groupID uuid.UUID) error {
	return s.repo.Group().AddMembership(ctx, userID, groupID)
}

func (s *groupService) RemoveMembership(ctx context.Context, userID, groupID uuid.UUID) error {
	return s.repo.Group().RemoveMembership(ctx, userID, groupID)
}

func (s *groupService) ListUserGroups(ctx context.Context, userID uuid.UUID) ([]entities.Group, error) {
	ms, err := s.repo.Group().ListUserGroups(ctx, userID)
	if err != nil {
		return nil, err
	}
	res := make([]entities.Group, len(ms))
	for i, m := range ms {
		res[i] = adapters.GroupEntityAdapter{Model: m}.ToEntity()
	}
	return res, nil
}
