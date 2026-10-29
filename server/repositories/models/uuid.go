package models

import (
	"database/sql/driver"

	"github.com/google/uuid"
)

// UUID is uuid.UUID as the models carry it: written as its canonical string,
// and marshalled to JSON as one.
type UUID uuid.UUID

// NilUUID is the zero value, matching uuid.Nil.
var NilUUID = UUID(uuid.Nil)

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
