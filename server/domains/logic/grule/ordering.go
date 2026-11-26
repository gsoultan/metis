package grule

import (
	"fmt"
	"sort"
	"strings"
)

// ordering decides which rules can be translated together and in what order:
// a rule that reads a field another rule sets is decided after it.
//
// Grule chains rules: one rule's action changes a fact, and on the next cycle
// another rule's condition sees the change. A process chains decisions the
// same way, one step after another, as long as the chain has no loop and
// each field is set in one step only. Rules that break either, and rules that
// read what a rule that could not be translated would have set, are dropped
// with a reason, repeatedly, until what is left is consistent.
type ordering struct {
	active  []*analysedRule
	skipped map[string]string // rule name → why
	writes  map[string][]string
	layer   map[string]int
}

func newOrdering(active []*analysedRule, skipped map[string]string, writes map[string][]string) *ordering {
	return &ordering{active: active, skipped: skipped, writes: writes}
}

// settle drops rules until none of the checks drops another.
func (o *ordering) settle() {
	for {
		before := len(o.active)
		o.dropReadersOfSkipped()
		o.dropLoops()
		o.assignLayers()
		o.dropFieldsSetInTwoSteps()
		if len(o.active) == before {
			return
		}
	}
}

func (o *ordering) drop(rule *analysedRule, reason string) {
	o.skipped[rule.Name] = reason
}

// removeSkipped takes the rules dropped so far out of the active ones.
func (o *ordering) removeSkipped() {
	kept := o.active[:0]
	for _, r := range o.active {
		if _, gone := o.skipped[r.Name]; !gone {
			kept = append(kept, r)
		}
	}
	o.active = kept
}

// dropReadersOfSkipped drops a rule that reads a field a dropped rule sets:
// in Grule it would see that rule's answer, and in metis nothing sets it.
func (o *ordering) dropReadersOfSkipped() {
	setBy := map[string]string{}
	for name := range o.skipped {
		for _, key := range o.writes[name] {
			setBy[key] = name
		}
	}
	for _, r := range o.active {
		for _, key := range sortedKeys(r.reads) {
			if writer, ok := setBy[key]; ok {
				o.drop(r, fmt.Sprintf("it reads %s, which %s sets, and %s could not be imported", key, writer, writer))
				break
			}
		}
	}
	o.removeSkipped()
}

// dependsOn lists the rules r must be decided after: every other rule that
// sets a field r reads. A field r sets itself is not a dependency: rules that
// set the same field are decided together, in one table, and groupComponent
// says whether that table can give Grule's answer.
func (o *ordering) dependsOn(r *analysedRule) []*analysedRule {
	var deps []*analysedRule
	for _, w := range o.active {
		if w == r {
			continue
		}
		for _, key := range w.writes {
			if r.reads[key] && !r.writesField(key) {
				deps = append(deps, w)
				break
			}
		}
	}
	return deps
}

// dropLoops drops every rule on a loop of dependencies: A sets what B reads
// and B sets what A reads. That is inference, and one pass cannot do it.
func (o *ordering) dropLoops() {
	for _, cycle := range o.cycles() {
		names := make([]string, len(cycle))
		for i, r := range cycle {
			names[i] = r.Name
		}
		for _, r := range cycle {
			o.drop(r, "it is part of a loop ("+strings.Join(names, " → ")+"), where each rule's answer changes another's condition")
		}
	}
	o.removeSkipped()
}

func sortedKeys(set map[string]bool) []string {
	keys := make([]string, 0, len(set))
	for key := range set {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}
