package entities

// RuleFile is one rule file handed to an import: its name, which says whether
// it is GRL or JSON, and what it holds.
type RuleFile struct {
	Name    string `json:"name"`
	Content string `json:"content"`
}
