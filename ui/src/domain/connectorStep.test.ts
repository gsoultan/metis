import { describe, expect, it } from 'bun:test';

import type { ApiConnector } from '../services/types';
import {
  clearedStepFields,
  missingStepFields,
  queryParameters,
  STEP_REQUEST_KEYS,
  stepFieldPatch,
  stepFieldValue,
  stepSchemasOf,
  unmappedParameters,
} from './connectorStep';

const lookup: ApiConnector = {
  id: 'lookup-id',
  key: 'sql-query',
  name: 'Database Lookup',
  node_schema: [
    { key: 'connector_statement', label: 'Query', type: 'textarea', required: true },
    { key: 'connector_params', label: 'Values', type: 'mapping' },
    { key: 'result_variable', label: 'Store the answer as', type: 'string', required: true },
  ],
};
const slack: ApiConnector = { id: 'slack-id', key: 'slack-message', name: 'Slack' };

describe('connector steps', () => {
  it('knows which connectors ask a step for fields', () => {
    const schemas = stepSchemasOf([lookup, slack]);
    expect(schemas.has('lookup-id')).toBe(true);
    expect(schemas.has('slack-id')).toBe(false);
  });

  it('reads a field under its stored name or the alias an older editor used', () => {
    expect(stepFieldValue({ result_variable: 'customer' }, 'result_variable')).toBe('customer');
    expect(stepFieldValue({ resultVariable: 'customer' }, 'result_variable')).toBe('customer');
  });

  it('clears the alias as it sets a field, so a value loaded earlier cannot win on save', () => {
    expect(stepFieldPatch('result_variable', 'customer')).toEqual({ result_variable: 'customer', resultVariable: undefined });
    expect(stepFieldPatch('connector_statement', 'SELECT 1')).toEqual({ connector_statement: 'SELECT 1' });
  });

  it('clears every request field when the connector changes', () => {
    // A query left on a step that now posts to Slack would still make it a
    // lookup the server refuses to deploy.
    const cleared = clearedStepFields();
    for (const key of STEP_REQUEST_KEYS) {
      expect(key in cleared).toBe(true);
      expect(cleared[key]).toBeUndefined();
    }
    expect('resultVariable' in cleared).toBe(true);
  });

  it('names the required fields a step has left empty', () => {
    const missing = missingStepFields({ connector_statement: '  ' }, lookup.node_schema ?? []);
    expect(missing.map((field) => field.label)).toEqual(['Query', 'Store the answer as']);
    expect(missingStepFields({ connector_statement: 'SELECT 1', result_variable: 'x' }, lookup.node_schema ?? [])).toEqual([]);
  });

  it('finds the values a query asks for', () => {
    expect(queryParameters('SELECT * FROM t WHERE a = :id AND b = :id OR c = :name')).toEqual(['id', 'name']);
    expect(queryParameters("SELECT ':not_this', created_at::date FROM t WHERE x = :this")).toEqual(['this']);
    expect(queryParameters('SELECT [a:b], "c:d" FROM t')).toEqual([]);
  });

  it('says which values have nowhere to come from', () => {
    const data = {
      connector_statement: 'SELECT tier FROM customers WHERE id = :customer_id AND region = :region',
      connector_params: { customer_id: 'customerId', region: '' },
    };
    expect(unmappedParameters(data)).toEqual(['region']);
    expect(unmappedParameters({})).toEqual([]);
  });
});
