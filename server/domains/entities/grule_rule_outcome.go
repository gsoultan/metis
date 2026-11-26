package entities

// GruleRuleOutcome is what became of one rule.
type GruleRuleOutcome struct {
	Name     string `json:"name"`
	Position string `json:"position"`

	// Decision is the key of the table the rule is a line of, empty when the
	// rule was not imported; Reason then says why.
	Decision string `json:"decision,omitzero"`
	Reason   string `json:"reason,omitzero"`

	// Notes are said about a rule that was imported.
	Notes []string `json:"notes,omitzero"`
}
