package grule

import "fmt"

// cycles finds the strongly connected groups of more than one rule, by
// Tarjan's algorithm over the dependency graph.
func (o *ordering) cycles() [][]*analysedRule {
	t := &tarjan{o: o, index: map[*analysedRule]int{}, low: map[*analysedRule]int{}, onStack: map[*analysedRule]bool{}}
	for _, r := range o.active {
		if _, seen := t.index[r]; !seen {
			t.visit(r)
		}
	}
	return t.groups
}

type tarjan struct {
	o       *ordering
	next    int
	index   map[*analysedRule]int
	low     map[*analysedRule]int
	onStack map[*analysedRule]bool
	stack   []*analysedRule
	groups  [][]*analysedRule
}

func (t *tarjan) visit(r *analysedRule) {
	t.index[r], t.low[r] = t.next, t.next
	t.next++
	t.stack = append(t.stack, r)
	t.onStack[r] = true
	for _, dep := range t.o.dependsOn(r) {
		if _, seen := t.index[dep]; !seen {
			t.visit(dep)
			t.low[r] = min(t.low[r], t.low[dep])
		} else if t.onStack[dep] {
			t.low[r] = min(t.low[r], t.index[dep])
		}
	}
	if t.low[r] != t.index[r] {
		return
	}
	var group []*analysedRule
	for {
		top := t.stack[len(t.stack)-1]
		t.stack = t.stack[:len(t.stack)-1]
		t.onStack[top] = false
		group = append(group, top)
		if top == r {
			break
		}
	}
	if len(group) > 1 {
		t.groups = append(t.groups, group)
	}
}

// assignLayers numbers each rule's step: one after the latest step of the
// rules it depends on. The graph has no loops by now, so this ends.
func (o *ordering) assignLayers() {
	o.layer = map[string]int{}
	var depth func(r *analysedRule) int
	depth = func(r *analysedRule) int {
		if d, ok := o.layer[r.Name]; ok {
			return d
		}
		d := 0
		for _, dep := range o.dependsOn(r) {
			d = max(d, depth(dep)+1)
		}
		o.layer[r.Name] = d
		return d
	}
	for _, r := range o.active {
		depth(r)
	}
}

// dropFieldsSetInTwoSteps drops the later rules setting a field an earlier
// step already sets. Which answer Grule keeps depends on the order it happens
// to fire them in, and a decision has to say one thing.
func (o *ordering) dropFieldsSetInTwoSteps() {
	first := map[string]*analysedRule{}
	for _, r := range o.active {
		for _, key := range r.writes {
			if w, ok := first[key]; !ok || o.layer[r.Name] < o.layer[w.Name] {
				first[key] = r
			}
		}
	}
	for _, r := range o.active {
		for _, key := range r.writes {
			if w := first[key]; o.layer[r.Name] > o.layer[w.Name] {
				o.drop(r, fmt.Sprintf("it sets %s after reading what other rules set, and %s sets %s first; which answer Grule keeps depends on its firing order", key, w.Name, key))
				break
			}
		}
	}
	o.removeSkipped()
}
