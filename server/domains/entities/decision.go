package entities

import (
	"github.com/google/uuid"
	"time"
)

const (
	HitPolicyUnique   = "UNIQUE"
	HitPolicyFirst    = "FIRST"
	HitPolicyPriority = "PRIORITY"
	HitPolicyAny      = "ANY"
	HitPolicyCollect  = "COLLECT"

	// HitPolicyOutputOrder returns every matching line, ordered by the priority
	// of its output value. HitPolicyRuleOrder returns every matching line in
	// table order. Both were absent, which left the DMN hit-policy set
	// incomplete: a table exported from another tool using either of them
	// silently fell through to "take the first line", quietly answering with
	// one value where the author asked for all of them.
	HitPolicyOutputOrder = "OUTPUT ORDER"
	HitPolicyRuleOrder   = "RULE ORDER"

	AggregationSum   = "SUM"
	AggregationCount = "COUNT"
	AggregationMin   = "MIN"
	AggregationMax   = "MAX"
)

// DecisionDefinition represents a DMN decision table or expression.
type DecisionDefinition struct {
	ID                uuid.UUID        `json:"id"`
	Project           *Project         `json:"project,omitzero"`
	Key               string           `json:"key"`
	Name              string           `json:"name"`
	Version           int              `json:"version"`
	HitPolicy         string           `json:"hit_policy"`
	Aggregation       string           `json:"aggregation,omitzero"`
	RequiredDecisions []string         `json:"required_decisions,omitzero"`
	Inputs            []DecisionInput  `json:"inputs,omitzero"`
	Outputs           []DecisionOutput `json:"outputs,omitzero"`
	Rules             []DecisionRule   `json:"rules,omitzero"`
	CreatedAt         time.Time        `json:"created_at,omitzero"`
}

// DecisionInput represents an input column in a decision table.
type DecisionInput struct {
	ID         string `json:"id"`
	Label      string `json:"label"`
	Expression string `json:"expression"`
	Type       string `json:"type"` // string, number, boolean
}

// DecisionOutput represents an output column in a decision table.
type DecisionOutput struct {
	ID    string `json:"id"`
	Label string `json:"label"`
	Name  string `json:"name"`
	Type  string `json:"type"`

	// Values is the ordered list of allowed output values, most important
	// first — DMN's "output values" list.
	//
	// PRIORITY and OUTPUT ORDER are defined entirely in terms of it: they rank
	// matching lines by where each line's output sits in this list, not by
	// where the line sits in the table. Without it PRIORITY has nothing to sort
	// by, which is why it used to be an alias for FIRST — a table asking for
	// "the most severe outcome that applies" got "the first one written down".
	Values []string `json:"values,omitzero"`
}

// DecisionRule represents a rule in a decision table.
type DecisionRule struct {
	ID          string   `json:"id"`
	Inputs      []string `json:"inputs,omitzero"`
	Outputs     []any    `json:"outputs,omitzero"`
	Description string   `json:"description,omitzero"`
}

// DecisionResult is the result of evaluating a decision.
type DecisionResult struct {
	Values map[string]any `json:"values"`

	// MatchedRules are the positions in the table of the lines that produced
	// this result, in table order. "Why did it decide that?" is the question a
	// decision table exists to answer, and without these the answer arrives
	// with no reasoning attached — the editor highlights the line that applied,
	// and had nothing to highlight.
	//
	// Under FIRST this holds one entry; under COLLECT, every line that matched.
	MatchedRules []int `json:"matched_rules,omitzero"`

	// The identity of what decided, carried on the result rather than looked up
	// again by whoever wants to record it.
	//
	// An audit entry naming only the outputs answers "what was decided" and not
	// "by what, and on what grounds" — and the second is the question asked six
	// months later, by someone who needs to know which version of the policy was
	// in force. The table it came from is versioned and immutable, so these four
	// fields are enough to reconstruct the reasoning exactly.
	DecisionKey     string `json:"decision_key,omitzero"`
	DecisionName    string `json:"decision_name,omitzero"`
	DecisionVersion int    `json:"decision_version,omitzero"`

	// MatchedRuleIDs are the same lines as MatchedRules, named rather than
	// numbered. A position is only meaningful against the version of the table
	// that produced it; an ID survives the table being edited.
	MatchedRuleIDs []string `json:"matched_rule_ids,omitzero"`
}

// DecisionImpact answers "what breaks if I change this?".
//
// A decision table is a policy several processes can share, and the person
// about to edit one is usually the person least able to see who else depends on
// it. Changing a threshold with three hundred instances part-way through the
// process that reads it is a different act from changing one nothing uses, and
// the difference should be visible before the change, not after.
type DecisionImpact struct {
	DecisionKey string `json:"decision_key"`

	// Processes that can reach this decision.
	Processes []DecisionUsage `json:"processes,omitzero"`

	// RunningInstances is the total across those processes — the number of
	// business commitments already in flight under the current policy.
	RunningInstances int `json:"running_instances"`
}

// DecisionUsage is one process that consults a decision.
type DecisionUsage struct {
	DefinitionID   uuid.UUID `json:"definition_id"`
	DefinitionKey  string    `json:"definition_key"`
	DefinitionName string    `json:"definition_name,omitzero"`
	Version        int       `json:"version"`

	// Steps names the steps that consult it — "Score the applicant" tells
	// somebody where the policy is used; a count does not.
	Steps []string `json:"steps,omitzero"`

	RunningInstances int `json:"running_instances"`
}
