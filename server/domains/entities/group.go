package entities

import (
	"time"

	"github.com/google/uuid"
)

// Group represents a user group for task assignment.
type Group struct {
	ID           uuid.UUID     `json:"id"`
	Organization *Organization `json:"organization,omitzero"`
	Name         string        `json:"name"`
	Description  string        `json:"description"`
	Roles        []string      `json:"roles,omitzero"`
	CreatedAt    time.Time     `json:"created_at,omitzero"`
}
