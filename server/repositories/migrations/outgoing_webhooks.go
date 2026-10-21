package migrations

import (
	"context"
	"fmt"

	"gorm.io/gorm"
)

// OutgoingWebhooksMigration is the version under which a project could name
// addresses to be told about its events. 39 is the business key's.
const OutgoingWebhooksMigration = 40

// outgoingWebhooksDDL is frozen text: the tables migration 40 made, which must
// not change when the model does. tests/migrations compares it with the storm
// model and the model is the arbiter.
//
// The webhooks reference their project, so a project with webhooks cannot be
// deleted out from under them; the deliveries reference their webhook, which
// is soft-deleted, so the log of one that was removed still reads. No
// reference to process instances, by the ledger's rule (instanceDeviationsDDL):
// writing a delivery happens inside a step, and must not wait on a row the
// engine holds. The indexes are plain builds: the tables have just been
// created and are read by nothing yet.
var outgoingWebhooksDDL = []string{
	`CREATE TABLE IF NOT EXISTS outgoing_webhooks (
    id uuid DEFAULT gen_random_uuid() NOT NULL,
    created_at timestamptz DEFAULT now() NOT NULL,
    updated_at timestamptz DEFAULT now() NOT NULL,
    project_id uuid NOT NULL,
    name varchar(255) NOT NULL,
    url varchar(2048) NOT NULL,
    events jsonb DEFAULT '[]'::jsonb NOT NULL,
    secret varchar(512) NOT NULL,
    enabled bool NOT NULL,
    deleted_at timestamptz,
    PRIMARY KEY (id),
    CONSTRAINT fk_outgoing_webhooks_project_id FOREIGN KEY (project_id) REFERENCES projects (id)
)`,
	`CREATE INDEX IF NOT EXISTS ix_outgoing_webhooks_deleted_at ON outgoing_webhooks (deleted_at)`,
	`CREATE INDEX IF NOT EXISTS ix_outgoing_webhooks_project_live ON outgoing_webhooks (project_id) WHERE (enabled AND (deleted_at IS NULL))`,
	`CREATE TABLE IF NOT EXISTS outgoing_webhook_deliveries (
    id uuid DEFAULT gen_random_uuid() NOT NULL,
    created_at timestamptz DEFAULT now() NOT NULL,
    updated_at timestamptz DEFAULT now() NOT NULL,
    webhook_id uuid NOT NULL,
    event varchar(64) NOT NULL,
    body text NOT NULL,
    status varchar(16) NOT NULL,
    attempts int8 NOT NULL,
    next_attempt_at timestamptz NOT NULL,
    last_attempt_at timestamptz,
    last_status_code int8,
    last_error varchar(1024),
    last_response varchar(1024),
    test bool NOT NULL,
    PRIMARY KEY (id),
    CONSTRAINT fk_outgoing_webhook_deliveries_webhook_id FOREIGN KEY (webhook_id) REFERENCES outgoing_webhooks (id)
)`,
	`CREATE INDEX IF NOT EXISTS ix_outgoing_webhook_deliveries_created ON outgoing_webhook_deliveries (created_at)`,
	`CREATE INDEX IF NOT EXISTS ix_outgoing_webhook_deliveries_due ON outgoing_webhook_deliveries (next_attempt_at) WHERE ((status)::text = 'pending'::text)`,
	`CREATE INDEX IF NOT EXISTS ix_outgoing_webhook_deliveries_webhook ON outgoing_webhook_deliveries (webhook_id, created_at, id)`,
}

// outgoingWebhooks is migration 40.
//
// Telling another system that something happened was one setting,
// WEBHOOK_ENDPOINTS, which posted every event of every project to every URL
// in it, unsigned, once, and dropped what it could not keep up with. A
// project now names its own receivers and the events each wants, and what
// they are owed is a queue that commits with the step that raised it.
//
// One transaction, waited for no longer than deviationRequestsLockWait: the
// foreign key takes a brief lock on projects, as migration 37's does.
// PostgreSQL only, as 21 to 39.
func outgoingWebhooks() Migration {
	return Migration{
		Version: OutgoingWebhooksMigration,
		Name:    "a project can post its events to outgoing webhooks",
		Run:     addOutgoingWebhooks,
	}
}

func addOutgoingWebhooks(ctx context.Context, db *gorm.DB) error {
	if db.Name() != "postgres" {
		return nil
	}
	err := inBoundedTransaction(ctx, db, outgoingWebhooksDDL...)
	if lockNotAvailable(err) {
		return fmt.Errorf("projects was held for more than %s by a long query or transaction; "+
			"the upgrade stopped rather than hold every writer of it behind it, and will finish when started again once that ends: %w",
			deviationRequestsLockWait, err)
	}
	if err != nil {
		return fmt.Errorf("create the outgoing webhooks: %w", err)
	}
	return nil
}
