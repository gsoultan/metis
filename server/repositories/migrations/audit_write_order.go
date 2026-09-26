package migrations

import (
	"context"
	"fmt"

	"gorm.io/gorm"
)

// AuditWriteOrderMigration is the version "an audit entry records the order it
// was written in" is recorded under. Named once, here, so the test that rewinds
// it and the list that runs it cannot disagree about which row they mean.
const AuditWriteOrderMigration = 28

// auditWriteOrder is migration 28.
//
// The audit trail is read oldest first, by created_at — the moment the writing
// transaction began, which every entry one step writes shares — and nothing
// recorded the order among those. They came back in whatever order PostgreSQL
// returned the ties: the order the rows were stored in, which is the order
// they were written only until something moves a row. The ids could not
// break the tie either: they are random.
//
// So each entry now takes a number from a sequence as it is written, and the
// trail is read by created_at and then by that. A sequence rather than
// time-ordered ids, for three reasons. It is the database's: every writer gets
// one without supplying anything — the audit observer, which leaves the id to
// the column's default, a release still running during a rolling upgrade, a
// script — and replicas need not agree about the time. Within a transaction it
// is the order of the inserts, exactly. And entries written before this
// migration keep the order they had, where ordering by random ids would have
// shuffled every trail already recorded.
//
// Those entries are left without a number. Nothing recorded the order they
// were written in, and numbering them from where each row happens to be stored
// would rewrite every audit row in the table to record a guess.
//
// Two catalogue changes and a sequence: adding a nullable column with no
// default and setting a default rewrite no rows, so the table is locked for as
// long as the catalogue takes, not for as long as the trail is. Transactional,
// so no entry is written between the column and its default and left without a
// number it could have had. PostgreSQL only, as 21 to 23 and 27: anywhere else
// the baseline has just built the table from the current model.
func auditWriteOrder() Migration {
	return Migration{
		Version:       AuditWriteOrderMigration,
		Name:          "an audit entry records the order it was written in",
		Transactional: true,
		Run:           EnsureAuditWriteOrder,
	}
}

// auditWriteOrderStatements number audit entries as they are written.
//
// The sequence is owned by the column, as a serial's is, and named as
// PostgreSQL names a serial's. Owned is what makes pg_get_serial_sequence find
// it, and what storm's introspection reads as a serial rather than as a bare
// default naming one database's sequence — the model and the table agree.
var auditWriteOrderStatements = []string{
	`ALTER TABLE audit_logs ADD COLUMN IF NOT EXISTS seq bigint`,
	`CREATE SEQUENCE IF NOT EXISTS audit_logs_seq_seq`,
	`ALTER SEQUENCE audit_logs_seq_seq OWNED BY audit_logs.seq`,
	`ALTER TABLE audit_logs ALTER COLUMN seq SET DEFAULT nextval('audit_logs_seq_seq')`,
}

// EnsureAuditWriteOrder gives audit_logs the column and the sequence that
// record the order entries are written in. Every statement is idempotent, so
// it is safe on a table that already has them.
//
// Exported for the test harnesses, which build their schema with AutoMigrate
// rather than the migration runner — as EnsureVersionIndexes is. AutoMigrate
// creates the column from the model with no sequence behind it, and a trail
// read in a test would then tie exactly where this is meant to break the tie.
func EnsureAuditWriteOrder(ctx context.Context, db *gorm.DB) error {
	if db.Name() != "postgres" {
		return nil
	}
	for _, stmt := range auditWriteOrderStatements {
		if err := db.WithContext(ctx).Exec(stmt).Error; err != nil {
			return fmt.Errorf("number audit entries as they are written: %w", err)
		}
	}
	return nil
}
