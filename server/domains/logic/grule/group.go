package grule

import (
	"fmt"
	"slices"
	"strings"
)

// group is the rules one decision table answers for: they set the same
// fields, in the same step, and the table's lines are in the order that gives
// Grule's answer under FIRST.
type group struct {
	layer  int
	fields []string
	rules  []*analysedRule
}

// firesBefore is Grule's order: higher salience first, and, between rules of
// equal salience, the order they were written in. Grule does not promise an
// order there, which the report says.
func firesBefore(a, b *analysedRule) int {
	if a.Salience != b.Salience {
		return b.Salience - a.Salience
	}
	return a.order - b.order
}

// components splits one step's rules into the sets that share a field.
func components(rules []*analysedRule) [][]*analysedRule {
	parent := map[*analysedRule]*analysedRule{}
	var find func(r *analysedRule) *analysedRule
	find = func(r *analysedRule) *analysedRule {
		if parent[r] == nil || parent[r] == r {
			return r
		}
		parent[r] = find(parent[r])
		return parent[r]
	}
	owner := map[string]*analysedRule{}
	for _, r := range rules {
		for _, key := range r.writes {
			if other, ok := owner[key]; ok {
				parent[find(r)] = find(other)
			} else {
				owner[key] = r
			}
		}
	}
	byRoot := map[*analysedRule][]*analysedRule{}
	var roots []*analysedRule
	for _, r := range rules {
		root := find(r)
		if _, seen := byRoot[root]; !seen {
			roots = append(roots, root)
		}
		byRoot[root] = append(byRoot[root], r)
	}
	out := make([][]*analysedRule, len(roots))
	for i, root := range roots {
		out[i] = byRoot[root]
	}
	return out
}

// groupComponent turns rules that share fields into tables, or says why they
// cannot be.
//
// Rules that each fire once by retracting themselves all fire when they
// match, so the last to fire sets each field: one table per set of fields
// with the same rules setting them, lines in reverse firing order, FIRST.
// Rules guarded by a field they set fire until one of them has set it: one
// table, lines in firing order, FIRST — provided every rule sets the same
// fields and each one's firing stops all the others.
func groupComponent(layer int, rules []*analysedRule) ([]group, string) {
	guarded := 0
	for _, r := range rules {
		if r.guarded {
			guarded++
		}
	}
	slices.SortStableFunc(rules, firesBefore)
	switch guarded {
	case 0:
		if reason := readsAnotherWriter(rules); reason != "" {
			return nil, reason
		}
		return retractingGroups(layer, rules), ""
	case len(rules):
		return guardedGroup(layer, rules)
	}
	return nil, "rules that set " + strings.Join(sharedFields(rules), ", ") +
		" mix ones that stop on a field they set with ones that only retract themselves, so the answer depends on Grule's firing order"
}

func retractingGroups(layer int, rules []*analysedRule) []group {
	byWriters := map[string]*group{}
	var order []string
	for _, field := range sharedFields(rules) {
		var writers []*analysedRule
		var names []string
		for _, r := range rules {
			if r.writesField(field) {
				writers = append(writers, r)
				names = append(names, r.Name)
			}
		}
		id := strings.Join(names, "\x00")
		if g, ok := byWriters[id]; ok {
			g.fields = append(g.fields, field)
			continue
		}
		slices.Reverse(writers)
		byWriters[id] = &group{layer: layer, fields: []string{field}, rules: writers}
		order = append(order, id)
	}
	groups := make([]group, len(order))
	for i, id := range order {
		groups[i] = *byWriters[id]
	}
	return groups
}

func guardedGroup(layer int, rules []*analysedRule) ([]group, string) {
	fields := rules[0].writes
	for _, r := range rules[1:] {
		if !sameFields(r.writes, fields) {
			return nil, fmt.Sprintf("%s sets %s and %s sets %s; rules that stop on a field they set are imported together only when they set the same fields",
				rules[0].Name, strings.Join(fields, ", "), r.Name, strings.Join(r.writes, ", "))
		}
	}
	for _, r := range rules {
		for _, w := range rules {
			if w != r && !w.stops(r) {
				return nil, fmt.Sprintf("after %s fires, %s can still fire, so the answer depends on Grule's firing order", w.Name, r.Name)
			}
		}
	}
	return []group{{layer: layer, fields: slices.Clone(fields), rules: rules}}, ""
}

// stops reports whether w firing makes one of r's guards false.
func (w *analysedRule) stops(r *analysedRule) bool {
	for _, part := range conjuncts(r.When) {
		if w.isGuard(part) {
			return true
		}
	}
	return false
}

// sharedFields lists every field the rules set, in the order first set.
func sharedFields(rules []*analysedRule) []string {
	seen := map[string]bool{}
	var fields []string
	for _, r := range rules {
		for _, key := range r.writes {
			if !seen[key] {
				seen[key] = true
				fields = append(fields, key)
			}
		}
	}
	return fields
}

func sameFields(a, b []string) bool {
	return len(a) == len(b) && !slices.ContainsFunc(a, func(key string) bool { return !slices.Contains(b, key) })
}

// readsAnotherWriter says why rules that only retract themselves cannot be one
// table when one of them reads a field another of them sets: what it reads
// then depends on whether the other fired first.
func readsAnotherWriter(rules []*analysedRule) string {
	for _, r := range rules {
		for _, w := range rules {
			if w == r {
				continue
			}
			for _, key := range w.writes {
				if r.reads[key] {
					return fmt.Sprintf("%s reads %s, which %s sets in the same pass, so what it reads depends on Grule's firing order", r.Name, key, w.Name)
				}
			}
		}
	}
	return ""
}
