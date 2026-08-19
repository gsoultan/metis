package entities

import (
	"time"

	"github.com/google/uuid"
)

// Form represents a user-defined form schema.
type Form struct {
	ID        uuid.UUID      `json:"id"`
	Project   *Project       `json:"project,omitzero"`
	Key       string         `json:"key"`
	Name      string         `json:"name"`
	Schema    map[string]any `json:"schema"`
	CreatedAt time.Time      `json:"created_at"`
}
