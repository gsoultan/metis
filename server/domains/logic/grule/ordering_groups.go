package grule

import "sort"

// byLayer lists the active rules of each step, steps in order.
func (o *ordering) byLayer() [][]*analysedRule {
	steps := map[int][]*analysedRule{}
	for _, r := range o.active {
		steps[o.layer[r.Name]] = append(steps[o.layer[r.Name]], r)
	}
	layers := make([]int, 0, len(steps))
	for layer := range steps {
		layers = append(layers, layer)
	}
	sort.Ints(layers)
	out := make([][]*analysedRule, len(layers))
	for i, layer := range layers {
		out[i] = steps[layer]
	}
	return out
}

// groups makes the tables, dropping the rules no table can answer for. It
// reports whether it dropped any, since a dropped rule can leave another
// reading a field nothing sets.
func (o *ordering) groups() ([]group, bool) {
	var groups []group
	dropped := false
	for _, rules := range o.byLayer() {
		for _, component := range components(rules) {
			made, reason := groupComponent(o.layer[component[0].Name], component)
			if reason != "" {
				for _, r := range component {
					o.drop(r, reason)
				}
				dropped = true
				continue
			}
			groups = append(groups, made...)
		}
	}
	o.removeSkipped()
	return groups, dropped
}

// plan settles the rules and makes the tables, until making the tables drops
// nothing more.
func (o *ordering) plan() []group {
	for {
		o.settle()
		groups, dropped := o.groups()
		if !dropped {
			return groups
		}
	}
}
