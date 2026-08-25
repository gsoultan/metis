import { describe, expect, test } from 'bun:test';
import { buildCondition, isTooRichForBuilder, parseCondition } from './conditionExpression';

describe('buildCondition', () => {
  test('never emits JavaScript', () => {
    // The server refuses `js:` conditions by default; a builder that still
    // wrote them would author definitions that stop routing.
    for (const operator of ['==', '!=', '>', '<', '>=', '<='] as const) {
      expect(buildCondition({ variable: 'amount', operator, value: '100' })).not.toStartWith('js:');
    }
  });

  test('numbers and booleans stay bare', () => {
    expect(buildCondition({ variable: 'amount', operator: '>', value: '100' })).toBe('amount > 100');
    expect(buildCondition({ variable: 'amount', operator: '<=', value: '99.5' })).toBe('amount <= 99.5');
    expect(buildCondition({ variable: 'approved', operator: '==', value: 'true' })).toBe('approved = true');
  });

  test('strings are quoted, or a comparison would name a second variable', () => {
    expect(buildCondition({ variable: 'status', operator: '!=', value: 'approved' })).toBe('status != "approved"');
    expect(buildCondition({ variable: 'status', operator: '==', value: 'GOLD' })).toBe('status = "GOLD"');
  });

  test('equality is FEEL single-equals — the engine has no == token', () => {
    expect(buildCondition({ variable: 'tier', operator: '==', value: 'VIP' })).toBe('tier = "VIP"');
  });

  test("a user's own quotes are not doubled", () => {
    expect(buildCondition({ variable: 'status', operator: '==', value: "'approved'" })).toBe('status = "approved"');
    expect(buildCondition({ variable: 'status', operator: '==', value: '"approved"' })).toBe('status = "approved"');
  });

  test('embedded double quotes are escaped, not truncated', () => {
    expect(buildCondition({ variable: 'note', operator: '==', value: 'say "hi"' })).toBe('note = "say \\"hi\\""');
  });

  test('a half-typed comparison stores as empty, not as garbage', () => {
    expect(buildCondition({ variable: '', operator: '>', value: '100' })).toBe('');
    expect(buildCondition({ variable: 'amount', operator: '>', value: '' })).toBe('');
  });
});

describe('parseCondition', () => {
  test('reads back what it writes', () => {
    for (const comparison of [
      { variable: 'amount', operator: '>' as const, value: '100' },
      { variable: 'status', operator: '!=' as const, value: 'approved' },
      { variable: 'tier', operator: '==' as const, value: 'VIP' },
      // A quoted value may contain boolean keywords without becoming compound.
      { variable: 'color', operator: '==' as const, value: 'black and white' },
    ]) {
      expect(parseCondition(buildCondition(comparison))).toEqual(comparison);
    }
  });

  test('opens legacy js: conditions for editing', () => {
    expect(parseCondition('js:amount > 100')).toEqual({ variable: 'amount', operator: '>', value: '100' });
    expect(parseCondition('js:status == approved')).toEqual({ variable: 'status', operator: '==', value: 'approved' });
  });

  test('opens the legacy bare-equals shape', () => {
    expect(parseCondition('status=approved')).toEqual({ variable: 'status', operator: '==', value: 'approved' });
  });

  test('refuses what three fields cannot hold, rather than mangling it', () => {
    expect(parseCondition('amount > 100 and tier = "GOLD"')).toBeNull();
    expect(parseCondition('js:new Array(1e9).join("x")')).toBeNull();
    expect(parseCondition('')).toBeNull();
  });
});

describe('isTooRichForBuilder', () => {
  test('an empty condition is editable, not rich', () => {
    // Empty means "no condition yet" — the builder's starting state, which
    // must not be mistaken for something worth protecting.
    expect(isTooRichForBuilder('')).toBe(false);
    expect(isTooRichForBuilder('   ')).toBe(false);
  });

  test('simple comparisons are editable', () => {
    expect(isTooRichForBuilder('amount > 100')).toBe(false);
    expect(isTooRichForBuilder('status = "GOLD"')).toBe(false);
    expect(isTooRichForBuilder('js:amount > 100')).toBe(false);
    expect(isTooRichForBuilder('status=approved')).toBe(false);
  });

  test('expressions the three fields cannot hold are protected', () => {
    expect(isTooRichForBuilder('amount > 100 and tier = "GOLD"')).toBe(true);
    expect(isTooRichForBuilder('js:new Array(1e9).join("x")')).toBe(true);
    expect(isTooRichForBuilder('date(dueDate) < today()')).toBe(true);
  });
});
