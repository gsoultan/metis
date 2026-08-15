package models

import (
	"database/sql/driver"

	"github.com/google/uuid"
	"gorm.io/gorm"
	"gorm.io/gorm/schema"
)

// UUID is uuid.UUID carrying a database column type that every supported engine
// understands.
//
// The models used to pin these columns with `gorm:"type:uuid"`. PostgreSQL and
// SQLite accept that; MySQL has no uuid type, so AutoMigrate failed on the first
// table and the server could not start against MySQL at all — even though MySQL
// is offered in the config and the setup wizard. Nothing caught it because every
// test ran on SQLite.
//
// GORM asks the field's type for its column type before falling back to the
// `type:` tag, so putting the decision here keeps it with the data rather than
// repeated across 30-odd struct tags. PostgreSQL and SQLite still get `uuid`, so
// existing deployments see no schema change.
type UUID uuid.UUID

// NilUUID is the zero value, matching uuid.Nil.
var NilUUID = UUID(uuid.Nil)

// GormDBDataType gives each dialect a column type it actually has.
func (u UUID) GormDBDataType(db *gorm.DB, _ *schema.Field) string {
	if db == nil || db.Dialector == nil {
		return "uuid"
	}
	switch db.Name() {
	case "mysql":
		// MySQL has no uuid type. char(36) holds the canonical hyphenated form
		// that Value below writes, and compares as a fixed-width string.
		return "char(36)"
	default:
		// postgres, sqlite and anything else that understood the previous tag.
		return "uuid"
	}
}

// Value writes the canonical string form, exactly as uuid.UUID did before.
func (u UUID) Value() (driver.Value, error) {
	return uuid.UUID(u).Value()
}

// Scan reads whatever the driver returns, delegating to uuid.UUID so string and
// []byte columns both work.
func (u *UUID) Scan(src any) error {
	return (*uuid.UUID)(u).Scan(src)
}

// MarshalText and UnmarshalText keep JSON encoding as the hyphenated string.
// A defined type does not inherit its underlying type's methods, so without
// these a UUID would marshal as a 16-element byte array.
func (u UUID) MarshalText() ([]byte, error) {
	return uuid.UUID(u).MarshalText()
}

func (u *UUID) UnmarshalText(data []byte) error {
	return (*uuid.UUID)(u).UnmarshalText(data)
}

func (u UUID) String() string {
	return uuid.UUID(u).String()
}

// ToUUID and FromUUID convert at the persistence boundary.
func (u UUID) ToUUID() uuid.UUID { return uuid.UUID(u) }

func FromUUID(id uuid.UUID) UUID { return UUID(id) }

// FromUUIDPtr converts an optional identifier, preserving nil.
func FromUUIDPtr(id *uuid.UUID) *UUID {
	if id == nil {
		return nil
	}
	converted := UUID(*id)
	return &converted
}

// ToUUIDPtr converts an optional identifier back, preserving nil.
func ToUUIDPtr(id *UUID) *uuid.UUID {
	if id == nil {
		return nil
	}
	converted := uuid.UUID(*id)
	return &converted
}
