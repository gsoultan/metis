package grule

import (
	"github.com/gsoultan/metis/server/domains/entities"
)

// step is one business rule task of the imported process: the table it
// consults and where each of the table's variables comes from.
type step struct {
	decision     entities.DecisionDefinition
	inputMapping map[string]any
}

// stepFor maps every variable a group's table reads.
//
// A field no imported rule sets is read from the fact as the process was
// started with it: `Applicant.Age`. A field one does set is read from the
// variable an earlier step wrote it to, and from the fact while no step has:
// in Grule a rule sees a field's new value once another rule has set it, and
// its old one until then.
func (t *translation) stepFor(g group, table entities.DecisionDefinition) step {
	mapping := map[string]any{}
	for _, r := range g.rules {
		for key := range r.reads {
			mapping[variableName(key)] = t.source(key)
		}
	}
	for _, input := range table.Inputs {
		if input.Expression == conditionColumn {
			mapping[conditionColumn] = true
		}
	}
	return step{decision: table, inputMapping: mapping}
}

func (t *translation) source(key string) string {
	if !t.written[key] {
		return key
	}
	name := variableName(key)
	return "if " + name + " != null then " + name + " else " + key
}

// markWritten records every field the imported tables set.
func (t *translation) markWritten(groups []group) {
	for _, g := range groups {
		for _, field := range g.fields {
			t.written[field] = true
		}
	}
}
