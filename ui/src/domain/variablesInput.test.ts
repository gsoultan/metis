import { describe, expect, it } from 'bun:test';
import { explainJsonError, formatVariables, parseVariables } from './variablesInput';

describe('parseVariables', () => {
  it('reads a blank box as no variables, not as a mistake', () => {
    expect(parseVariables('   \n ')).toEqual({ ok: true, value: {} });
  });

  it('reads an object', () => {
    expect(parseVariables('{"amount": 42.5}')).toEqual({ ok: true, value: { amount: 42.5 } });
  });

  it('refuses an array, because variables are named', () => {
    const result = parseVariables('[1, 2]');
    expect(result.ok).toBe(false);
    expect(result.ok === false && result.message).toContain('object');
  });

  it('refuses a bare value', () => {
    expect(parseVariables('42').ok).toBe(false);
    expect(parseVariables('null').ok).toBe(false);
  });

  it('names single quotes as the problem, since that is the usual one', () => {
    const result = parseVariables("{'amount': 42}");
    expect(result.ok === false && result.message).toContain('double quotes');
  });

  it('names a trailing comma as the problem', () => {
    const result = parseVariables('{"a": 1,}');
    expect(result.ok === false && result.message).toContain('comma');
  });

  it('still explains itself on an engine that names nothing', () => {
    const result = parseVariables('{\n  "a": 1\n  "b": 2\n}');
    expect(result.ok).toBe(false);
    expect(result.ok === false && result.message).toContain('not valid JSON');
  });
});

describe('formatVariables', () => {
  it('pretty-prints valid JSON', () => {
    expect(formatVariables('{"a":1}')).toBe('{\n  "a": 1\n}');
  });

  it('leaves invalid JSON alone rather than destroying what was typed', () => {
    expect(formatVariables('{"a":')).toBe('{"a":');
  });

  it('collapses an empty object to a blank box', () => {
    expect(formatVariables('{}')).toBe('');
  });
});

describe('explainJsonError', () => {
  const text = '{\n  "a": 1\n  "b": 2\n}';

  it('uses the line V8 names, rather than counting characters itself', () => {
    const v8 = `Expected ',' or '}' after property value in JSON at position 13 (line 3 column 3)`;
    expect(explainJsonError(v8, text)).toStartWith('Line 3: ');
  });

  it('converts an older engine\'s character offset into a line', () => {
    expect(explainJsonError('Unexpected string in JSON at position 13', text)).toStartWith('Line 3: ');
  });

  it('says nothing about where when the engine did not say', () => {
    expect(explainJsonError(`JSON Parse error: Expected '}'`, text)).toBe('This is not valid JSON yet.');
  });
});
