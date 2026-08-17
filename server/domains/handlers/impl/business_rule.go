package impl

import (
	"context"
	"fmt"
	"github.com/gsoultan/gobpm/server/domains/logic/feel"
	"github.com/rs/zerolog/log"

	"github.com/gsoultan/gobpm/server/domains/entities"
	"github.com/gsoultan/gobpm/server/domains/services/contracts"
)

type BusinessRuleTaskHandler struct {
	engine          contracts.EngineRunner
	decisionService contracts.DecisionService
}

func (h *BusinessRuleTaskHandler) DoExecute(ctx context.Context, instance *entities.ProcessInstance, def *entities.ProcessDefinition, node entities.Node, iterationID string) error {
	decisionKey := node.GetStringProperty("decision_key")
	if decisionKey == "" {
		// If no decision key, treat as pass-through
		return h.engine.ProceedIteration(ctx, instance, def, node.ID, iterationID)
	}

	decisionVersion := 0
	if v, ok := node.Properties["decision_version"].(float64); ok {
		decisionVersion = int(v)
	} else if v, ok := node.Properties["decision_version"].(int); ok {
		decisionVersion = v
	}

	// Prepare inputs based on input mapping
	inputs := instance.Variables
	if mapping, ok := node.Properties["input_mapping"].(map[string]any); ok && len(mapping) > 0 {
		inputs = resolveMapping(mapping, instance.Variables)
	}

	result, err := h.decisionService.Evaluate(ctx, decisionKey, decisionVersion, inputs)
	if err != nil {
		return fmt.Errorf("decision evaluation failed for node %s: %w", node.ID, err)
	}

	// Apply decision results to process variables based on output mapping
	if mapping, ok := node.Properties["output_mapping"].(map[string]any); ok {
		for target, value := range resolveMapping(mapping, result.Values) {
			instance.SetVariable(target, value)
		}
	} else {
		// Default: apply all results to process variables
		for k, v := range result.Values {
			instance.SetVariable(k, v)
		}
	}

	return h.engine.ProceedIteration(ctx, instance, def, node.ID, iterationID)
}

// resolveMapping turns a mapping of target name → source into concrete values.
//
// A source was previously a variable name and nothing else, so a mapping could
// rename a value and no more: computing `total` from `price` and `quantity`
// meant adding a script task beside the decision purely to do the arithmetic.
// A source is now a FEEL expression, which subsumes the old behaviour — a bare
// name is still a variable reference — and adds everything else:
// `price * quantity`, `applicant.address.city`, `sum(items.price)`.
//
// A source that resolves to nothing is omitted rather than written as null,
// which is what the name-only version did: mapping an absent variable left the
// target unset instead of setting it to nothing.
func resolveMapping(mapping map[string]any, source map[string]any) map[string]any {
	out := make(map[string]any, len(mapping))
	for target, expression := range mapping {
		text, isText := expression.(string)
		if !isText {
			// A non-string source is a constant the author wrote into the
			// mapping; pass it through untouched.
			out[target] = expression
			continue
		}

		// The plain-name case first: it is the common one, it cannot fail, and
		// it keeps a variable whose name is also a FEEL keyword working.
		if value, ok := source[text]; ok {
			out[target] = value
			continue
		}

		value, err := feel.Evaluate(text, source)
		if err != nil {
			log.Warn().
				Err(err).
				Str("target", target).
				Str("expression", text).
				Msg("Mapping expression could not be evaluated; the target is left unset")
			continue
		}
		if value.IsNull() {
			continue
		}
		out[target] = value.ToAny()
	}
	return out
}
