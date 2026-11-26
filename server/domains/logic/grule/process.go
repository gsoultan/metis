package grule

import (
	"fmt"

	"github.com/gsoultan/metis/server/domains/entities"
)

// Where the imported process's steps are drawn: in a row, left to right.
const (
	stepSpacing = 220
	rowY        = 200
	firstX      = 100
)

// buildProcess is start → one business rule task per table, in step order →
// end. The tables of one step read nothing the others set, so their order
// among themselves does not matter.
func buildProcess(key, name string, steps []step) *entities.ProcessDefinition {
	if name == "" {
		name = key
	}
	def := &entities.ProcessDefinition{Key: key, Name: name,
		Documentation: "Imported from Grule rules. Each step is one decision table; see each table's lines for the rule it came from."}
	previous := &entities.Node{ID: "start", Name: "Start", Type: entities.StartEvent, X: firstX, Y: rowY}
	def.Nodes = append(def.Nodes, previous)
	for i, s := range steps {
		node := &entities.Node{
			ID: fmt.Sprintf("decide_%d", i+1), Name: s.decision.Name, Type: entities.BusinessRuleTask,
			X: firstX + (i+1)*stepSpacing, Y: rowY,
			Properties: map[string]any{"decision_key": s.decision.Key, "input_mapping": s.inputMapping},
		}
		def.Nodes = append(def.Nodes, node)
		def.Flows = append(def.Flows, flow(len(def.Flows)+1, previous, node))
		previous = node
	}
	end := &entities.Node{ID: "end", Name: "End", Type: entities.EndEvent, X: firstX + (len(steps)+1)*stepSpacing, Y: rowY}
	def.Nodes = append(def.Nodes, end)
	def.Flows = append(def.Flows, flow(len(def.Flows)+1, previous, end))
	return def
}

func flow(n int, from, to *entities.Node) *entities.SequenceFlow {
	id := fmt.Sprintf("flow_%d", n)
	from.Outgoing = append(from.Outgoing, id)
	to.Incoming = append(to.Incoming, id)
	return &entities.SequenceFlow{ID: id, SourceRef: from.ID, TargetRef: to.ID}
}
