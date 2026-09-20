/**
 * The two or three things about a task worth putting in a list row.
 *
 * The task list rendered every process variable as its own outlined badge,
 * uppercased and truncated by the column width:
 *
 *     AMOUNT: 1750     APPROVALLEVEL: DIR…   APPROVER: FINANCE…
 *     CURRENCY: GBP    DESCRIPTION: EXPEN…   SUBMITTEDBY: ALICE
 *
 * Three things were wrong with that. It is the database schema shown to an
 * approver, which `.junie/guidelines.md` §5 forbids. Every badge was clipped
 * mid-word, so none of them could be read. And `AMOUNT: 1750` next to
 * `CURRENCY: GBP` asks the person to do the join themselves, when what they
 * need to see is £1,750.
 *
 * Six badges per row also set the row height to ~137px, so twelve tasks filled
 * 2,000px of screen.
 *
 * This picks a small number of facts, composes the ones that belong together,
 * and says how many it left out. The full set stays available on the task
 * itself — a list cell is a summary, not a record.
 */
import { humanizeIdentifier } from './wording';

/**
 * Variable names already used as the row's reference line by `taskReference`.
 * Repeating them as a badge says the same thing twice in one row.
 */
const REFERENCE_KEYS = new Set([
  'reference', 'title', 'subject', 'description', 'summary', 'name', 'label',
]);

/** Paired money variables, so `amount` + `currency` render as one fact. */
const AMOUNT_KEYS = ['amount', 'value', 'total'];
const CURRENCY_KEYS = ['currency', 'currencycode', 'currency_code'];

/** More than this in one row is a record, not a summary. */
export const MAX_FACTS = 3;

/** Longer than this breaks the column whatever the screen width. */
const MAX_VALUE = 28;

export interface TaskFact {
  /** The variable name in words — "Approval level", not `APPROVALLEVEL`. */
  label: string;
  /** The value, formatted and bounded. */
  value: string;
}

export interface TaskFacts {
  facts: TaskFact[];
  /** How many readable variables were left out, for a "+N more" affordance. */
  hidden: number;
}

/**
 * Picks the facts worth showing for one task.
 *
 * Variables come from a process definition somebody authored, so a value may be
 * anything: an object, an array, null, a very long string. Only scalars are
 * shown — a list cell is not the place to discover that a variable holds a
 * nested object.
 */
export function taskFacts(
  variables: Record<string, unknown> | undefined,
  max: number = MAX_FACTS,
): TaskFacts {
  if (!variables || max <= 0) return { facts: [], hidden: 0 };

  const normalised = new Map<string, unknown>();
  for (const [key, value] of Object.entries(variables)) {
    normalised.set(key.toLowerCase().replace(/[_-]/g, ''), value);
  }

  const used = new Set<string>();
  const facts: TaskFact[] = [];

  const money = moneyFact(variables, normalised, used);
  if (money) facts.push(money);

  for (const [key, value] of Object.entries(variables)) {
    const normal = key.toLowerCase().replace(/[_-]/g, '');
    if (used.has(normal)) continue;
    if (REFERENCE_KEYS.has(normal)) continue;

    const text = scalarText(value);
    if (text === '') continue;

    used.add(normal);
    facts.push({ label: humanizeIdentifier(key), value: text });
  }

  return { facts: facts.slice(0, max), hidden: Math.max(0, facts.length - max) };
}

/**
 * Composes an amount and its currency into a single fact.
 *
 * Falls back to the bare number when there is no currency, and to the raw code
 * when `Intl` does not recognise it — a process may carry "points" or an
 * internal unit, and refusing to render it would lose the number entirely.
 */
function moneyFact(
  variables: Record<string, unknown>,
  normalised: Map<string, unknown>,
  used: Set<string>,
): TaskFact | null {
  const amountKey = AMOUNT_KEYS.find((k) => normalised.has(k) && isNumeric(normalised.get(k)));
  if (!amountKey) return null;

  const amount = Number(normalised.get(amountKey));
  const currencyKey = CURRENCY_KEYS.find((k) => scalarText(normalised.get(k)) !== '');
  const currency = currencyKey ? String(normalised.get(currencyKey)).toUpperCase() : '';

  used.add(amountKey);
  if (currencyKey) used.add(currencyKey);

  // The original casing is what the label should read, so find it back.
  const originalKey = Object.keys(variables)
    .find((k) => k.toLowerCase().replace(/[_-]/g, '') === amountKey) ?? amountKey;

  return { label: humanizeIdentifier(originalKey), value: formatMoney(amount, currency) };
}

function formatMoney(amount: number, currency: string): string {
  if (currency === '') return new Intl.NumberFormat().format(amount);
  try {
    return new Intl.NumberFormat(undefined, { style: 'currency', currency }).format(amount);
  } catch {
    // Not an ISO 4217 code. Keep both parts rather than dropping either.
    return `${currency} ${new Intl.NumberFormat().format(amount)}`;
  }
}

function isNumeric(value: unknown): boolean {
  if (typeof value === 'number') return Number.isFinite(value);
  if (typeof value === 'string' && value.trim() !== '') return Number.isFinite(Number(value));
  return false;
}

/** Scalars only, bounded. Objects and arrays return "" and are skipped. */
function scalarText(value: unknown): string {
  if (value === null || value === undefined) return '';
  if (typeof value === 'boolean') return value ? 'Yes' : 'No';
  if (typeof value === 'number') return Number.isFinite(value) ? String(value) : '';
  if (typeof value !== 'string') return '';

  const trimmed = value.trim();
  if (trimmed === '') return '';
  return trimmed.length > MAX_VALUE ? `${trimmed.slice(0, MAX_VALUE - 1)}…` : trimmed;
}
