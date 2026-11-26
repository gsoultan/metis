package entities

// GruleImport is what a set of Grule rules becomes in metis: the decision
// tables that answer for the rules that could be translated, a process that
// consults them in order, and, for every rule, whether it was imported and
// why not when it was not.
//
// The same shape is the preview and the result. A preview stores nothing; an
// import stores the decisions and, when asked, deploys the process, and fills
// in the ids.
type GruleImport struct {
	Decisions []DecisionDefinition `json:"decisions"`
	Process   *ProcessDefinition   `json:"process,omitzero"`
	Rules     []GruleRuleOutcome   `json:"rules"`

	// Results names the process variable each field a rule sets is written
	// to: `Applicant.Discount` → `Applicant_Discount`.
	Results map[string]string `json:"results,omitzero"`

	// Warnings are said about the import as a whole.
	Warnings []string `json:"warnings,omitzero"`

	// Stored is true once the decisions have been saved, and ProcessID is set
	// once the process has been deployed.
	Stored    bool   `json:"stored,omitzero"`
	ProcessID string `json:"process_id,omitzero"`
}
