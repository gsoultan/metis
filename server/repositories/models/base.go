package models

import (
	"database/sql/driver"
	"encoding/json"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/gsoultan/gobpm/internal/pkg/crypto"
	"gorm.io/gorm"
)

// Base is a base model that uses UUID V7 for the ID.
type Base struct {
	ID        uuid.UUID      `gorm:"type:uuid;primaryKey" json:"id,omitzero"`
	CreatedAt time.Time      `json:"created_at,omitzero"`
	UpdatedAt time.Time      `json:"updated_at,omitzero"`
	DeletedAt gorm.DeletedAt `gorm:"index" json:"-"`
}

// BeforeCreate is a GORM hook that generates a new UUID V7 for the ID if it's nil.
func (b *Base) BeforeCreate(tx *gorm.DB) (err error) {
	if b.ID == uuid.Nil {
		b.ID, err = uuid.NewV7()
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
