package gorms

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/gsoultan/metis/internal/pkg/crypto"
	"github.com/gsoultan/metis/server/repositories/contracts"
	"github.com/gsoultan/metis/server/repositories/models"
	"gorm.io/gorm"
)

const tableWebhooks = "webhooks"

type gormWebhookRepository struct {
	db *gorm.DB
}

// NewWebhookRepository creates a new GORM-based WebhookRepository.
func NewWebhookRepository(db *gorm.DB) contracts.WebhookRepository {
	return &gormWebhookRepository{db: db}
}

// GetByToken resolves the address in a delivery's URL.
//
// Deliberately unscoped by tenant: this runs before anyone is authenticated,
// because the whole point of a webhook is that the sender has no account here.
// The token is what identifies the webhook, and the row it finds carries the
// project every subsequent action is scoped to — so the tenant is derived from
// the webhook rather than trusted from the request.
func (r *gormWebhookRepository) GetByToken(ctx context.Context, token string) (models.WebhookModel, error) {
	var m models.WebhookModel
	if err := GetTx(ctx, r.db).Where("token = ?", token).First(&m).Error; err != nil {
		return models.WebhookModel{}, fmt.Errorf("could not get webhook by token: %w", err)
	}
	secret, err := crypto.Decrypt(m.Secret)
	if err != nil {
		return models.WebhookModel{}, fmt.Errorf("could not read the webhook secret: %w", err)
	}
	m.Secret = secret
	return m, nil
}

func (r *gormWebhookRepository) ListByProject(ctx context.Context, projectID uuid.UUID) ([]models.WebhookModel, error) {
	var list []models.WebhookModel
	db := tenantScopeDB(ctx, GetTx(ctx, r.db), tableWebhooks)
	if err := db.Where(QualifiedByProjectID(tableWebhooks), projectID).Find(&list).Error; err != nil {
		return nil, fmt.Errorf("could not list webhooks: %w", err)
	}
	// The secret never leaves on a list. Nothing that reads many webhooks needs
	// it, and a decrypted secret in a response body is one logging middleware
	// away from being in a file.
	for i := range list {
		list[i].Secret = ""
	}
	return list, nil
}

func (r *gormWebhookRepository) Create(ctx context.Context, m models.WebhookModel) error {
	if err := requireProjectInTenant(ctx, GetTx(ctx, r.db), uuid.UUID(m.ProjectID)); err != nil {
		return err
	}
	encrypted, err := crypto.Encrypt(m.Secret)
	if err != nil {
		return fmt.Errorf("could not protect the webhook secret: %w", err)
	}
	m.Secret = encrypted
	if err := GetTx(ctx, r.db).Create(&m).Error; err != nil {
		return fmt.Errorf("could not create webhook: %w", err)
	}
	return nil
}

func (r *gormWebhookRepository) SetEnabled(ctx context.Context, id uuid.UUID, enabled bool) error {
	db := GetTx(ctx, r.db)
	if err := requireVisibleToTenant(ctx, db, tableWebhooks, &models.WebhookModel{}, id); err != nil {
		return err
	}
	if err := db.Model(&models.WebhookModel{}).
		Where(QualifiedByID(tableWebhooks), id).
		Update("enabled", enabled).Error; err != nil {
		return fmt.Errorf("could not switch the webhook: %w", err)
	}
	return nil
}

func (r *gormWebhookRepository) Delete(ctx context.Context, id uuid.UUID) error {
	db := GetTx(ctx, r.db)
	if err := requireVisibleToTenant(ctx, db, tableWebhooks, &models.WebhookModel{}, id); err != nil {
		return err
	}
	if err := db.Delete(&models.WebhookModel{}, QualifiedByID(tableWebhooks), id).Error; err != nil {
		return fmt.Errorf("could not delete webhook: %w", err)
	}
	return nil
}

// ClaimDelivery records a delivery, reporting whether it is the first.
//
// The insert is the claim, and the unique index on (webhook_id, delivery_id) is
// what makes it one. A partner that does not get a 2xx in time sends the same
// event again — often several times — and two of those can arrive at once, so a
// check-then-insert would let both conclude they were first and both move the
// process.
func (r *gormWebhookRepository) ClaimDelivery(ctx context.Context, webhookID uuid.UUID, deliveryID string) (bool, error) {
	id, err := uuid.NewV7()
	if err != nil {
		return false, fmt.Errorf("could not generate a delivery id: %w", err)
	}
	record := models.WebhookDeliveryModel{
		Base:       models.Base{ID: models.UUID(id)},
		WebhookID:  models.UUID(webhookID),
		DeliveryID: deliveryID,
		ReceivedAt: time.Now(),
	}
	err = GetTx(ctx, r.db).Create(&record).Error
	if err == nil {
		return true, nil
	}
	if errors.Is(err, gorm.ErrDuplicatedKey) {
		return false, nil
	}
	return false, fmt.Errorf("could not record the delivery: %w", err)
}

// ForgetDeliveriesBefore drops delivery records nothing will ask about again.
//
// These rows exist to answer "have I seen this?" for as long as a sender might
// retry, which is hours at most. Kept forever they are an unbounded table fed by
// remote input.
func (r *gormWebhookRepository) ForgetDeliveriesBefore(ctx context.Context, cutoff time.Time) (int64, error) {
	result := GetTx(ctx, r.db).
		Where("received_at < ?", cutoff).
		Delete(&models.WebhookDeliveryModel{})
	if result.Error != nil {
		return 0, fmt.Errorf("could not forget old deliveries: %w", result.Error)
	}
	return result.RowsAffected, nil
}
