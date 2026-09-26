/** A step's loop settings, told in a sentence the way the engine reads them. */

import { asNumber, asText } from '../types/bpmn';

const PACE: ReadonlyMap<string, string> = new Map([
  ['parallel', 'all at the same time'],
  ['sequential', 'one after another'],
]);

/**
 * What a step's loop settings make it do, in a sentence, or undefined when it
 * runs once.
 *
 * Read the way the engine reads them. A step is set to repeat by any
 * multi-instance type except an empty one and "none". The engine runs two
 * kinds, all at the same time and one after another. It refuses any other
 * kind at deploy, and fails a version stored before it did when it is
 * started, before it looks for anything to go through, so such a step does
 * not run at all. A list to go through wins over a fixed count. A completion
 * condition, once set, is what ends the loop, in place of every run having
 * finished.
 */
export function loopSummary(data: Record<string, unknown>): string | undefined {
  const type = asText(data.multiInstanceType);
  if (type === '' || type === 'none') return undefined;

  const pace = PACE.get(type);
  if (pace === undefined) {
    return `Set to repeat as "${type}", a kind of repeat that cannot run. ` +
      'Deploying this process will be refused, and a version already deployed with it fails to start.';
  }

  const collection = asText(data.collection);
  const count = asNumber(data.loopCardinality);
  const times = collection !== '' ? `once for each item in ${collection}` : timesFor(count);
  if (times === undefined) return 'Set to repeat, but it names no list and no count, so it runs once.';

  const sentences = [`Runs ${times}, ${pace}.`];
  const itemName = asText(data.elementVariable);
  if (collection !== '' && itemName !== '') sentences.push(`Each run sees its item as ${itemName}.`);
  const doneWhen = asText(data.completionCondition);
  if (doneWhen !== '') sentences.push(`Moves on once this is true: ${doneWhen}.`);
  return sentences.join(' ');
}

function timesFor(count: number): string | undefined {
  if (count <= 0) return undefined;
  return count === 1 ? 'once' : `${count} times`;
}
