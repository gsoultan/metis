package impl

import (
	"github.com/gsoultan/metis/server/domains/entities"
	"github.com/gsoultan/metis/server/domains/logic/mapping"
)

// The properties a connector step's mappings are stored under: the same
// target → source maps a decision's inputs and outputs use, so a mapping means
// one thing wherever it is written.
const (
	inputMappingProperty  = "input_mapping"
	outputMappingProperty = "output_mapping"
)

// stepInputs is what a connector step sends: every process variable, or — when
// the step maps its inputs — only the fields it names, each computed from the
// variables.
//
// Only the named fields, as an HTTP step's input_ mapping has always done: a
// step that says what the partner is sent should not also send the rest of the
// process's data to somebody else's system.
func stepInputs(node entities.Node, variables map[string]any) map[string]any {
	inputs, ok := node.Properties[inputMappingProperty].(map[string]any)
	if !ok || len(inputs) == 0 {
		return variables
	}
	return mapping.Resolve(inputs, variables)
}

// stepOutputs is what a connector step keeps of its answer: all of it, or —
// when the step maps its outputs — only the variables it names, each computed
// from the answer.
//
// The designer's tables for these used to be written under names nothing on
// the server read, so a step configured to rename a field did not. See
// ui/src/domain/connectorStep.ts for how an older step is carried over.
func stepOutputs(node entities.Node, answer map[string]any) map[string]any {
	outputs, ok := node.Properties[outputMappingProperty].(map[string]any)
	if !ok || len(outputs) == 0 {
		return answer
	}
	return mapping.Resolve(outputs, answer)
}
