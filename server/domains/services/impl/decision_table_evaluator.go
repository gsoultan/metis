package impl

import (
	"context"
	"fmt"
	"sort"

	"github.com/gsoultan/gobpm/server/domains/entities"
	"github.com/gsoultan/gobpm/server/domains/services/contracts"
)

// DecisionTableEvaluatorImpl implements DecisionTableEvaluator by applying the
// table's hit policy against each rule using the injected ExpressionEvaluator Strategy.
// Supports UNIQUE, FIRST, COLLECT, ANY, and PRIORITY hit policies.
type DecisionTableEvaluatorImpl struct {
	expr contracts.ExpressionEvaluator
}

// NewDecisionTableEvaluator creates a new DecisionTableEvaluatorImpl.
func NewDecisionTableEvaluator(expr contracts.ExpressionEvaluator) contracts.DecisionTableEvaluator {
	return &DecisionTableEvaluatorImpl{expr: expr}
}

// EvaluateTable evaluates the decision table against the given variables.
func (e *DecisionTableEvaluatorImpl) EvaluateTable(ctx context.Context, def entities.DecisionDefinition, variables map[string]any) (entities.DecisionResult, error) {
	matched, err := e.collectMatchingRules(ctx, def, variables)
	if err != nil {
		return entities.DecisionResult{}, err
	}
	return e.applyHitPolicy(def, matched)
}

// matchedRule is a rule that applied, with its position in the table so the
// result can say which line the answer came from.
type matchedRule struct {
	index int
	rule  entities.DecisionRule
}

// collectMatchingRules returns all rules whose input conditions match the variables.
func (e *DecisionTableEvaluatorImpl) collectMatchingRules(ctx context.Context, def entities.DecisionDefinition, variables map[string]any) ([]matchedRule, error) {
	var matched []matchedRule
	for i, rule := range def.Rules {
		ok, err := e.ruleMatches(ctx, def, rule, variables)
		if err != nil {
			return nil, err
		}
		if ok {
			matched = append(matched, matchedRule{index: i, rule: rule})
		}
	}
	return matched, nil
}

// ruleMatches returns true if all input conditions of the rule match the variables.
func (e *DecisionTableEvaluatorImpl) ruleMatches(ctx context.Context, def entities.DecisionDefinition, rule entities.DecisionRule, variables map[string]any) (bool, error) {
	for i, inputExpr := range rule.Inputs {
		if i >= len(def.Inputs) {
			break
		}
		inputDef := def.Inputs[i]
		inputVal := variables[inputDef.Expression]
		vars := map[string]any{"_input": inputVal}
		ok, err := e.expr.EvaluateBool(ctx, inputExpr, vars)
		if err != nil || !ok {
			return false, err
		}
	}
	return true, nil
}

