import { describe, expect, it } from 'bun:test';

import { migrateConnectorMappings } from './connectorMappings';

describe('carrying an older connector step over', () => {
  it('moves the old tables to where the engine reads them, turned round', () => {
    const migrated = migrateConnectorMappings({
      connector_id: 'c',
      inputs: { companyNumber: 'registration_id' },
      outputs: { credit_score: 'creditScore' },
    });
    expect(migrated).toEqual({
      connector_id: 'c',
      input_mapping: { registration_id: 'companyNumber' },
      output_mapping: { creditScore: 'credit_score' },
    });
  });

  it('keeps new maps a step already has, and still drops the old keys', () => {
    const migrated = migrateConnectorMappings({
      inputs: { a: 'x' },
      input_mapping: { y: 'b' },
    });
    expect(migrated).toEqual({ input_mapping: { y: 'b' } });
  });

  it('leaves a step with nothing to carry over exactly as it was', () => {
    const properties = { connector_id: 'c', result_variable: 'customer' };
    expect(migrateConnectorMappings(properties)).toBe(properties);
  });

  it('skips a row whose target was never filled in', () => {
    expect(migrateConnectorMappings({ inputs: { a: '', b: 'field' } })).toEqual({ input_mapping: { field: 'b' } });
  });
});
