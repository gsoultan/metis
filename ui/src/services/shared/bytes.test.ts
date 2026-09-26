import { describe, expect, it } from 'bun:test';

import { fromBase64, toBase64 } from './bytes';

describe('toBase64 / fromBase64', () => {
  it('carries names in any script, where btoa threw', () => {
    const xml = '<bpmn:task name="Genehmigung prüfen — 审批 — موافقة"/>';
    expect(() => btoa(xml)).toThrow();
    expect(fromBase64(toBase64(xml))).toBe(xml);
  });

  it('encodes the UTF-8 bytes, which is what the server decodes', () => {
    // "é" is C3 A9 in UTF-8; Go's encoding/json writes those bytes as "w6k=".
    expect(toBase64('é')).toBe('w6k=');
    expect(fromBase64('w6k=')).toBe('é');
  });

  it('reads a model the server exported without garbling it', () => {
    // What atob produced for the same bytes: one character per byte.
    expect(atob('w6k=')).not.toBe('é');
  });

  it('handles a model larger than one call can spread', () => {
    const large = 'ä'.repeat(200_000);
    expect(fromBase64(toBase64(large))).toBe(large);
  });
});