// applyHitPolicy applies the table's hit policy to the matched lines.
//
// The set is now complete. PRIORITY and OUTPUT ORDER used to fall through to
// "take the first line", and RULE ORDER was not recognised at all — so a table
// exported from another tool using any of them answered with one value where
// its author had asked for the most important one, or for all of them.
func (e *DecisionTableEvaluatorImpl) applyHitPolicy(def entities.DecisionDefinition, matched []matchedRule) (entities.DecisionResult, error) {
	if len(matched) == 0 {
		// No line matched. An empty result rather than an error: a table is
		// allowed not to have an opinion, and the caller can tell the
		// difference by the empty MatchedRules.
		return entities.DecisionResult{Values: map[string]any{}}, nil
	}

	switch def.HitPolicy {
	case entities.HitPolicyFirst:
		return e.buildResult(def, matched[:1]), nil

	case entities.HitPolicyUnique:
		if len(matched) > 1 {
			return entities.DecisionResult{}, fmt.Errorf(
				"UNIQUE hit policy violated: lines %v all matched, but UNIQUE promises exactly one",
				ruleNumbers(matched))
		}
		return e.buildResult(def, matched), nil

	case entities.HitPolicyAny:
		// ANY means the author asserts every matching line agrees. When they
		// disagree the table contradicts itself, and picking one silently is
		// how a decision table starts lying about what it decided.
		if err := e.assertOutputsAgree(def, matched); err != nil {
			return entities.DecisionResult{}, err
		}
		return e.buildResult(def, matched[:1]), nil

	case entities.HitPolicyPriority:
		ordered, err := e.orderByOutputPriority(def, matched)
		if err != nil {
			return entities.DecisionResult{}, err
		}
		return e.buildResult(def, ordered[:1]), nil

	case entities.HitPolicyOutputOrder:
		ordered, err := e.orderByOutputPriority(def, matched)
		if err != nil {
			return entities.DecisionResult{}, err
		}
		return e.collectAll(def, ordered), nil

	case entities.HitPolicyRuleOrder:
		return e.collectAll(def, matched), nil

	case entities.HitPolicyCollect:
		return e.applyCollect(def, matched), nil

	case "":
		// An unset hit policy is UNIQUE per the specification, but tables in
		// the wild leave it blank and rely on first-match. Treating it as FIRST
		// keeps those working; treating it as UNIQUE would turn them into
		// errors on the first row that overlaps.
		return e.buildResult(def, matched[:1]), nil

	default:
		return entities.DecisionResult{}, fmt.Errorf(
			"unknown hit policy %q: expected UNIQUE, FIRST, PRIORITY, ANY, COLLECT, OUTPUT ORDER or RULE ORDER",
			def.HitPolicy)
	}
}

// ruleNumbers renders matched lines as the 1-based numbers an author sees in
// the editor, so an error names the rows rather than array offsets.
func ruleNumbers(matched []matchedRule) []int {
	numbers := make([]int, len(matched))
	for i, m := range matched {
		numbers[i] = m.index + 1
	}
	return numbers
}

// assertOutputsAgree checks that every matched line produces the same outputs.
func (e *DecisionTableEvaluatorImpl) assertOutputsAgree(def entities.DecisionDefinition, matched []matchedRule) error {
	first := matched[0]
	for _, m := range matched[1:] {
		for i := range def.Outputs {
			if outputAt(first.rule, i) != outputAt(m.rule, i) {
				return fmt.Errorf(
					"ANY hit policy violated: lines %d and %d both matched but disagree on %q (%v vs %v)",
					first.index+1, m.index+1, def.Outputs[i].Name, outputAt(first.rule, i), outputAt(m.rule, i))
			}
		}
	}
	return nil
}

// orderByOutputPriority sorts matched lines by their first output's position in
// that column's declared value list, most important first.
//
// The ordering is stable, so lines of equal priority keep their table order —
// which is what makes OUTPUT ORDER deterministic rather than merely sorted.
func (e *DecisionTableEvaluatorImpl) orderByOutputPriority(def entities.DecisionDefinition, matched []matchedRule) ([]matchedRule, error) {
	if len(def.Outputs) == 0 {
		return matched, nil
	}
	priorities := def.Outputs[0].Values
	if len(priorities) == 0 {
		return nil, fmt.Errorf(
			"hit policy %s needs the output column %q to list its values in priority order; without that list there is nothing to rank by",
			def.HitPolicy, def.Outputs[0].Name)
	}

	rank := make(map[string]int, len(priorities))
	for i, value := range priorities {
		rank[value] = i
	}

	ordered := append([]matchedRule(nil), matched...)
	sort.SliceStable(ordered, func(a, b int) bool {
		return priorityOf(ordered[a], rank) < priorityOf(ordered[b], rank)
	})
	return ordered, nil
}

// priorityOf ranks one line's output. A value missing from the declared list
// sorts last rather than failing the evaluation: the table still has an answer,
// and an unlisted value is an authoring gap to fix, not a reason to stall an
// instance mid-flight.
func priorityOf(m matchedRule, rank map[string]int) int {
	value := fmt.Sprintf("%v", outputAt(m.rule, 0))
	if position, ok := rank[value]; ok {
		return position
	}
	return len(rank)
}

// outputAt returns a line's output for a column, or nil when the line is short.
func outputAt(rule entities.DecisionRule, i int) any {
	if i < len(rule.Outputs) {
		return rule.Outputs[i]
	}
	return nil
}

