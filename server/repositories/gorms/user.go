package gorms

import (
	"context"
	"fmt"
	"time"

	"github.com/gsoultan/metis/server/repositories/contracts"
	"github.com/gsoultan/metis/server/repositories/models"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

type gormUserRepository struct {
	db *gorm.DB
}

// NewUserRepository creates a new GORM-based UserRepository.
func NewUserRepository(db *gorm.DB) contracts.UserRepository {
	return &gormUserRepository{db: db}
}

func (r *gormUserRepository) Get(ctx context.Context, id uuid.UUID) (models.UserModel, error) {
	var m models.UserModel
	if err := GetTx(ctx, r.db).Preload("Organizations").Preload("Projects").First(&m, QueryByID, id).Error; err != nil {
		return models.UserModel{}, lookupError(err, "user")
	}
	return m, nil
}

func (r *gormUserRepository) GetByUsername(ctx context.Context, username string) (models.UserModel, error) {
	var m models.UserModel
	if err := GetTx(ctx, r.db).Preload("Organizations").Preload("Projects").Where(QueryByUsername, username).First(&m).Error; err != nil {
		return models.UserModel{}, lookupError(err, "user")
	}
	return m, nil
}

func (r *gormUserRepository) GetWithPasswordByUsername(ctx context.Context, username string) (models.UserModel, string, error) {
	var m models.UserModel
	if err := GetTx(ctx, r.db).Preload("Organizations").Preload("Projects").Where(QueryByUsername, username).First(&m).Error; err != nil {
		return models.UserModel{}, "", lookupError(err, "user")
	}
	return m, m.PasswordHash, nil
}

func (r *gormUserRepository) GetWithPasswordByID(ctx context.Context, id uuid.UUID) (models.UserModel, string, error) {
	var m models.UserModel
	if err := GetTx(ctx, r.db).Preload("Organizations").Preload("Projects").First(&m, QueryByID, id).Error; err != nil {
		return models.UserModel{}, "", lookupError(err, "user")
	}
	return m, m.PasswordHash, nil
}

func (r *gormUserRepository) ListByOrganization(ctx context.Context, organizationID uuid.UUID) ([]models.UserModel, error) {
	var modelsList []models.UserModel
	query := GetTx(ctx, r.db).Preload("Organizations").Preload("Projects")
	if organizationID != uuid.Nil {
		query = query.Joins("JOIN user_organizations ON user_organizations.user_model_id = users.id").
			Where("user_organizations.organization_model_id = ?", organizationID)
	}
	if err := query.Find(&modelsList).Error; err != nil {
		return nil, fmt.Errorf("could not list users: %w", err)
	}
	return modelsList, nil
}

func (r *gormUserRepository) Create(ctx context.Context, u models.UserModel, passwordHash string) error {
	u.PasswordHash = passwordHash
	if err := GetTx(ctx, r.db).Create(&u).Error; err != nil {
		return fmt.Errorf("could not create user: %w", err)
	}
	return nil
}

func (r *gormUserRepository) Update(ctx context.Context, u models.UserModel) error {
	if err := GetTx(ctx, r.db).Save(&u).Error; err != nil {
		return fmt.Errorf("could not update user: %w", err)
	}
	return nil
}

// SetPasswordHash updates just the one column.
func (r *gormUserRepository) SetPasswordHash(ctx context.Context, id uuid.UUID, passwordHash string) error {
	// The cutoff moves in the same statement as the hash. Two statements would
	// leave a window where the password had changed and the old tokens were
	// still good, which is the whole bug.
	result := GetTx(ctx, r.db).Model(&models.UserModel{}).
		Where(QueryByID, id).
		Updates(map[string]any{
			"password_hash":     passwordHash,
			"tokens_valid_from": time.Now().UTC(),
		})
	if result.Error != nil {
		return fmt.Errorf("could not set password: %w", result.Error)
	}
	if result.RowsAffected == 0 {
		return fmt.Errorf("could not set password: no such user")
	}
	return nil
}

func (r *gormUserRepository) Delete(ctx context.Context, id uuid.UUID) error {
	if err := GetTx(ctx, r.db).Delete(&models.UserModel{}, QueryByID, id).Error; err != nil {
		return fmt.Errorf("could not delete user: %w", err)
	}
	return nil
}

func (r *gormUserRepository) AddOrganization(ctx context.Context, userID, organizationID uuid.UUID) error {
	user := models.UserModel{Base: models.Base{ID: models.UUID(userID)}}
	org := models.OrganizationModel{Base: models.Base{ID: models.UUID(organizationID)}}
	if err := GetTx(ctx, r.db).Model(&user).Association("Organizations").Append(&org); err != nil {
		return fmt.Errorf("could not add organization to user: %w", err)
	}
	return nil
}

func (r *gormUserRepository) RemoveOrganization(ctx context.Context, userID, organizationID uuid.UUID) error {
	user := models.UserModel{Base: models.Base{ID: models.UUID(userID)}}
	org := models.OrganizationModel{Base: models.Base{ID: models.UUID(organizationID)}}
	if err := GetTx(ctx, r.db).Model(&user).Association("Organizations").Delete(&org); err != nil {
		return fmt.Errorf("could not remove organization from user: %w", err)
	}
	return nil
}

func (r *gormUserRepository) AddProject(ctx context.Context, userID, projectID uuid.UUID) error {
	user := models.UserModel{Base: models.Base{ID: models.UUID(userID)}}
	project := models.ProjectModel{Base: models.Base{ID: models.UUID(projectID)}}
	if err := GetTx(ctx, r.db).Model(&user).Association("Projects").Append(&project); err != nil {
		return fmt.Errorf("could not add project to user: %w", err)
	}
	return nil
}

func (r *gormUserRepository) RemoveProject(ctx context.Context, userID, projectID uuid.UUID) error {
	user := models.UserModel{Base: models.Base{ID: models.UUID(userID)}}
	project := models.ProjectModel{Base: models.Base{ID: models.UUID(projectID)}}
	if err := GetTx(ctx, r.db).Model(&user).Association("Projects").Delete(&project); err != nil {
		return fmt.Errorf("could not remove project from user: %w", err)
	}
	return nil
}
