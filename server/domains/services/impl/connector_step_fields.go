package impl

import (
	"fmt"
	"maps"
	"strings"

	"github.com/gsoultan/metis/server/domains/entities"
	servicecontracts "github.com/gsoultan/metis/server/domains/services/contracts"
	"github.com/gsoultan/metis/server/domains/services/impl/connectors"
)

// withStepFields adds what a step wrote in its connector's fields — the
// message, the recipient, the subject — to what the connector is sent.
//
// Each field is a template filled in from the process variables, and it wins
// over a variable or mapped input of the same name: it is what the modeller
// wrote on this step. A template that cannot be filled in fails the step,
// rather than sending the message with a hole in it.
//
// inputs is copied before it is changed: without an input mapping it is the
// instance's own variables.
func withStepFields(node entities.Node, inputs, variables map[string]any) (map[string]any, error) {
	fields := stepFieldTemplates(node)
	if len(fields) == 0 {
		return inputs, nil
	}
	out := maps.Clone(inputs)
	if out == nil {
		out = make(map[string]any, len(fields))
	}
	for name, template := range fields {
		value, err := connectors.RenderStepText(template, variables)
		if err != nil {
			return nil, fmt.Errorf("step %q could not fill in its %s: %w", node.ID, name, err)
		}
		out[name] = value
	}
	return out, nil
}

// stepFieldTemplates returns the fields a step filled in, by the payload name
// each is sent under. A field left blank is not sent, so an input of that name
// still is.
func stepFieldTemplates(node entities.Node) map[string]string {
	fields := map[string]string{}
	for key, value := range node.Properties {
		name, isField := strings.CutPrefix(key, servicecontracts.StepFieldPrefix)
		text, isText := value.(string)
		if !isField || name == "" || !isText || strings.TrimSpace(text) == "" {
			continue
		}
		fields[name] = text
	}
	return fields
}
