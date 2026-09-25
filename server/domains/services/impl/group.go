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

// AddMembership puts an account in a group of the organization it belongs to.
//
// It went straight to the repository, which checks that the group is the
// caller's and never looks at the account: accounts are installation-wide
// there, and have to be (see requireAccountVisible). So an administrator of
// one organization could put another organization's account into one of
// their groups, and then read its name and email back from the member list.
func (s *groupService) AddMembership(ctx context.Context, userID, groupID uuid.UUID) error {
	if err := s.requireMemberOfGroupsOrganization(ctx, userID, groupID); err != nil {
		return err
	}
	return s.repo.Group().AddMembership(ctx, userID, groupID)
}

// RemoveMembership takes an account out of one of the caller's groups.
//
// The group has to be the caller's, which the repository checks. The account
// does not: removing somebody places them nowhere, and a membership that
// already crosses organizations — added before AddMembership refused one — is
// a row in the caller's own group that this is the only way to be rid of.
func (s *groupService) RemoveMembership(ctx context.Context, userID, groupID uuid.UUID) error {
	return s.repo.Group().RemoveMembership(ctx, userID, groupID)
}

// requireMemberOfGroupsOrganization refuses an account that does not belong to
// the group's organization.
//
// The group is read through the tenant scope, so another organization's group
// is not found. The account is then held to the group's organization rather
// than to the caller's, which is the same one on a request and still the rule
// for work that carries no tenant. Both refusals read as not found: to this
// caller, neither exists.
func (s *groupService) requireMemberOfGroupsOrganization(ctx context.Context, userID, groupID uuid.UUID) error {
	group, err := s.repo.Group().Get(ctx, groupID)
	if err != nil {
		return err
	}
	account, err := s.repo.User().Get(ctx, userID)
	if err != nil {
		return err
	}
	if !belongsTo(account, uuid.UUID(group.OrganizationID)) {
		return errNoSuchAccount
	}
	return nil
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
