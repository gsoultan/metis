package entities

import "github.com/google/uuid"

// JavaScriptConditionUsage locates one stored `js:` condition — one line of the
// worklist for the javascript-conditions feature flag. The flag ships off, so a
// definition on this list has a decision point that will refuse to route until
// the condition is rewritten in FEEL (or the flag is explicitly turned on).
//
// It reports exactly the fields the condition chain evaluates: sequence-flow
// conditions and completion conditions. Node.Condition is deliberately absent —
// it carries timer expressions and script bodies, which the flag does not gate.
type JavaScriptConditionUsage struct {
	DefinitionID   uuid.UUID `json:"definition_id"`
	DefinitionKey  string    `json:"definition_key"`
	DefinitionName string    `json:"definition_name"`
	Version        int       `json:"version"`

	// ElementID names the sequence flow or node carrying the condition;
	// ElementName is the node's display name, empty for flows (they have none).
	ElementID   string `json:"element_id"`
	ElementName string `json:"element_name,omitzero"`

	// Where says which field carries it: "flow condition" or
	// "completion condition".
	Where     string `json:"where"`
	Condition string `json:"condition"`
}
