package impl

import (
	"github.com/gsoultan/metis/server/domains/entities"
	"github.com/gsoultan/metis/server/domains/logic/mapping"
	servicecontracts "github.com/gsoultan/metis/server/domains/services/contracts"
)

// connectorRequestFor reads what a step asks of its connector.
//
// Parameters are resolved here, against the variables, with the same mapping a
// decision's inputs use — so a parameter is a variable name or a FEEL
// expression in exactly the sense it is everywhere else. The executor receives
// values, never expressions, and binds them.
func connectorRequestFor(node entities.Node, variables map[string]any) servicecontracts.ConnectorRequest {
	req := servicecontracts.ConnectorRequest{
		Statement:      node.GetStringProperty(servicecontracts.StatementProperty),
		ResultVariable: node.GetStringProperty(servicecontracts.ResultVariableProperty),
		Variables:      variables,
	}
	if params, ok := node.Properties[servicecontracts.ParamsProperty].(map[string]any); ok && len(params) > 0 {
		req.Params = mapping.Resolve(params, variables)
	}
	return req
}
