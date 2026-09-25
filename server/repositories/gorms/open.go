package gorms

import (
	"fmt"

	"github.com/gsoultan/metis/internal/pkg/config"
	"github.com/gsoultan/metis/internal/pkg/dbpool"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

// Dialector builds the driver for a connection string.
//
// One answer rather than one per call site, for the same reason Config is: the
// application's own boot, the setup wizard and every environment connection all
// have to open a database the same way, and a setting one of them does not share
// is a setting nothing checks.
//
// PostgreSQL only. It used to fall back to SQLite for anything it did not
// recognise, which meant a config naming a driver this no longer supports would
// open a fresh empty file beside the real database and start serving from it —
// indistinguishable, to whoever restarted the service, from total data loss.
// An unrecognised driver is now an error at the point of opening.
func Dialector(driver, dsn string) (gorm.Dialector, error) {
	if driver != config.DriverPostgres {
		return nil, fmt.Errorf(
			"this runs on PostgreSQL; %q is not a database engine it supports", driver)
	}
	return postgres.Open(dsn), nil
}

// Open connects to a database with this codebase's settings applied.
//
// Opening in GORM does not contact the server — the first query does — so a
// successful return here means the connection string parsed, not that the
// database answered. Callers that need to know it is reachable have to ask it
// something; see Ping.
func Open(driver, dsn string) (*gorm.DB, error) {
	dialector, err := Dialector(driver, dsn)
	if err != nil {
		return nil, err
	}
	db, err := gorm.Open(dialector, Config())
	if err != nil {
		return nil, fmt.Errorf("could not open the %s database: %w", driver, err)
	}
	dbpool.Apply(db)
	return db, nil
}

// Ping asks the database a question, which is the only way to learn it is
// reachable — opening a connection does not.
func Ping(db *gorm.DB) error {
	sqlDB, err := db.DB()
	if err != nil {
		return fmt.Errorf("could not reach the connection pool: %w", err)
	}
	if err := sqlDB.Ping(); err != nil {
		return fmt.Errorf("the database did not answer: %w", err)
	}
	return nil
}

// Config is the GORM configuration every connection to this schema must use.
//
// It exists so there is one answer rather than one per gorm.Open call site.
// Test harnesses must use it too: a setting the tests do not share is a setting
// the tests do not check.
func Config() *gorm.Config {
	return &gorm.Config{
		// TranslateError turns each driver's own way of saying "that row already
		// exists" into gorm.ErrDuplicatedKey. Without it, recognising a unique
		// constraint means matching error text per dialect — four spellings to
		// keep in step, and the wrong one is discovered in production. The
		// definition version allocator depends on it.
		TranslateError: true,
	}
}
