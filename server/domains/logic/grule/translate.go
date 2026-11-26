package grule

import (
	"fmt"
	"regexp"
	"slices"
	"sort"

	"github.com/gsoultan/metis/server/domains/entities"
)

// keyPrefix is what an import's decisions and process are named by.
var keyPrefix = regexp.MustCompile(`^[a-z][a-z0-9-]{0,39}$`)

// Translate turns parsed rules into decision tables and the process that
// consults them, and says what became of every rule. prefix names the
// decisions (`pricing-applicant-discount`) and the process (`pricing`).
//
// It is pure: nothing is stored, nothing is evaluated.
func Translate(rules []Rule, prefix, name string) (entities.GruleImport, error) {
	if !keyPrefix.MatchString(prefix) {
		return entities.GruleImport{}, fmt.Errorf("the key %q must start with a lower-case letter and hold only lower-case letters, digits and dashes, at most 40", prefix)
	}
	if duplicate := duplicateName(rules); duplicate != "" {
		return entities.GruleImport{}, fmt.Errorf("two rules are named %s; Grule refuses that too", duplicate)
	}
	t := newTranslation(rules)
	groups := t.ordering.plan()
	t.markWritten(groups)
	result := entities.GruleImport{Results: map[string]string{}}
	taken := map[string]bool{}
	var steps []step
	for _, g := range groups {
		table := buildTable(g, decisionKey(prefix, g.fields, taken))
		result.Decisions = append(result.Decisions, table)
		steps = append(steps, t.stepFor(g, table))
		for _, field := range g.fields {
			result.Results[field] = variableName(field)
		}
		for _, r := range g.rules {
			t.decided[r.Name] = table.Key
		}
	}
	if len(groups) > 0 {
		result.Process = buildProcess(prefix, name, steps)
	}
	result.Rules = t.outcomes()
	result.Warnings = t.warnings(groups)
	return result, nil
}

func duplicateName(rules []Rule) string {
	seen := map[string]bool{}
	for _, r := range rules {
		if seen[r.Name] {
			return r.Name
		}
		seen[r.Name] = true
	}
	return ""
}

// translation is one Translate call's working state.
type translation struct {
	rules    []Rule
	analysed map[string]*analysedRule
	ordering *ordering
	decided  map[string]string // rule name → decision key
	written  map[string]bool   // every field an imported rule sets
}

func newTranslation(rules []Rule) *translation {
	t := &translation{rules: rules, analysed: map[string]*analysedRule{}, decided: map[string]string{}, written: map[string]bool{}}
	skipped := map[string]string{}
	writes := map[string][]string{}
	var active []*analysedRule
	for i, rule := range rules {
		writes[rule.Name] = assignedFields(rule)
		r, reason := analyse(rule, i)
		if reason != "" {
			skipped[rule.Name] = reason
			continue
		}
		t.analysed[rule.Name] = r
		active = append(active, r)
	}
	t.ordering = newOrdering(active, skipped, writes)
	return t
}

// assignedFields is every field a rule's actions set, read from the actions
// themselves so that it is known for a rule that could not be analysed too.
func assignedFields(rule Rule) []string {
	var keys []string
	for _, statement := range rule.Then {
		if a, ok := statement.(Assign); ok {
			if p, isPath := a.Target.(Path); isPath {
				keys = append(keys, fieldKey(p))
			}
		}
	}
	return keys
}

func (t *translation) outcomes() []entities.GruleRuleOutcome {
	out := make([]entities.GruleRuleOutcome, 0, len(t.rules))
	for _, rule := range t.rules {
		outcome := entities.GruleRuleOutcome{Name: rule.Name, Position: rule.Position}
		if key, ok := t.decided[rule.Name]; ok {
			outcome.Decision = key
			outcome.Notes = t.notesFor(rule.Name)
		} else {
			outcome.Reason = t.ordering.skipped[rule.Name]
		}
		out = append(out, outcome)
	}
	return out
}

// notesFor is what the report says about an imported rule: its own notes,
// and the fields it sets that a rule left out also sets.
func (t *translation) notesFor(name string) []string {
	r := t.analysed[name]
	notes := slices.Clone(r.notes)
	for _, other := range sortedSkipped(t.ordering.skipped) {
		for _, key := range t.ordering.writes[other] {
			if r.writesField(key) {
				notes = append(notes, fmt.Sprintf("%s also sets %s and was not imported; where %s would apply, metis answers differently from Grule", other, key, other))
				break
			}
		}
	}
	return notes
}

func sortedSkipped(skipped map[string]string) []string {
	names := make([]string, 0, len(skipped))
	for name := range skipped {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}
