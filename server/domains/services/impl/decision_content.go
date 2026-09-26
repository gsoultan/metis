package impl

import (
	"encoding/json"
	"fmt"
	"reflect"

	"github.com/gsoultan/metis/server/repositories/models"
)

// What makes two versions of a decision table the same policy.
//
// A save compares the table it is given with the version it was saved from and
// stores nothing when they agree. Compared: the name, the hit policy and its
// aggregation, the decisions it requires, its input and output columns, its
// rules in order, and the examples kept with it — everything a version holds
// that an author can change. Not compared: the id, the version number and when
// it was saved, which every version has its own of, and the key and project,
// which a save takes from the version it edits rather than from the request.
//
// Compared as the JSON the store writes, so a field is compared exactly as it
// would be kept. Two differences in spelling are not differences in policy and
// are read as equal: an absent list and an empty one — an editor that sends
// back `"tests": []` for a table stored with none has changed nothing — and a
// number written 5 or 5.0.

// decisionContent is the part of a version a save can change.
type decisionContent struct {
	Name              string                  `json:"name"`
	HitPolicy         string                  `json:"hit_policy"`
	Aggregation       string                  `json:"aggregation"`
	RequiredDecisions []string                `json:"required_decisions"`
	Inputs            []models.DecisionInput  `json:"inputs"`
	Outputs           []models.DecisionOutput `json:"outputs"`
	Rules             []models.DecisionRule   `json:"rules"`
	Tests             []models.DecisionTest   `json:"tests"`
}

// sameDecisionContent reports whether two versions state the same policy.
func sameDecisionContent(a, b models.DecisionDefinitionModel) (bool, error) {
	left, err := canonicalContent(a)
	if err != nil {
		return false, err
	}
	right, err := canonicalContent(b)
	if err != nil {
		return false, err
	}
	return reflect.DeepEqual(left, right), nil
}

// canonicalContent is a version's content as generic JSON, with the fields
// that hold nothing left out.
func canonicalContent(m models.DecisionDefinitionModel) (any, error) {
	raw, err := json.Marshal(decisionContent{
		Name:              m.Name,
		HitPolicy:         m.HitPolicy,
		Aggregation:       m.Aggregation,
		RequiredDecisions: m.RequiredDecisions,
		Inputs:            m.Inputs,
		Outputs:           m.Outputs,
		Rules:             m.Rules,
		Tests:             m.Tests,
	})
	if err != nil {
		return nil, fmt.Errorf("could not encode the decision to compare it: %w", err)
	}
	// Decoded rather than compared as bytes: the generic form reads every
	// number as a float64, and compares objects without regard to key order.
	var generic any
	if err := json.Unmarshal(raw, &generic); err != nil {
		return nil, fmt.Errorf("could not decode the decision to compare it: %w", err)
	}
	return withoutEmptyFields(generic), nil
}

// withoutEmptyFields drops the object fields that hold nothing: null, an empty
// list, an empty object.
//
// Only fields, never list elements. A rule's cells are matched to the table's
// columns by position, so an empty cell dropped from the middle of one would
// move every cell after it into the wrong column.
func withoutEmptyFields(value any) any {
	switch v := value.(type) {
	case map[string]any:
		kept := make(map[string]any, len(v))
		for name, field := range v {
			if field = withoutEmptyFields(field); !holdsNothing(field) {
				kept[name] = field
			}
		}
		return kept
	case []any:
		elements := make([]any, len(v))
		for i, element := range v {
			elements[i] = withoutEmptyFields(element)
		}
		return elements
	default:
		return value
	}
}

func holdsNothing(value any) bool {
	switch v := value.(type) {
	case nil:
		return true
	case map[string]any:
		return len(v) == 0
	case []any:
		return len(v) == 0
	default:
		return false
	}
}
