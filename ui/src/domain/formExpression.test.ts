import { describe, expect, it } from 'bun:test';
import { evaluateFormCondition, evaluateFormExpression, ExpressionError } from './formExpression';

const scope = {
  data: { amount: 500, status: 'approved', flag: true, nested: { deep: 'yes' } },
  vars: { region: 'EU', count: 3 },
};

describe('what a form definition can no longer do', () => {
  // The vulnerability this replaced. `hiddenIf` went to `new Function` inside a
  // `with` block, so a definition author's JavaScript ran in the browser of
  // whoever opened the task — normally an approver, in their session, with the
  // auth store and localStorage in reach. Demonstrated against the old code: a
  // hiddenIf of `(globalThis.x = token, false)` read the session token and the
  // form rendered as though nothing had happened.
  it('cannot reach a global', () => {
    (globalThis as unknown as Record<string, unknown>).__stolen = null;
    expect(() => evaluateFormExpression('(globalThis.__stolen = 1, false)', scope)).toThrow(ExpressionError);
    expect((globalThis as unknown as Record<string, unknown>).__stolen).toBeNull();
  });

  it.each([
    ['globalThis.localStorage', 'a global by name'],
    ['window.document.cookie', 'the document'],
    ['fetch("https://evil.example.com")', 'a network call'],
    ['data.constructor.constructor("return 1")()', 'Function via constructor'],
    ['data.__proto__', 'the prototype chain'],
    ['data.toString()', 'a method call'],
    ['[].map(x => x)', 'an arrow function'],
    ['data.amount = 1', 'an assignment'],
  ])('refuses %s (%s)', (expression) => {
    expect(() => evaluateFormExpression(expression, scope)).toThrow(ExpressionError);
  });

  // A refused expression must not break the form. It is deciding whether to
  // hide a field, and throwing there would blank the page.
  it('treats a refused condition as false, so the field stays visible', () => {
    expect(evaluateFormCondition('globalThis.anything', scope)).toBe(false);
    expect(evaluateFormCondition('this is not an expression', scope)).toBe(false);
  });
});

describe('what a form definition still needs to do', () => {
  it.each([
    ['data.amount > 100', true],
    ['data.amount < 100', false],
    ['data.status === "approved"', true],
    ["data.status === 'rejected'", false],
    ['data.status !== "rejected"', true],
    ['vars.region == "EU"', true],
    ['data.flag', true],
    ['!data.flag', false],
    ['data.amount >= 500 && vars.region === "EU"', true],
    ['data.amount > 1000 || data.flag', true],
    ['(data.amount > 100) && !(vars.count > 10)', true],
    ['data.nested.deep === "yes"', true],
    ['data.missing === undefined', true],
    ['vars.count + 1 > 3', true],
    ['data.amount / 2 === 250', true],
  ])('evaluates %s', (expression, expected) => {
    expect(evaluateFormCondition(expression, scope)).toBe(expected);
  });

  it('returns a value, not only a boolean, for default expressions', () => {
    expect(evaluateFormExpression('vars.region', scope)).toBe('EU');
    expect(evaluateFormExpression('vars.count + 1', scope)).toBe(4);
    expect(evaluateFormExpression('"Order " + vars.region', scope)).toBe('Order EU');
  });

  it('reads a missing field as undefined rather than failing', () => {
    expect(evaluateFormExpression('data.nothing', scope)).toBeUndefined();
    expect(evaluateFormExpression('data.nothing.deeper', scope)).toBeUndefined();
  });
});

describe('bounds', () => {
  it('refuses an expression that nests too deeply to be a form rule', () => {
    const deep = '('.repeat(200) + 'data.flag' + ')'.repeat(200);
    expect(() => evaluateFormExpression(deep, scope)).toThrow(ExpressionError);
  });

  it('refuses an unterminated string rather than guessing', () => {
    expect(() => evaluateFormExpression('data.status === "open', scope)).toThrow(ExpressionError);
  });
});
