package entities

import (
	"time"

	"github.com/google/uuid"
)

// Project represents a workspace for grouping process definitions, instances, and tasks.
type Project struct {
	ID           uuid.UUID     `json:"id"`
	Organization *Organization `json:"organization,omitzero"`
	Name         string        `json:"name"`
	Description  string        `json:"description,omitzero"`
	CreatedAt    time.Time     `json:"created_at,omitzero"`
	UpdatedAt    time.Time     `json:"updated_at,omitzero"`
}
