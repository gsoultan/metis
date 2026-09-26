/**
 * The values the table checks try for a column: one from every group of values
 * its cells cannot tell apart.
 *
 * If a line says `> 1000`, then 1000 and 1001 are where the interesting
 * behaviour is: a number column is sampled on and between every number its
 * cells mention, a text column with every word they name and one they do not.
 * What holds for a sample holds for everything it stands for, which is what
 * lets a check that tries a few values speak for all of them.
 */
import { ANY_VALUE, type DecisionInputColumn } from './decisionTable';

/** A value to try, and how to say it. */
export interface Sample {
  /** What the matcher sees. */
  value: string | number | boolean;
  /** What a person reads. */
  label: string;
  /**
   * Every value the cells cannot tell apart from this one, in words: "more
   * than 10 but less than 20". What holds for the sample holds for all of them.
   */
  standsFor: string;
  /** The condition that matches exactly what the sample stands for. */
  condition: string;
}

/**
 * The values worth trying for one column: one from every group of values its
 * cells cannot tell apart, so that what holds for the sample holds for the
 * group. The overlap check reads cells with the same samples.
 */
export function columnSamples(column: DecisionInputColumn, cells: string[]): Sample[] {
  if (column.type === 'boolean') {
    return [
      { value: true, label: 'yes', standsFor: 'yes', condition: 'true' },
      { value: false, label: 'no', standsFor: 'no', condition: 'false' },
    ];
  }

  if (column.type === 'number') {
    return numberSamples(thresholdsIn(cells));
  }

  // Text: every literal the table mentions, plus one value it does not, which is
  // how a missing catch-all shows up.
  const literals = new Set<string>();
  for (const cell of cells) {
    for (const part of cell.split(',')) {
      const text = part.trim().replace(/^not\(/, '').replace(/\)$/, '').trim();
      // A blank part is no value, and a bare dash is "any value". Quoted, both
      // are text: `""` is the cell menu's "Empty", which the engine matches
      // against empty text like any other text, and was dropped here as if it
      // were no value at all.
      if (text === '' || text === ANY_VALUE) continue;
      literals.add(unquote(text));
    }
  }
  const named = [...literals];
  const samples: Sample[] = named.map(textSample);
  samples.push({
    value: 'anything-else-entirely',
    label: 'anything else',
    standsFor: 'anything else',
    condition: named.length > 0 ? `not(${named.map(quoted).join(', ')})` : ANY_VALUE,
  });
  return samples;
}

/** A text value, said the way the cell menu says it: `""` is "empty". */
function textSample(value: string): Sample {
  const label = value === '' ? 'empty' : value;
  return { value, label, standsFor: label, condition: quoted(value) };
}

/**
 * Text as a cell spells it. Quoted, so that a word which happens to name a
 * process variable is still read as the word; the other kind of quote when the
 * text holds a double quote, which it can only have come from a cell quoted
 * that way.
 */
function quoted(text: string): string {
  return text.includes('"') ? `'${text}'` : `"${text}"`;
}

/** Every number a column's cells mention, lowest first. */
export function thresholdsIn(cells: string[]): number[] {
  const thresholds = new Set<number>();
  for (const cell of cells) {
    for (const match of cell.matchAll(/-?\d+(\.\d+)?/g)) {
      thresholds.add(Number(match[0]));
    }
  }
  return [...thresholds].sort((a, b) => a - b);
}

/**
 * One value from each stretch of the number line that no cell tells apart:
 * below every threshold, each threshold itself, one value strictly between
 * each pair of neighbours, and one above them all.
 *
 * Every cell this analysis reads changes its answer only at a threshold, so a
 * value from a stretch speaks for the whole stretch. The threshold itself is
 * there because that is where an off-by-one hides, the commonest gap there is.
 */
function numberSamples(thresholds: number[]): Sample[] {
  if (thresholds.length === 0) return [numberSample(-1, 'any number', ANY_VALUE)];
  const lowest = thresholds[0];
  const samples = [numberSample(lowest - 1, `less than ${lowest}`, `< ${lowest}`)];
  thresholds.forEach((threshold, index) => {
    samples.push(numberSample(threshold, String(threshold), String(threshold)));
    const next = thresholds[index + 1];
    samples.push(
      next === undefined
        ? numberSample(threshold + 1, `more than ${threshold}`, `> ${threshold}`)
        : numberSample(
            strictlyBetween(threshold, next),
            `more than ${threshold} but less than ${next}`,
            `]${threshold}..${next}[`,
          ),
    );
  });
  return samples;
}

function numberSample(value: number, standsFor: string, condition: string): Sample {
  return { value, label: String(value), standsFor, condition };
}

/**
 * The next whole number when there is room for one, which reads naturally,
 * and the midpoint when there is not. Plus one used to be tried regardless, and
 * past a threshold less than one away it skipped the stretch in between.
 */
function strictlyBetween(low: number, high: number): number {
  if (low + 1 < high) return low + 1;
  // Rounded so that 0.1 and 0.2 give 0.15 rather than 0.15000000000000002.
  return Number(((low + high) / 2).toPrecision(12));
}

function unquote(text: string): string {
  const trimmed = text.trim();
  if (trimmed.length >= 2 && (trimmed.startsWith('"') || trimmed.startsWith("'")) && trimmed.endsWith(trimmed[0])) {
    return trimmed.slice(1, -1);
  }
  return trimmed;
}
