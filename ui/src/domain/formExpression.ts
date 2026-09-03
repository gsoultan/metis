/**
 * Evaluating the little expressions a form definition carries.
 *
 * A user task's `form_definition` is a property of a node in a deployed process
 * definition — untrusted input, authored by whoever can model a process. Its
 * fields carry `logic.hiddenIf`, `logic.disabledIf` and `{{ … }}` defaults, and
 * these used to be handed to `new Function`, inside a `with` block.
 *
 * That executed the author's JavaScript in the browser of whoever opened the
 * task. The victim is normally an approver, who by the nature of approval has
 * more authority than the person who wrote the form, and the code ran in their
 * session — reachable from it are the auth store, localStorage, and the token
 * in both. Demonstrated before this existed: a `hiddenIf` of
 * `(globalThis.x = token, false)` read the session token and the form rendered
 * as though nothing had happened.
 *
 * The server already refuses authored JavaScript in gateway conditions by
 * default, for the same reason. The browser is not a safer place to run it: it
 * is where the session is.
 *
 * So this is the same answer FEEL was on the server — a small evaluator that
 * can express what forms actually need and cannot express anything else. There
 * is no function call, no member access beyond the two context objects, no
 * assignment, and no way to reach a global. What it cannot parse it refuses,
 * rather than falling back to something that can run it.
 */

/** The two objects an expression may read. */
export interface FormScope {
  /** The values currently entered into the form. */
  data: Record<string, unknown>;
  /** The process instance's variables. */
  vars: Record<string, unknown>;
}

/** Raised for anything the grammar does not cover. */
export class ExpressionError extends Error {}

// Deep enough for any real condition, shallow enough that a hostile definition
// cannot exhaust the stack — this parser recurses, and a browser tab dying is
// still a denial of service.
const MAX_DEPTH = 32;

type Token =
  | { kind: 'number'; value: number }
  | { kind: 'string'; value: string }
  | { kind: 'name'; value: string }
  | { kind: 'op'; value: string };

const OPERATORS = [
  '===', '!==', '==', '!=', '<=', '>=', '&&', '||',
  '(', ')', '.', '!', '<', '>', '+', '-', '*', '/',
];

function tokenize(source: string): Token[] {
  const tokens: Token[] = [];
  let i = 0;

  while (i < source.length) {
    const char = source[i];

    if (/\s/.test(char)) {
      i += 1;
      continue;
    }

    if (/[0-9]/.test(char)) {
      let j = i;
      while (j < source.length && /[0-9.]/.test(source[j])) j += 1;
      const text = source.slice(i, j);
      const value = Number(text);
      if (Number.isNaN(value)) throw new ExpressionError(`${text} is not a number`);
      tokens.push({ kind: 'number', value });
      i = j;
      continue;
    }

    if (char === '"' || char === "'") {
      // No escape handling: a form condition comparing against a string with a
      // quote in it is not a case worth the parser surface, and refusing is
      // safe where guessing is not.
      const end = source.indexOf(char, i + 1);
      if (end < 0) throw new ExpressionError('a string is never closed');
      tokens.push({ kind: 'string', value: source.slice(i + 1, end) });
      i = end + 1;
      continue;
    }

    if (/[A-Za-z_$]/.test(char)) {
      let j = i;
      while (j < source.length && /[A-Za-z0-9_$]/.test(source[j])) j += 1;
      tokens.push({ kind: 'name', value: source.slice(i, j) });
      i = j;
      continue;
    }

    const operator = OPERATORS.find((candidate) => source.startsWith(candidate, i));
    if (!operator) throw new ExpressionError(`${char} is not part of a form expression`);
    tokens.push({ kind: 'op', value: operator });
    i += operator.length;
  }

  return tokens;
}

class Parser {
  private position = 0;
  private readonly tokens: Token[];
  private readonly scope: FormScope;

  constructor(tokens: Token[], scope: FormScope) {
    this.tokens = tokens;
    this.scope = scope;
  }

  evaluate(): unknown {
    const value = this.or(0);
    if (this.position < this.tokens.length) {
      throw new ExpressionError('the expression has trailing input');
    }
    return value;
  }

  private peek(): Token | undefined {
    return this.tokens[this.position];
  }

  private eat(value: string): boolean {
    const token = this.peek();
    if (token && token.kind === 'op' && token.value === value) {
      this.position += 1;
      return true;
    }
    return false;
  }

  private guard(depth: number): void {
    if (depth > MAX_DEPTH) throw new ExpressionError('the expression nests too deeply');
  }

  private or(depth: number): unknown {
    this.guard(depth);
    let left = this.and(depth + 1);
    while (this.eat('||')) {
      const right = this.and(depth + 1);
      left = Boolean(left) || Boolean(right);
    }
    return left;
  }

  private and(depth: number): unknown {
    this.guard(depth);
    let left = this.comparison(depth + 1);
    while (this.eat('&&')) {
      const right = this.comparison(depth + 1);
      left = Boolean(left) && Boolean(right);
    }
    return left;
  }

  private comparison(depth: number): unknown {
    this.guard(depth);
    const left = this.additive(depth + 1);
    for (const operator of ['===', '!==', '==', '!=', '<=', '>=', '<', '>']) {
      if (this.eat(operator)) {
        const right = this.additive(depth + 1);
        return compare(operator, left, right);
      }
    }
    return left;
  }

