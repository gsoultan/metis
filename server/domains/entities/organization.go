package entities

import (
	"time"

	"github.com/google/uuid"
)

// Organization represents a top-level entity that can contain multiple projects.
type Organization struct {
	ID          uuid.UUID `json:"id"`
	Name        string    `json:"name"`
	Description string    `json:"description,omitzero"`
	CreatedAt   time.Time `json:"created_at,omitzero"`
	UpdatedAt   time.Time `json:"updated_at,omitzero"`
}
