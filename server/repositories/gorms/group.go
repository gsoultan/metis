package gorms

import (
	"context"
	"fmt"

	"github.com/gsoultan/gobpm/server/repositories/contracts"
	"github.com/gsoultan/gobpm/server/repositories/models"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

type gormGroupRepository struct {
	db *gorm.DB
}

// NewGroupRepository creates a new GORM-based GroupRepository.
func NewGroupRepository(db *gorm.DB) contracts.GroupRepository {
	return &gormGroupRepository{db: db}
}

func (r *gormGroupRepository) List(ctx context.Context, organizationID uuid.UUID) ([]models.GroupModel, error) {
	var modelsList []models.GroupModel
	query := GetTx(ctx, r.db)
	if organizationID != uuid.Nil {
		query = query.Where(QueryByOrganizationID, organizationID)
	}
	if err := query.Find(&modelsList).Error; err != nil {
		return nil, fmt.Errorf("could not list groups: %w", err)
	}
	return modelsList, nil
}

func (r *gormGroupRepository) Create(ctx context.Context, g models.GroupModel) error {
	if err := GetTx(ctx, r.db).Create(&g).Error; err != nil {
		return fmt.Errorf("could not create group: %w", err)
	}
	return nil
}

func (r *gormGroupRepository) Get(ctx context.Context, id uuid.UUID) (models.GroupModel, error) {
	var m models.GroupModel
	if err := GetTx(ctx, r.db).First(&m, QueryByID, id).Error; err != nil {
		return models.GroupModel{}, fmt.Errorf("could not get group: %w", err)
	}
	return m, nil
}

func (r *gormGroupRepository) Update(ctx context.Context, g models.GroupModel) error {
	if err := GetTx(ctx, r.db).Save(&g).Error; err != nil {
		return fmt.Errorf("could not update group: %w", err)
	}
	return nil
}

func (r *gormGroupRepository) Delete(ctx context.Context, id uuid.UUID) error {
	if err := GetTx(ctx, r.db).Delete(&models.MembershipModel{}, "group_id = ?", id).Error; err != nil {
		return fmt.Errorf("could not delete group memberships: %w", err)
	}
	if err := GetTx(ctx, r.db).Delete(&models.GroupModel{}, QueryByID, id).Error; err != nil {
		return fmt.Errorf("could not delete group: %w", err)
	}
	return nil
}

func (r *gormGroupRepository) ListGroupMembers(ctx context.Context, groupID uuid.UUID) ([]models.UserModel, error) {
	var userModels []models.UserModel
	err := GetTx(ctx, r.db).
		Joins("JOIN memberships ON memberships.user_id = users.id").
		Where("memberships.group_id = ?", groupID).
		Find(&userModels).Error
	if err != nil {
		return nil, fmt.Errorf("could not list group members: %w", err)
	}
	return userModels, nil
}

func (r *gormGroupRepository) AddMembership(ctx context.Context, userID, groupID uuid.UUID) error {
	m := models.MembershipModel{
		UserID:  models.UUID(userID),
		GroupID: models.UUID(groupID),
	}
	if err := GetTx(ctx, r.db).Create(&m).Error; err != nil {
		return fmt.Errorf("could not add membership: %w", err)
	}
	return nil
}

func (r *gormGroupRepository) RemoveMembership(ctx context.Context, userID, groupID uuid.UUID) error {
	if err := GetTx(ctx, r.db).Delete(&models.MembershipModel{}, "user_id = ? AND group_id = ?", userID, groupID).Error; err != nil {
		return fmt.Errorf("could not remove membership: %w", err)
	}
	return nil
}

func (r *gormGroupRepository) ListUserGroups(ctx context.Context, userID uuid.UUID) ([]models.GroupModel, error) {
	var groupModels []models.GroupModel
	err := GetTx(ctx, r.db).
		Joins("JOIN memberships ON memberships.group_id = groups.id").
		Where("memberships.user_id = ?", userID).
		Find(&groupModels).Error
	if err != nil {
		return nil, fmt.Errorf("could not list user groups: %w", err)
	}
	return groupModels, nil
}
