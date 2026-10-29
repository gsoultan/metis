package models

type FormModel struct {
	Base
	ProjectID UUID   `json:"project_id,omitzero"`
	Key       string `json:"key"`
	Name      string `json:"name"`
	// column:fields — the storm model calls it Fields, because Schema is
	// storm's own declaration hook and a field cannot share the name. Two
	// names for one column is what the drift check exists to catch, so the
	// column follows the model that will outlive this one.
	Schema map[string]any `json:"schema,omitzero"`
}

func (FormModel) TableName() string {
	return "forms"
}
