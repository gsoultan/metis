package impl

import (
	"github.com/gsoultan/metis/server/domains/entities"
	"github.com/gsoultan/metis/server/domains/logic/mapping"
	servicecontracts "github.com/gsoultan/metis/server/domains/services/contracts"
)

// The node properties a step names its own connector request by. The designer
// writes them from the fields a connector asks a step for.
const (
	// connectorStatementProperty is the operation text the step wrote — for a
	// database lookup, its SQL.
	connectorStatementProperty = "connector_statement"
	// connectorParamsProperty maps each parameter the statement names to the
	// process value it takes: a variable name or a FEEL expression.
	connectorParamsProperty = "connector_params"
	// resultVariableProperty is the variable the answer is stored under. The
	// designer already writes it under this name for a script task.
	resultVariableProperty = "result_variable"
)

// connectorRequestFor reads what a step asks of its connector.
//
// Parameters are resolved here, against the variables, with the same mapping a
// decision's inputs use — so a parameter is a variable name or a FEEL
// expression in exactly the sense it is everywhere else. The executor receives
// values, never expressions, and binds them.
func connectorRequestFor(node entities.Node, variables map[string]any) servicecontracts.ConnectorRequest {
	req := servicecontracts.ConnectorRequest{
		Statement:      node.GetStringProperty(connectorStatementProperty),
		ResultVariable: node.GetStringProperty(resultVariableProperty),
		Variables:      variables,
	}
	if params, ok := node.Properties[connectorParamsProperty].(map[string]any); ok && len(params) > 0 {
		req.Params = mapping.Resolve(params, variables)
	}
	return req
}