// collectAll returns every matched line's outputs as a list per column.
//
// This is what multi-hit means, and what buildResult could not express: it
// writes one value per column, so each line overwrote the one before and a
// policy that promised every match returned only the last.
func (e *DecisionTableEvaluatorImpl) collectAll(def entities.DecisionDefinition, matched []matchedRule) entities.DecisionResult {
	values := make(map[string]any, len(def.Outputs))
	for i, output := range def.Outputs {
		column := make([]any, 0, len(matched))
		for _, m := range matched {
			column = append(column, outputAt(m.rule, i))
		}
		values[output.Name] = column
	}
	return entities.DecisionResult{Values: values, MatchedRules: ruleIndexes(matched)}
}

func ruleIndexes(matched []matchedRule) []int {
	indexes := make([]int, len(matched))
	for i, m := range matched {
		indexes[i] = m.index
	}
	return indexes
}

// applyCollect aggregates all matched rule outputs according to the aggregation function.
func (e *DecisionTableEvaluatorImpl) applyCollect(def entities.DecisionDefinition, matched []matchedRule) entities.DecisionResult {
	if def.Aggregation == "" {
		// COLLECT with no aggregator is a list of every match. It used to run
		// through buildResult, which writes one value per column — so each
		// matching line overwrote the previous one and a table asking for all
		// of them received only the last.
		return e.collectAll(def, matched)
	}
	result := e.buildResult(def, matched)
	aggregated := make(map[string]any)
	for i, output := range def.Outputs {
		var nums []float64
		for _, m := range matched {
			if i < len(m.rule.Outputs) {
				if n, ok := numericOutput(m.rule.Outputs[i]); ok {
					nums = append(nums, n)
				}
			}
		}
		aggregated[output.Name] = aggregate(def.Aggregation, nums)
	}
	// An aggregate still comes from lines, and which ones is the explanation.
	return entities.DecisionResult{Values: aggregated, MatchedRules: result.MatchedRules}
}

// buildResult maps output values from the first (or all) matched rules into the result.
func (e *DecisionTableEvaluatorImpl) buildResult(def entities.DecisionDefinition, rules []matchedRule) entities.DecisionResult {
	values := make(map[string]any)
	indexes := make([]int, 0, len(rules))
	for _, m := range rules {
		indexes = append(indexes, m.index)
		for i, output := range def.Outputs {
			if i < len(m.rule.Outputs) {
				values[output.Name] = m.rule.Outputs[i]
			}
		}
	}
	return entities.DecisionResult{Values: values, MatchedRules: indexes}
}

// aggregate applies the aggregation function over a slice of numbers.
func aggregate(fn string, nums []float64) any {
	if len(nums) == 0 {
		return nil
	}
	switch fn {
	case entities.AggregationSum:
		var sum float64
		for _, n := range nums {
			sum += n
		}
		return sum
	case entities.AggregationCount:
		return float64(len(nums))
	case entities.AggregationMin:
		return slicesMin(nums)
	case entities.AggregationMax:
		return slicesMax(nums)
	}
	return nums
}

func slicesMin(nums []float64) float64 {
	m := nums[0]
	for _, n := range nums[1:] {
		if n < m {
			m = n
		}
	}
	return m
}

func slicesMax(nums []float64) float64 {
	m := nums[0]
	for _, n := range nums[1:] {
		if n > m {
			m = n
		}
	}
	return m
}

// numericOutput coerces a rule's output cell to a number for aggregation.
//
// Outputs are authored values that have been through JSON, so a figure may
// arrive as any numeric type. COLLECT with SUM over a column that is not
// numeric reports nothing rather than guessing, which is why this returns the
// ok flag instead of a zero.
func numericOutput(v any) (float64, bool) {
	switch n := v.(type) {
	case float64:
		return n, true
	case float32:
		return float64(n), true
	case int:
		return float64(n), true
	case int32:
		return float64(n), true
	case int64:
		return float64(n), true
	}
	return 0, false
}
