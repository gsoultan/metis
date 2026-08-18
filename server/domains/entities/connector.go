package entities

import (
	"time"

	"github.com/google/uuid"
)

// Connector represents a pre-built service connector template (e.g., Slack, Jira, Email).
type Connector struct {
	ID          uuid.UUID           `json:"id"`
	Key         string              `json:"key"` // e.g., "slack-message", "http-json"
	Name        string              `json:"name"`
	Description string              `json:"description,omitzero"`
	Icon        string              `json:"icon,omitzero"` // Lucide icon name or SVG
	Type        string              `json:"type"`          // e.g., "social", "utility", "communication"
	Schema      []ConnectorProperty `json:"schema,omitzero"`
	CreatedAt   time.Time           `json:"created_at,omitzero"`
}

// ConnectorProperty defines the schema for a connector's configuration.
type ConnectorProperty struct {
	Key          string `json:"key"`
	Label        string `json:"label"`
	Type         string `json:"type"` // string, password, boolean, number, select
	Description  string `json:"description,omitzero"`
	DefaultValue string `json:"default_value,omitzero"`
	Required     bool   `json:"required,omitzero"`
	Options      []any  `json:"options,omitzero"` // For select type
}

// ConnectorInstance represents a specific configuration of a connector for a project.
type ConnectorInstance struct {
	ID        uuid.UUID      `json:"id"`
	Project   *Project       `json:"project,omitzero"`
	Connector *Connector     `json:"connector,omitzero"`
	Name      string         `json:"name"`
	Config    map[string]any `json:"config,omitzero"` // Actual values for the properties
	CreatedAt time.Time      `json:"created_at,omitzero"`
	UpdatedAt time.Time      `json:"updated_at,omitzero"`
}

// ConnectorManifest is a connector described by a document, as a catalogue
// lists it. The document itself is fetched separately — a list of forty
// connectors does not need forty YAML documents in it.
type ConnectorManifest struct {
	ID      uuid.UUID `json:"id,omitzero"`
	Key     string    `json:"key"`
	Name    string    `json:"name,omitzero"`
	Version int       `json:"version,omitzero"`
	Enabled bool      `json:"enabled"`
}
