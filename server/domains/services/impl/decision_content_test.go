package impl

import (
	"testing"

	"github.com/gsoultan/metis/server/repositories/models"
)

// A save stores nothing when the table is the one it was saved from. What
// counts as "the same" decides whether an author's change is kept, so it is
// pinned case by case: a spelling difference is not a change, and anything an
// evaluation or a reader would see differently is.
func TestSameDecisionContent(t *testing.T) {
	stored := func() models.DecisionDefinitionModel {
		return models.DecisionDefinitionModel{
			Base:      models.Base{ID: models.UUID{1}},
			Key:       "credit-band",
			Name:      "Credit band",
			Version:   3,
			HitPolicy: "FIRST",
			Inputs:    []models.DecisionInput{{ID: "in", Label: "Score", Expression: "score", Type: "number"}},
			Outputs:   []models.DecisionOutput{{ID: "out", Label: "Band", Name: "band", Type: "string"}},
			Rules: []models.DecisionRule{
				{ID: "r1", Inputs: []string{"> 10"}, Outputs: []any{"HIGH"}},
				{ID: "r2", Inputs: []string{"-"}, Outputs: []any{5.0}},
			},
		}
	}

	cases := []struct {
		name string
		edit func(*models.DecisionDefinitionModel)
		same bool
	}{
		{name: "nothing changed", edit: func(*models.DecisionDefinitionModel) {}, same: true},
		{
			name: "empty lists spelled out rather than left out",
			edit: func(m *models.DecisionDefinitionModel) {
				m.RequiredDecisions = []string{}
				m.Tests = []models.DecisionTest{}
				m.Outputs[0].Values = []string{}
			},
			same: true,
		},
		{
			name: "a whole number written as an integer",
			edit: func(m *models.DecisionDefinitionModel) { m.Rules[1].Outputs = []any{5} },
			same: true,
		},
		{
			name: "only what a save takes from elsewhere differs",
			edit: func(m *models.DecisionDefinitionModel) {
				m.ID = models.UUID{2}
				m.Version = 4
				m.Key = "renamed"
			},
			same: true,
		},
		{name: "a threshold", edit: func(m *models.DecisionDefinitionModel) { m.Rules[0].Inputs = []string{"> 20"} }},
		{name: "an answer", edit: func(m *models.DecisionDefinitionModel) { m.Rules[0].Outputs = []any{"LOW"} }},
		{
			name: "the order of the rules",
			edit: func(m *models.DecisionDefinitionModel) { m.Rules[0], m.Rules[1] = m.Rules[1], m.Rules[0] },
		},
		{name: "the hit policy", edit: func(m *models.DecisionDefinitionModel) { m.HitPolicy = "UNIQUE" }},
		{name: "the name", edit: func(m *models.DecisionDefinitionModel) { m.Name = "Credit tier" }},
		{
			name: "a required decision",
			edit: func(m *models.DecisionDefinitionModel) { m.RequiredDecisions = []string{"risk"} },
		},
		{
			name: "an example",
			edit: func(m *models.DecisionDefinitionModel) {
				m.Tests = []models.DecisionTest{{ID: "t1", Name: "a high score", Inputs: map[string]any{"score": 20}}}
			},
		},
		{
			name: "an empty cell is still a cell",
			edit: func(m *models.DecisionDefinitionModel) { m.Rules[0].Inputs = []string{"> 10", ""} },
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			edited := stored()
			c.edit(&edited)
			same, err := sameDecisionContent(stored(), edited)
			if err != nil {
				t.Fatalf("compare: %v", err)
			}
			if same != c.same {
				t.Errorf("same = %v, want %v", same, c.same)
			}
		})
	}
}
