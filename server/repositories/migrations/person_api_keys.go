package migrations

import (
	"context"
	"fmt"

	"gorm.io/gorm"
)

// PersonAPIKeysMigration is the version under which a person could call the
// People task API with a key of their own. 47 is task links'.
const PersonAPIKeysMigration = 48

// personAPIKeysDDL is frozen text: the table migration 48 made, which must not
// change when the model does. tests/migrations compares it with the storm
// model and the model is the arbiter.
//
// The key belongs to a participant, so the foreign key is to workflow_users.
// The indexes are plain builds: the table has
// just been created and is read by nothing yet.
var personAPIKeysDDL = []string{
	`CREATE TABLE IF NOT EXISTS person_api_keys (
    id uuid DEFAULT gen_random_uuid() NOT NULL,
    created_at timestamptz DEFAULT now() NOT NULL,
    updated_at timestamptz DEFAULT now() NOT NULL,
    participant_id uuid NOT NULL,
    name varchar(100) NOT NULL,
    prefix varchar(16) NOT NULL,
    token_hash varchar(64) NOT NULL,
    last_used_at timestamptz,
    revoked_at timestamptz,
    PRIMARY KEY (id),
    CONSTRAINT fk_person_api_keys_participant_id FOREIGN KEY (participant_id) REFERENCES workflow_users (id)
)`,
	`CREATE UNIQUE INDEX IF NOT EXISTS ux_person_api_keys_token_hash ON person_api_keys (token_hash)`,
	`CREATE INDEX IF NOT EXISTS ix_person_api_keys_participant_id ON person_api_keys (participant_id)`,
}

// personAPIKeys is migration 48.
//
// A person's own system — a script that approves from a spreadsheet, a
// supplier's portal — had no way to act as them but their password. A key per
// system, revoked on its own, is what they hand it instead.
//
// One transaction, waited for no longer than deviationRequestsLockWait: the
// foreign key takes a brief lock on workflow_users. PostgreSQL only, as 21 to
// 47, and only where the People directory is: an environment's database has
// no workflow_users, and its People sign in through the main database.
func personAPIKeys() Migration {
	return Migration{
		Version: PersonAPIKeysMigration,
		Name:    "a person can call the People task API with a key of their own",
		Run:     addPersonAPIKeys,
	}
}

func addPersonAPIKeys(ctx context.Context, db *gorm.DB) error {
	if db.Name() != "postgres" || !db.WithContext(ctx).Migrator().HasTable("workflow_users") {
		return nil
	}
	err := inBoundedTransaction(ctx, db, personAPIKeysDDL...)
	if lockNotAvailable(err) {
		return fmt.Errorf("workflow_users was held for more than %s by a long query or transaction; "+
			"the upgrade stopped rather than hold every writer of it behind it, and will finish when started again once that ends: %w",
			deviationRequestsLockWait, err)
	}
	if err != nil {
		return fmt.Errorf("create the personal API keys: %w", err)
	}
	return nil
}
