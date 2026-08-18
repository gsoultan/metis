package entities

import (
	"time"

	"github.com/google/uuid"
)

// Deployment represents a set of resources deployed together.
type Deployment struct {
	ID        uuid.UUID  `json:"id"`
	Project   *Project   `json:"project,omitzero"`
	Name      string     `json:"name"`
	CreatedAt time.Time  `json:"created_at"`
	Resources []Resource `json:"resources,omitzero"`
}

// Resource represents a file or content within a deployment.
type Resource struct {
	ID         uuid.UUID   `json:"id"`
	Deployment *Deployment `json:"deployment,omitzero"`
	Name       string      `json:"name"`
	Content    []byte      `json:"content"`
	Type       string      `json:"type"`
}