  private additive(depth: number): unknown {
    this.guard(depth);
    let left = this.multiplicative(depth + 1);
    for (;;) {
      if (this.eat('+')) left = arithmetic('+', left, this.multiplicative(depth + 1));
      else if (this.eat('-')) left = arithmetic('-', left, this.multiplicative(depth + 1));
      else return left;
    }
  }

  private multiplicative(depth: number): unknown {
    this.guard(depth);
    let left = this.unary(depth + 1);
    for (;;) {
      if (this.eat('*')) left = arithmetic('*', left, this.unary(depth + 1));
      else if (this.eat('/')) left = arithmetic('/', left, this.unary(depth + 1));
      else return left;
    }
  }

  private unary(depth: number): unknown {
    this.guard(depth);
    if (this.eat('!')) return !this.unary(depth + 1);
    if (this.eat('-')) return -toNumber(this.unary(depth + 1));
    return this.primary(depth + 1);
  }

  private primary(depth: number): unknown {
    this.guard(depth);
    const token = this.peek();
    if (!token) throw new ExpressionError('the expression ends early');

    if (this.eat('(')) {
      const value = this.or(depth + 1);
      if (!this.eat(')')) throw new ExpressionError('a bracket is never closed');
      return value;
    }

    if (token.kind === 'number' || token.kind === 'string') {
      this.position += 1;
      return token.value;
    }

    if (token.kind === 'name') {
      this.position += 1;
      return this.path(token.value);
    }

    throw new ExpressionError(`${token.value} cannot start an expression`);
  }

  /**
   * Reads a dotted path, and only from `data` or `vars`.
   *
   * This is the rule that makes the rest safe. An expression cannot name
   * `globalThis`, `window`, `localStorage` or anything else, because a root that
   * is not one of the two context objects is refused rather than looked up.
   */
  private path(root: string): unknown {
    if (root === 'true') return true;
    if (root === 'false') return false;
    if (root === 'null') return null;
    if (root === 'undefined') return undefined;

    if (root !== 'data' && root !== 'vars') {
      throw new ExpressionError(`${root} is not readable from a form expression; use data or vars`);
    }

    let current: unknown = this.scope[root];
    while (this.eat('.')) {
      const token = this.peek();
      if (!token || token.kind !== 'name') throw new ExpressionError('a dot is not followed by a name');
      this.position += 1;
      current = readProperty(current, token.value);
    }
    return current;
  }
}

/**
 * Reads one property, refusing the ones that lead out of plain data.
 *
 * Without this, `data.constructor.constructor` reaches Function and undoes the
 * entire point of the parser.
 */
function readProperty(target: unknown, name: string): unknown {
  if (target === null || target === undefined) return undefined;
  if (name === '__proto__' || name === 'constructor' || name === 'prototype') {
    throw new ExpressionError(`${name} is not readable from a form expression`);
  }
  if (typeof target !== 'object') return undefined;
  if (!Object.prototype.hasOwnProperty.call(target, name)) return undefined;
  return (target as Record<string, unknown>)[name];
}

function compare(operator: string, left: unknown, right: unknown): boolean {
  switch (operator) {
    case '===':
    case '==':
      return left === right;
    case '!==':
    case '!=':
      return left !== right;
    case '<':
      return toNumber(left) < toNumber(right);
    case '<=':
      return toNumber(left) <= toNumber(right);
    case '>':
      return toNumber(left) > toNumber(right);
    case '>=':
      return toNumber(left) >= toNumber(right);
    default:
      throw new ExpressionError(`${operator} is not a comparison`);
  }
}

function arithmetic(operator: string, left: unknown, right: unknown): unknown {
  if (operator === '+' && (typeof left === 'string' || typeof right === 'string')) {
    return String(left ?? '') + String(right ?? '');
  }
  const a = toNumber(left);
  const b = toNumber(right);
  switch (operator) {
    case '+': return a + b;
    case '-': return a - b;
    case '*': return a * b;
    case '/': return b === 0 ? NaN : a / b;
    default: throw new ExpressionError(`${operator} is not arithmetic`);
  }
}

function toNumber(value: unknown): number {
  if (typeof value === 'number') return value;
  if (typeof value === 'boolean') return value ? 1 : 0;
  if (typeof value === 'string' && value.trim() !== '') return Number(value);
  return NaN;
}

/** Evaluates an expression, returning its value. Throws for anything refused. */
export function evaluateFormExpression(expression: string, scope: FormScope): unknown {
  return new Parser(tokenize(expression), scope).evaluate();
}

/**
 * Evaluates an expression as a condition.
 *
 * A refused or broken expression is false rather than an exception, because the
 * caller is deciding whether to hide a field and a thrown error there would
 * blank the form. False means "not hidden", "not disabled" — the field stays
 * visible and editable, which is the safe direction for a rule nobody can read.
 */
export function evaluateFormCondition(expression: string, scope: FormScope): boolean {
  if (!expression) return false;
  try {
    return Boolean(evaluateFormExpression(expression, scope));
  } catch (error) {
    console.warn(
      `Form logic was refused and treated as false: ${expression}`,
      error instanceof Error ? error.message : error,
    );
    return false;
  }
}
