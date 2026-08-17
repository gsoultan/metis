package migrations

import "time"

// SchemaMigration records one applied migration. It is the answer to "which
// version is this database at", which AutoMigrate could never give.
type SchemaMigration struct {
	// autoIncrement must be off. GORM makes an integer primary key
	// auto-incrementing by default, and this one is an identity we assign — a
	// database that renumbered it would lose track of which migration is which.
	Version   int       `gorm:"primaryKey;autoIncrement:false"`
	Name      string    `gorm:"size:255"`
	AppliedAt time.Time `gorm:"autoCreateTime"`
	// DurationMS is kept because the first question about a slow deployment is
	// which migration was slow, and it is unanswerable afterwards otherwise.
	DurationMS int64
}

// TableName overrides the table name for SchemaMigration.
func (SchemaMigration) TableName() string { return "schema_migrations" }

// schemaLock is a single-row mutual exclusion held while migrations run.
//
// Replicas start together, so without it several would run the same migration
// at once. Advisory locks would be neater but they differ across all four
// supported engines; a row whose primary key can only be inserted once behaves
// the same everywhere.
type schemaLock struct {
	// Same reason as SchemaMigration.Version: the row's ID is always lockRowID,
	// and an auto-incrementing column would hand out a second one instead of
	// rejecting the duplicate the lock depends on.
	ID         int    `gorm:"primaryKey;autoIncrement:false"`
	Owner      string `gorm:"size:255"`
	AcquiredAt time.Time
}

// TableName overrides the table name for schemaLock.
func (schemaLock) TableName() string { return "schema_migration_locks" }

// lockRowID is the only row this table ever holds.
const lockRowID = 1
