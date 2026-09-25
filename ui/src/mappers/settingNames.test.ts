import { describe, expect, it } from 'bun:test';

import { editorKeyFor, heldName, heldOnce, storedName } from './settingNames';

describe('storedName', () => {
  it('sends a panel field under the name the server stores it by', () => {
    expect(storedName('httpUrl')).toBe('http_url');
    expect(storedName('duration')).toBe('timer_duration');
  });

  it('reads a name an older editor wrote', () => {
    expect(storedName('decisionKey')).toBe('decision_key');
    expect(storedName('inputMapping')).toBe('input_mapping');
  });

  it('leaves any other name as it is', () => {
    expect(storedName('decision_key')).toBe('decision_key');
    expect(storedName('some_new_setting')).toBe('some_new_setting');
  });
});

describe('heldName', () => {
  it('is the name the panel reads', () => {
    expect(heldName('http_url')).toBe('httpUrl');
    expect(heldName('httpUrl')).toBe('httpUrl');
    // The panel writes these under the stored name itself.
    expect(heldName('inputMapping')).toBe('input_mapping');
    expect(heldName('decision_key')).toBe('decision_key');
  });
});

describe('heldOnce', () => {
  it('holds what the server sent under the names the panel reads', () => {
    expect(heldOnce({ http_url: 'https://a.example', decision_key: 'risk', topic: 'orders' })).toEqual({
      httpUrl: 'https://a.example',
      decision_key: 'risk',
      topic: 'orders',
    });
  });

  it('keeps the panel’s copy where a setting is there twice', () => {
    expect(heldOnce({ http_url: 'stale', httpUrl: 'edited' })).toEqual({ httpUrl: 'edited' });
    expect(heldOnce({ inputMapping: { a: 'stale' }, input_mapping: { a: 'edited' } })).toEqual({
      input_mapping: { a: 'edited' },
    });
  });

  it('drops the nested copy steps used to carry', () => {
    expect(heldOnce({ label: 'Rule', properties: { http_url: 'https://a.example' } })).toEqual({ label: 'Rule' });
  });
});

describe('editorKeyFor', () => {
  it('names the other key a setting may be left under, for a writer to clear', () => {
    expect(editorKeyFor('result_variable')).toBe('resultVariable');
    expect(editorKeyFor('input_mapping')).toBe('inputMapping');
    expect(editorKeyFor('connector_statement')).toBeUndefined();
  });
});
