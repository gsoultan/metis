package impl

import (
	"testing"

	"github.com/gsoultan/metis/server/domains/entities"
)

func TestConnectorRequestFor(t *testing.T) {
	variables := map[string]any{"orderId": "A-1", "qty": 3.0, "price": 10.0}

	for name, tc := range map[string]struct {
		properties     map[string]any
		wantStatement  string
		wantResult     string
		wantParams     map[string]any
		wantNoParamSet bool
	}{
		"a step that asks for nothing": {
			properties:     map[string]any{"connector_id": "x"},
			wantNoParamSet: true,
		},
		"names and expressions are resolved to values": {
			properties: map[string]any{
				"connector_statement": "SELECT 1",
				"connector_params":    map[string]any{"order": "orderId", "total": "qty * price"},
				"result_variable":     "answer",
			},
			wantStatement: "SELECT 1",
			wantResult:    "answer",
			wantParams:    map[string]any{"order": "A-1", "total": 30.0},
		},
		// Resolving omits a parameter whose value is nothing rather than
		// writing it as null; the executor then refuses the missing name.
		"a parameter with no value is left out": {
			properties: map[string]any{"connector_params": map[string]any{"missing": "nobodySetThis"}},
			wantParams: map[string]any{},
		},
		"parameters that are not a map are none": {
			properties:     map[string]any{"connector_params": "order=orderId"},
			wantNoParamSet: true,
		},
	} {
		t.Run(name, func(t *testing.T) {
			req := connectorRequestFor(entities.Node{Properties: tc.properties}, variables)
			if req.Statement != tc.wantStatement {
				t.Errorf("statement = %q, want %q", req.Statement, tc.wantStatement)
			}
			if req.ResultVariable != tc.wantResult {
				t.Errorf("result variable = %q, want %q", req.ResultVariable, tc.wantResult)
			}
			if tc.wantNoParamSet {
				if req.Params != nil {
					t.Errorf("params = %v, want none", req.Params)
				}
				return
			}
			if len(req.Params) != len(tc.wantParams) {
				t.Fatalf("params = %v, want %v", req.Params, tc.wantParams)
			}
			for k, v := range tc.wantParams {
				if req.Params[k] != v {
					t.Errorf("param %s = %#v, want %#v", k, req.Params[k], v)
				}
			}
		})
	}
}
