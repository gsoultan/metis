package models

import (
	"database/sql/driver"
	"encoding/json"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/gsoultan/metis/internal/pkg/crypto"
	"gorm.io/gorm"
)

// Base is a base model that uses UUID V7 for the ID.
type Base struct {
	ID UUID `gorm:"primaryKey" json:"id,omitzero"`
	// not null: the storm reader decodes a timestamptz by indexing eight bytes
	// out of the wire buffer, so a NULL is a panic on the read rather than a
	// zero time. GORM fills both on every write, which is why it has never
	// happened — but AutoMigrate left the column permitting it, and anything
	// that writes a row without going through GORM could. Enforced by
	// tests/drift/nullability_test.go; migration 22 does the same to tables
	// that already exist.
	//
	// No DEFAULT deliberately. A zero priority is a meaningful priority; an
	// invented created_at is a falsified audit trail, and this column is the
	// compliance answer to "when did this happen". A writer that omits it
	// should be refused, not quietly given now().
	CreatedAt time.Time      `gorm:"not null" json:"created_at,omitzero"`
	UpdatedAt time.Time      `gorm:"not null" json:"updated_at,omitzero"`
	DeletedAt gorm.DeletedAt `gorm:"index" json:"-"`
}

// BeforeCreate is a GORM hook that generates a new UUID V7 for the ID if it's nil.
func (b *Base) BeforeCreate(tx *gorm.DB) (err error) {
	if b.ID == NilUUID {
		id, genErr := uuid.NewV7()
		if genErr != nil {
			return genErr
		}
		b.ID = UUID(id)
	}
	return
}

// EncryptedMap is a map that is encrypted when stored in the database.
type EncryptedMap map[string]any

// Scan decrypts and unmarshals the value from the database.
func (m *EncryptedMap) Scan(value any) error {
	if value == nil {
		*m = nil
		return nil
	}

	s, ok := value.(string)
	if !ok {
		if b, ok := value.([]byte); ok {
			s = string(b)
		} else {
			return fmt.Errorf("invalid type for EncryptedMap: %T", value)
		}
	}

	if s == "" {
		*m = nil
		return nil
	}

	// Values carrying the scheme prefix must decrypt. A decryption failure here
	// means the key is wrong or the column is corrupt; returning the raw bytes
	// as if they were cleartext (the previous behaviour) would silently hand
	// ciphertext to the caller as business data.
	if crypto.IsCiphertext(s) {
		decrypted, err := crypto.Decrypt(s)
		if err != nil {
			return fmt.Errorf("decrypt variables column: %w", err)
		}
		return json.Unmarshal([]byte(decrypted), m)
	}

	// No prefix: a row written before encryption was introduced. Read it as
	// cleartext JSON so existing deployments keep working; the next write
	// re-persists it encrypted.
	return json.Unmarshal([]byte(s), m)
}

// Value marshals and encrypts the map for the database.
//
// An encryption failure is returned rather than swallowed. The previous version
// wrote the plaintext JSON instead, so a misconfigured or missing key silently
// downgraded every subsequent write to cleartext with nothing in the logs.
func (m EncryptedMap) Value() (driver.Value, error) {
	if m == nil {
		return nil, nil
	}

	b, err := json.Marshal(m)
	if err != nil {
		return nil, err
	}

	encrypted, err := crypto.Encrypt(string(b))
	if err != nil {
		return nil, fmt.Errorf("encrypt variables column: %w", err)
	}

	return encrypted, nil
}
