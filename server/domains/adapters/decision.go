package adapters

import (
	"github.com/google/uuid"
	"github.com/gsoultan/gobpm/server/domains/entities"
	"github.com/gsoultan/gobpm/server/repositories/models"
)

type DecisionModelAdapter struct {
	Decision entities.DecisionDefinition
}

func (a DecisionModelAdapter) ToModel() models.DecisionDefinitionModel {
	var projectID uuid.UUID
	if a.Decision.Project != nil {
		projectID = a.Decision.Project.ID
	}
	inputs := make([]models.DecisionInput, len(a.Decision.Inputs))
	for i, in := range a.Decision.Inputs {
		inputs[i] = models.DecisionInput{
			ID:         in.ID,
			Label:      in.Label,
			Expression: in.Expression,
			Type:       in.Type,
		}
	}
	outputs := make([]models.DecisionOutput, len(a.Decision.Outputs))
	for i, out := range a.Decision.Outputs {
		outputs[i] = models.DecisionOutput{
			ID:     out.ID,
			Label:  out.Label,
			Name:   out.Name,
			Type:   out.Type,
			Values: out.Values,
		}
	}
	rules := make([]models.DecisionRule, len(a.Decision.Rules))
	for i, r := range a.Decision.Rules {
		rules[i] = models.DecisionRule{
			ID:          r.ID,
			Inputs:      r.Inputs,
			Outputs:     r.Outputs,
			Description: r.Description,
		}
	}
	return models.DecisionDefinitionModel{
		Base: models.Base{
			ID:        models.UUID(a.Decision.ID),
			CreatedAt: a.Decision.CreatedAt,
		},
		ProjectID:         models.UUID(projectID),
		Key:               a.Decision.Key,
		Name:              a.Decision.Name,
		Version:           a.Decision.Version,
		HitPolicy:         a.Decision.HitPolicy,
		Aggregation:       a.Decision.Aggregation,
		RequiredDecisions: a.Decision.RequiredDecisions,
		Inputs:            inputs,
		Outputs:           outputs,
		Rules:             rules,
		Tests:             testsToModel(a.Decision.Tests),
	}
}

type DecisionEntityAdapter struct {
	Model models.DecisionDefinitionModel
}

func (a DecisionEntityAdapter) ToEntity() entities.DecisionDefinition {
	inputs := make([]entities.DecisionInput, len(a.Model.Inputs))
	for i, in := range a.Model.Inputs {
		inputs[i] = entities.DecisionInput{
			ID:         in.ID,
			Label:      in.Label,
			Expression: in.Expression,
			Type:       in.Type,
		}
	}
	outputs := make([]entities.DecisionOutput, len(a.Model.Outputs))
	for i, out := range a.Model.Outputs {
		outputs[i] = entities.DecisionOutput{
			ID:     out.ID,
			Label:  out.Label,
			Name:   out.Name,
			Type:   out.Type,
			Values: out.Values,
		}
	}
	rules := make([]entities.DecisionRule, len(a.Model.Rules))
	for i, r := range a.Model.Rules {
		rules[i] = entities.DecisionRule{
			ID:          r.ID,
			Inputs:      r.Inputs,
			Outputs:     r.Outputs,
			Description: r.Description,
		}
	}
	return entities.DecisionDefinition{
		ID:                uuid.UUID(a.Model.ID),
		Project:           &entities.Project{ID: uuid.UUID(a.Model.ProjectID)},
		Key:               a.Model.Key,
		Name:              a.Model.Name,
		Version:           a.Model.Version,
		HitPolicy:         a.Model.HitPolicy,
		Aggregation:       a.Model.Aggregation,
		RequiredDecisions: a.Model.RequiredDecisions,
		Inputs:            inputs,
		Outputs:           outputs,
		Rules:             rules,
		Tests:             testsToEntity(a.Model.Tests),
		CreatedAt:         a.Model.CreatedAt,
	}
}

// testsToModel and testsToEntity carry a table's examples across the boundary.
//
// Written out rather than shared through a common type: the two layers are
// allowed to diverge, and the last field that crossed by being the same struct
// — an output's ranking list — was silently dropped for exactly as long as
// nobody looked.
func testsToModel(in []entities.DecisionTest) []models.DecisionTest {
	if len(in) == 0 {
		return nil
	}
	out := make([]models.DecisionTest, len(in))
	for i, t := range in {
		out[i] = models.DecisionTest{ID: t.ID, Name: t.Name, Inputs: t.Inputs, Expected: t.Expected}
	}
	return out
}

func testsToEntity(in []models.DecisionTest) []entities.DecisionTest {
	if len(in) == 0 {
		return nil
	}
	out := make([]entities.DecisionTest, len(in))
	for i, t := range in {
		out[i] = entities.DecisionTest{ID: t.ID, Name: t.Name, Inputs: t.Inputs, Expected: t.Expected}
	}
	return out
}
