package migrations

import (
	"fmt"

	"gorm.io/gorm"
)

// DriftReport lists columns a model declares that the database does not have.
//
// This is the guard rail that makes strict migrations usable. Under AutoMigrate,
// adding a field to a model was enough — it appeared on the next boot. It no
// longer is, and the failure mode of forgetting is nasty: the application reads
// and writes a column that does not exist, and the error surfaces at the first
// request rather than at startup.
//
// So startup says so instead. Reported rather than fixed, because silently
// applying the change is the behaviour being removed.
func DriftReport(db *gorm.DB, models []any) ([]string, error) {
	migrator := db.Migrator()

	var drift []string
	for _, model := range models {
		statement, err := parseModel(db, model)
		if err != nil {
			return nil, err
		}

		table := statement.Table
		if !migrator.HasTable(model) {
			drift = append(drift, fmt.Sprintf("table %q is declared by a model but does not exist", table))
			continue
		}

		for _, field := range statement.Schema.Fields {
			// Fields with no column of their own — associations, and anything
			// marked ignored — have nothing to compare against.
			if field.DBName == "" || !field.Readable {
				continue
			}
			if !migrator.HasColumn(model, field.DBName) {
				drift = append(drift, fmt.Sprintf("%s.%s is declared by a model but missing from the database", table, field.DBName))
			}
		}
	}
	return drift, nil
}

// parseModel resolves a model to its GORM schema without touching the database.
func parseModel(db *gorm.DB, model any) (*gorm.Statement, error) {
	statement := &gorm.Statement{DB: db}
	if err := statement.Parse(model); err != nil {
		return nil, fmt.Errorf("parse model %T: %w", model, err)
	}
	return statement, nil
}
