package grule

import (
	"bytes"
	"encoding/json"
	"fmt"
	"math"
)

// jsonRule is one rule in Grule's JSON form.
type jsonRule struct {
	Name     string            `json:"name"`
	Desc     string            `json:"desc"`
	Salience float64           `json:"salience"`
	When     any               `json:"when"`
	Then     []json.RawMessage `json:"then"`
}

// jsonOperators are the condition operators of Grule's JSON form and the GRL
// operator each stands for. "not" is GRL's `!=`, as Grule defines it.
var jsonOperators = map[string]string{
	"and": "&&", "or": "||", "eq": "==", "not": "!=",
	"gt": ">", "gte": ">=", "lt": "<", "lte": "<=",
	"bor": "|", "band": "&", "plus": "+", "minus": "-",
	"div": "/", "mul": "*", "mod": "%",
}

// ParseJSON parses rules in Grule's JSON form: one rule object, or a list of
// them. name is the file's name, used only in positions and messages.
func ParseJSON(name string, source []byte) ([]Rule, error) {
	if len(source) > MaxSourceBytes {
		return nil, fmt.Errorf("%s is %d bytes; a rule file may be at most %d", name, len(source), MaxSourceBytes)
	}
	raw, err := decodeRuleList(source)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", name, err)
	}
	if len(raw) > MaxRules {
		return nil, fmt.Errorf("%s holds more than %d rules", name, MaxRules)
	}
	rules := make([]Rule, 0, len(raw))
	for i, r := range raw {
		rule, err := r.toRule()
		if err != nil {
			return nil, fmt.Errorf("%s: rule %d (%s): %w", name, i+1, r.Name, err)
		}
		rule.Position = fmt.Sprintf("%s#%d", name, i+1)
		rules = append(rules, rule)
	}
	return rules, nil
}

func decodeRuleList(source []byte) ([]jsonRule, error) {
	trimmed := bytes.TrimSpace(source)
	decoder := json.NewDecoder(bytes.NewReader(trimmed))
	decoder.UseNumber()
	if len(trimmed) > 0 && trimmed[0] == '{' {
		var one jsonRule
		if err := decoder.Decode(&one); err != nil {
			return nil, fmt.Errorf("not a Grule JSON rule: %w", err)
		}
		return []jsonRule{one}, nil
	}
	var many []jsonRule
	if err := decoder.Decode(&many); err != nil {
		return nil, fmt.Errorf("not a list of Grule JSON rules: %w", err)
	}
	return many, nil
}

func (r jsonRule) toRule() (Rule, error) {
	if r.Name == "" {
		return Rule{}, fmt.Errorf("a rule needs a name")
	}
	if r.Salience != math.Trunc(r.Salience) || math.Abs(r.Salience) > math.MaxInt32 {
		return Rule{}, fmt.Errorf("salience %v is not a whole number", r.Salience)
	}
	if len(r.Then) > MaxStatements {
		return Rule{}, fmt.Errorf("more than %d actions in one rule", MaxStatements)
	}
	when, err := jsonExpr(r.When, 0)
	if err != nil {
		return Rule{}, fmt.Errorf("when: %w", err)
	}
	rule := Rule{Name: r.Name, Description: r.Desc, Salience: int(r.Salience), When: when}
	for i, raw := range r.Then {
		statement, err := jsonStatement(raw)
		if err != nil {
			return Rule{}, fmt.Errorf("then, action %d: %w", i+1, err)
		}
		rule.Then = append(rule.Then, statement)
	}
	return rule, nil
}
