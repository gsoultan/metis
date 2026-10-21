package migrations

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/gsoultan/metis/internal/pkg/crypto"
	"gorm.io/gorm"
)

// WebhookSecretsMigration is the version under which the inbound webhooks'
// secrets were sealed. 40 and 41 are left to the branches that claimed 40
// while this one was open.
const WebhookSecretsMigration = 42

// webhookSecrets is migration 42.
//
// A webhook's secret is what signs a delivery, so whoever holds it can start
// and move processes as the partner. The model said it was encrypted at rest
// and the repository wrote it as it was: a backup, a read replica or a
// `SELECT` was enough to forge any partner's deliveries. The repository seals
// it now, and this seals the rows written before it did, deleted ones too —
// a deleted webhook's secret is still the partner's.
//
// Transactional: every secret is sealed or none is. Only a secret that is not
// already sealed is touched, and each update is conditional on the value it
// read, so a repeated run changes nothing.
func webhookSecrets() Migration {
	return Migration{
		Version:       WebhookSecretsMigration,
		Name:          "inbound webhook secrets are sealed at rest",
		Transactional: true,
		Run:           sealWebhookSecrets,
	}
}

// plainWebhookSecret is a webhook whose secret is still in the clear.
type plainWebhookSecret struct {
	ID     uuid.UUID
	Secret string
}

func sealWebhookSecrets(ctx context.Context, db *gorm.DB) error {
	if !db.WithContext(ctx).Migrator().HasTable("webhooks") {
		return nil
	}
	var plain []plainWebhookSecret
	if err := db.WithContext(ctx).Raw(
		`SELECT id, secret FROM webhooks WHERE secret <> '' AND secret NOT LIKE 'gcm1:%'`).
		Scan(&plain).Error; err != nil {
		return fmt.Errorf("find the webhook secrets that are not sealed: %w", err)
	}
	for _, hook := range plain {
		if err := sealWebhookSecret(ctx, db, hook); err != nil {
			return err
		}
	}
	return nil
}

func sealWebhookSecret(ctx context.Context, db *gorm.DB, hook plainWebhookSecret) error {
	sealed, err := crypto.Encrypt(hook.Secret)
	if errors.Is(err, crypto.ErrKeyNotConfigured) {
		return fmt.Errorf("webhook secrets have to be sealed and no ENCRYPTION_KEY is configured: %w", err)
	}
	if err != nil {
		return fmt.Errorf("seal the secret of webhook %s: %w", hook.ID, err)
	}
	if err := db.WithContext(ctx).Exec(
		`UPDATE webhooks SET secret = ? WHERE id = ? AND secret = ?`,
		sealed, hook.ID, hook.Secret).Error; err != nil {
		return fmt.Errorf("seal the secret of webhook %s: %w", hook.ID, err)
	}
	return nil
}
