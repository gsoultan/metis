/**
 * How the table checks read one condition cell: whether they can read it at
 * all, and which values it accepts.
 *
 * A partial reimplementation of the unary tests the engine runs, covering the
 * notations people actually write and nothing more. Where it disagrees with
 * the engine, both checks built on it — coverage and overlaps — describe a
 * table that does not exist, so each rule here names the engine behaviour it
 * follows. A cell outside it makes a column "not checked", never guessed at.
 */
import { ANY_VALUE } from './decisionTable';

const NUMBER_LITERAL = /^-?\d+(\.\d+)?$/;
const COMPARISON = /^(>=|<=|>|<|!=|=)\s*-?\d+(\.\d+)?$/;
const RANGE = /^[[\]]\s*-?[\d.]+\s*\.\.\s*-?[\d.]+\s*[[\]]$/;

/**
 * Whether this analysis understands a cell well enough to trust its verdict.
 *
 * The column's type decides how much of the notation it reads. A comparison or
 * a range compares numbers, and the samples tried for any other column are
 * words, so in a Text column — which a new column is — `> 5` used to be read as
 * the text "> 5". The engine compares it with whatever number the process
 * supplies, so the checks built on that reading described a table that does
 * not exist.
 */
export function understandsCell(cell: string, type: string): boolean {
  const text = cell.trim();
  if (text === '' || text === ANY_VALUE) return true;
  if (COMPARISON.test(text) || RANGE.test(text)) return type === 'number';
  if (NUMBER_LITERAL.test(text) || /^(true|false)$/i.test(text)) return true;
  // not( ) is only as readable as what it negates. It used to be accepted
  // whatever it held, and `not(sum(items) > 10)` read as "anything but that
  // text": a condition that matches nearly everything, and an overlap error
  // that blocked Save over a table the check could not read.
  const negated = text.match(/^not\((.+)\)$/);
  if (negated) return understandsCell(negated[1], type) && !isWildcardText(negated[1]);
  // A list, or a single literal.
  return text.split(',').every(readablePart);
}

/** One name: what the engine reads as text when it is written without quotes. */
const SINGLE_NAME = /^[A-Za-z_]\w*$/;

/**
 * Single names the engine does not read as text: its keywords — `null` is no
 * value, `and` cannot start a test — and the name a cell's own input goes by.
 */
const NOT_TEXT = new Set(['and', 'or', 'not', 'null', 'in', 'between', 'if', 'then', 'else', '_input']);

/**
 * One part of a list, read the way the engine reads it: quoted text, a number,
 * or a single name. Bare text used to be read whatever it held, and the engine
 * reads `Gold Member`, `SKU-1`, `3M`, `1 000`, `v1.2` and `.5` as something
 * else or not at all — so a table it cannot run showed a green tick.
 */
function readablePart(part: string): boolean {
  const text = part.trim();
  if (/^("[^"]*"|'[^']*')$/.test(text) || NUMBER_LITERAL.test(text)) return true;
  return SINGLE_NAME.test(text) && !NOT_TEXT.has(text);
}

/**
 * Whether a cell holds unquoted text the engine cannot read as text — more
 * than one plain word, a dash or a dot, a digit first, or a keyword such as
 * `in` — which quotes would make readable. `null` is left out: unquoted, it
 * means no value at all, which may be what was meant.
 */
export function needsQuotes(cell: string): boolean {
  const text = cell.trim().replace(/^not\((.+)\)$/, '$1');
  return text.split(',').some((part) => {
    const word = part.trim();
    if (word === 'null' || word === '_input' || /^-\s*\d/.test(word)) return false;
    return /^[\w .-]+$/.test(word) && !readablePart(word);
  });
}

/** Whether a cell compares numbers — `> 5`, `[1..10]`, either under not( ) — rather than naming values. */
export function comparesNumbers(cell: string): boolean {
  const text = cell.trim().replace(/^not\((.+)\)$/, '$1').trim();
  return COMPARISON.test(text) || RANGE.test(text);
}

function isWildcardText(text: string): boolean {
  return text.trim() === '' || text.trim() === ANY_VALUE;
}

/** Whether a cell accepts a value. */
export type CellTest = (value: string | number | boolean) => boolean;

/**
 * Reads a cell into a test of whether it accepts a value.
 *
 * A partial reimplementation of the unary tests the engine runs, covering what
 * understandsCell admits and nothing more.
 *
 * The cell is read once and the test put to many values: the overlap check
 * asks every cell of a column about every value worth trying, which for a long
 * banded table is hundreds per cell, and reading the cell again for each of
 * them was most of the check's time.
 */
export function cellMatcher(cell: string, type: string): CellTest {
  const text = cell.trim();
  if (text === '' || text === ANY_VALUE) return () => true;

  // A yes/no column is read like any other: literalValue takes only the
  // lower-case words as yes and no, as the engine does — `TRUE` is a bare word,
  // which it reads as text, and text never equals a boolean. The column used to
  // stop here with true and false, so `not(true)` and `true, false` matched no
  // answer at all.
  const negated = text.match(/^not\((.+)\)$/);
  if (negated) {
    const inner = cellMatcher(negated[1], type);
    return (value) => !inner(value);
  }

  const numeric = type === 'number' ? numberTest(text) : undefined;
  // A list, or a single literal. Compared with its type, as the engine
  // compares: `"10"` is text and never equals the number 10, and a bare `10`
  // is a number and never equals the text "10".
  const literals = text.split(',').map(literalValue);
  return (value) => (numeric && typeof value === 'number' ? numeric(value) : literals.includes(value));
}

/** One literal as the engine reads it: quoted text, a number, true or false, or a bare word read as text. */
function literalValue(part: string): string | number | boolean {
  const text = part.trim();
  if (text.length >= 2 && (text.startsWith('"') || text.startsWith("'")) && text.endsWith(text[0])) {
    return text.slice(1, -1);
  }
  if (/^-?\d+(\.\d+)?$/.test(text)) return Number(text);
  if (text === 'true' || text === 'false') return text === 'true';
  return text;
}

/** A range or a comparison, when that is what the cell is. */
function numberTest(text: string): ((value: number) => boolean) | undefined {
  const range = text.match(/^([[\]])\s*(-?[\d.]+)\s*\.\.\s*(-?[\d.]+)\s*([[\]])$/);
  if (range) {
    const [, open, low, high, close] = range;
    const [from, to] = [Number(low), Number(high)];
    // `]` closes inclusively and `[` closes exclusively — the DMN spelling.
    return (value) => (open === '[' ? value >= from : value > from) && (close === ']' ? value <= to : value < to);
  }
  const comparison = text.match(/^(>=|<=|>|<|!=|=)?\s*(-?[\d.]+)$/);
  if (!comparison) return undefined;
  const [, operator = '=', operand] = comparison;
  const bound = Number(operand);
  switch (operator) {
    case '>':
      return (value) => value > bound;
    case '<':
      return (value) => value < bound;
    case '>=':
      return (value) => value >= bound;
    case '<=':
      return (value) => value <= bound;
    case '!=':
      return (value) => value !== bound;
    default:
      return (value) => value === bound;
  }
}
