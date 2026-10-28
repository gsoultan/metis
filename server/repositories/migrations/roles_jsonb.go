package migrations

import (
	"context"
	"fmt"

	"gorm.io/gorm"
)

// RolesJSONBMigration is the version under which an account's and a group's
// roles became jsonb. 48 is personal API keys'.
const RolesJSONBMigration = 49

// rolesJSONBDDL is frozen text: what migration 49 does, which must not change
// when the model does.
//
// GORM made users.roles and groups.roles text, with the JSON array as its
// characters; the model has always said jsonb, as user_organizations.roles is.
// As text, the question "does this account hold the administrator role" could
// only be asked of the database as a LIKE over the characters, and answered
// in Go. As jsonb it is ?|, asked and answered by the database.
//
// An empty value is not JSON, and names no role: it becomes NULL, which the
// readers already take as no roles. Every other value is what json.Marshal
// wrote, so the cast holds; a value it does not hold for stops the migration
// rather than losing the roles it spelled.
var rolesJSONBDDL = []string{
	`ALTER TABLE users ALTER COLUMN roles TYPE jsonb
	   USING CASE WHEN btrim(roles) = '' THEN NULL ELSE roles::jsonb END`,
	`ALTER TABLE groups ALTER COLUMN roles TYPE jsonb
	   USING CASE WHEN btrim(roles) = '' THEN NULL ELSE roles::jsonb END`,
}

// rolesJSONB is migration 49.
//
// Each ALTER rewrites its table under an exclusive lock. Both tables are the
// directory's accounts and groups: rows in the thousands, rewritten in
// milliseconds. The wait for the lock is bounded as 34 to 48 bound theirs, so
// a long transaction holding either table stops the upgrade rather than
// queueing every sign-in behind it.
//
// PostgreSQL only, as 21 to 48, and only where a column is still text: a
// database whose roles are already jsonb has nothing to do, and running the
// migration again is harmless.
func rolesJSONB() Migration {
	return Migration{
		Version: RolesJSONBMigration,
		Name:    "an account's and a group's roles are jsonb",
		Run:     EnsureRolesJSONB,
	}
}

// EnsureRolesJSONB makes users.roles and groups.roles jsonb where they are
// still text, and does nothing where they are not.
//
// Exported for the test harnesses, which build their schema with AutoMigrate
// rather than the migration runner — as EnsureAuditWriteOrder is. AutoMigrate
// makes both columns text from the GORM tags, and the repository's ?| has no
// meaning on text.
func EnsureRolesJSONB(ctx context.Context, db *gorm.DB) error {
	if db.Name() != "postgres" {
		return nil
	}
	var statements []string
	for i, table := range []string{"users", "groups"} {
		text, err := columnIsText(ctx, db, table, "roles")
		if err != nil {
			return err
		}
		if text {
			statements = append(statements, rolesJSONBDDL[i])
		}
	}
	if len(statements) == 0 {
		return nil
	}
	err := inBoundedTransaction(ctx, db, statements...)
	if lockNotAvailable(err) {
		return fmt.Errorf("users or groups was held for more than %s by a long query or transaction; "+
			"the upgrade stopped rather than hold every sign-in behind it, and will finish when started again once that ends: %w",
			deviationRequestsLockWait, err)
	}
	if err != nil {
		return fmt.Errorf("make roles jsonb: %w", err)
	}
	return nil
}

// columnIsText reports whether a column of a table on the search path is
// text. A table or column that is not there is not text.
func columnIsText(ctx context.Context, db *gorm.DB, table, column string) (bool, error) {
	var dataType string
	err := db.WithContext(ctx).Raw(
		`SELECT data_type FROM information_schema.columns
		  WHERE table_schema = current_schema() AND table_name = ? AND column_name = ?`,
		table, column).Scan(&dataType).Error
	if err != nil {
		return false, fmt.Errorf("could not read the type of %s.%s: %w", table, column, err)
	}
	return dataType == "text", nil
}
