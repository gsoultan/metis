import { describe, expect, test } from 'bun:test';

import { raiseIfRefused } from './raise';

describe('raiseIfRefused', () => {
  test('raises the refusal the HTTP endpoints report in err', () => {
    expect(() => raiseIfRefused({ err: 'Task already claimed' })).toThrow('Task already claimed');
  });

  test('raises the refusal Connect RPC reports in error', () => {
    expect(() => raiseIfRefused({ error: 'not permitted' })).toThrow('not permitted');
  });

  test('lets a successful reply through unchanged', () => {
    const reply = { err: '', id: 'task-1' };
    expect(raiseIfRefused(reply)).toBe(reply);
  });

  test('treats the field being absent, empty or null as success', () => {
    // All three are how "nothing went wrong" arrives: protobuf sends "" for an
    // unset string, the HTTP handlers omit the key, and JSON null shows up when
    // a handler sets it explicitly.
    expect(() => raiseIfRefused({})).not.toThrow();
    expect(() => raiseIfRefused({ err: '' })).not.toThrow();
    expect(() => raiseIfRefused({ err: null })).not.toThrow();
    expect(() => raiseIfRefused({ error: '' })).not.toThrow();
  });
});
