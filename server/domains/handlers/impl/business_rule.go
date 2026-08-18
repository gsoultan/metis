package impl

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/rs/zerolog/log"

	"github.com/gsoultan/gobpm/server/domains/entities"
	"github.com/gsoultan/gobpm/server/domains/logic/feel"
	"github.com/gsoultan/gobpm/server/domains/services/contracts"
)

// EventDecisionEvaluated is the timeline entry for a decision the process made.
//
// It sits here rather than beside the other event names in the audit writer
// because it is the handler that produces it, and the writer's narrative
// function falls through to the message this one already carries.
const EventDecisionEvaluated = "decision_evaluated"

type BusinessRuleTaskHandler struct {
	engine          contracts.EngineRunner
	decisionService contracts.DecisionService

	// auditWriter records what was decided and on what grounds.
	//
	// A decision table is a business policy, versioned and immutable, and the
	// question asked about one months later is not "what did it output" but
	// "which version was in force, and which line applied". Without this the
	// timeline shows a variable changing value with nothing to explain it.
	auditWriter contracts.AuditWriter
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

	h.recordEvaluation(ctx, instance, node, inputs, result)

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

// recordEvaluation writes the decision to the instance's timeline.
//
// It records the four things that reconstruct the reasoning: which table, which
// version of it, which lines applied, and what went in. The table is immutable
// once deployed, so those four are enough to replay the decision exactly as it
// was made — which is what a compliance question needs and what an output value
// on its own cannot give.
//
// A failure here is logged and not returned. The decision has been made; failing
// the node because the note about it could not be written would turn a
// bookkeeping problem into a stalled business process, and the retry would
// evaluate the table a second time.
func (h *BusinessRuleTaskHandler) recordEvaluation(
	ctx context.Context,
	instance *entities.ProcessInstance,
	node entities.Node,
	inputs map[string]any,
	result entities.DecisionResult,
) {
	if h.auditWriter == nil {
		return
	}

	nodeCopy := node
	entry := entities.AuditEntry{
		Instance:  &entities.ProcessInstance{ID: instance.ID},
		Node:      &nodeCopy,
		Type:      EventDecisionEvaluated,
		Message:   decisionMessage(result),
		Timestamp: time.Now(),
		Data: map[string]any{
			"decision_key":     result.DecisionKey,
			"decision_name":    result.DecisionName,
			"decision_version": result.DecisionVersion,
			"matched_rules":    result.MatchedRules,
			"matched_rule_ids": result.MatchedRuleIDs,
			"inputs":           inputs,
			"outputs":          result.Values,
		},
	}
	if instance.Project != nil {
		entry.Project = &entities.Project{ID: instance.Project.ID}
	}

	if err := h.auditWriter.RecordEvent(ctx, entry); err != nil {
		log.Error().Err(err).
			Str("instance", instance.ID.String()).
			Str("node", node.ID).
			Str("decision", result.DecisionKey).
			Msg("Could not record a decision evaluation; the decision itself stands")
	}
}

// decisionMessage is the line an operator reads on the timeline.
func decisionMessage(result entities.DecisionResult) string {
	name := result.DecisionName
	if name == "" {
		name = result.DecisionKey
	}
	if len(result.MatchedRules) == 0 {
		return fmt.Sprintf("%s (v%d) matched no rule", name, result.DecisionVersion)
	}
	lines := make([]string, 0, len(result.MatchedRules))
	for _, index := range result.MatchedRules {
		// Numbered as the author sees them in the editor, not as the slice
		// indexes them.
		lines = append(lines, strconv.Itoa(index+1))
	}
	return fmt.Sprintf("%s (v%d) applied rule %s", name, result.DecisionVersion, strings.Join(lines, ", "))
}
