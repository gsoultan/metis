package models

// DecisionDefinitionModel represents the GORM model for decision definitions.
type DecisionDefinitionModel struct {
	Base
	// (project_id, key, version) is unique, for the same reason it is on
	// ProcessDefinitionModel: the version is allocated read-then-write, and only
	// the database can settle a race between two deployments of the same key.
	// The constraint lives in a migration, not a tag — see that model.
	ProjectID         UUID             `gorm:"index" json:"project_id,omitzero"`
	Key               string           `gorm:"size:255;index" json:"key"`
	Name              string           `json:"name"`
	Version           int              `gorm:"not null;default:0" json:"version"`
	HitPolicy         string           `json:"hit_policy"`
	Aggregation       string           `json:"aggregation,omitzero"`
	RequiredDecisions []string         `gorm:"type:text;serializer:json" json:"required_decisions,omitzero"`
	Inputs            []DecisionInput  `gorm:"type:text;serializer:json" json:"inputs,omitzero"`
	Outputs           []DecisionOutput `gorm:"type:text;serializer:json" json:"outputs,omitzero"`
	Rules             []DecisionRule   `gorm:"type:text;serializer:json" json:"rules,omitzero"`

	// Tests are the examples this table is expected to get right — see
	// entities.DecisionTest. Added by migration 6.
	Tests []DecisionTest `gorm:"type:text;serializer:json" json:"tests,omitzero"`
}

// DecisionInput represents an input column in a decision table in the database.
type DecisionInput struct {
	ID         string `json:"id"`
	Label      string `json:"label"`
	Expression string `json:"expression"`
	Type       string `json:"type"` // string, number, boolean
}

// DecisionOutput represents an output column in a decision table in the database.
type DecisionOutput struct {
	ID    string `json:"id"`
	Label string `json:"label"`
	Name  string `json:"name"`
	Type  string `json:"type"`

	// Values is the ordered priority list — see entities.DecisionOutput. It
	// needs no migration: outputs are stored as JSON in one column, so the
	// field simply starts appearing. Tables written before it have none, which
	// is the same state as an author who has not set one.
	Values []string `json:"values,omitzero"`
}

// DecisionRule represents a rule in a decision table in the database.
type DecisionRule struct {
	ID          string   `json:"id"`
	Inputs      []string `json:"inputs,omitzero"`
	Outputs     []any    `json:"outputs,omitzero"`
	Description string   `json:"description,omitzero"`
}

// TableName overrides the table name for DecisionDefinitionModel.
func (DecisionDefinitionModel) TableName() string {
	return "decision_definitions"
}

// DecisionTest is an example the table is expected to get right.
type DecisionTest struct {
	ID       string         `json:"id"`
	Name     string         `json:"name"`
	Inputs   map[string]any `json:"inputs,omitzero"`
	Expected map[string]any `json:"expected,omitzero"`
}

// DecisionSummaryModel is one key of decision_definitions as its newest
// version, read without the table columns: a list of decisions names them and
// says what each requires, and has no use for their lines or examples.
type DecisionSummaryModel struct {
	ID                UUID
	ProjectID         UUID
	Key               string
	Name              string
	Version           int
	RequiredDecisions []string
}
