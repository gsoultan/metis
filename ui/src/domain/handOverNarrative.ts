/**
 * A task changing hands, or being edited, as a sentence in the reader's
 * language.
 *
 * The server stores a sentence with each audit entry, in English. For a
 * hand-over the entry also says who did it, who had the task, who has it now
 * and why, so the same sentence can be said through the catalogues. Anything
 * this cannot say, whether another kind of entry or one written before the
 * trail kept who acted, returns null, and the timeline shows what the server
 * stored.
 */
import type { Values } from '../i18n/translate';

type Translate = (key: string, values?: Values) => string;

/** The fields an edit can change, in the order a sentence names them. */
const EDITED_FIELDS = ['name', 'priority', 'due_date'] as const;

function text(data: Record<string, unknown> | undefined, key: string): string {
  const value = data?.[key];
  return typeof value === 'string' ? value : '';
}

/** "a", "a and b", "a, b and c", with the catalogue's word for "and". */
function inWords(items: string[], t: Translate): string {
  if (items.length < 2) return items.join('');
  return t('timeline.lastOf', { rest: items.slice(0, -1).join(', '), last: items[items.length - 1] });
}

function editedFields(data: Record<string, unknown> | undefined, t: Translate): string[] {
  const changes = data?.changes;
  if (!changes || typeof changes !== 'object' || Array.isArray(changes)) return [];
  const changed = changes as Record<string, unknown>;
  return EDITED_FIELDS.filter((field) => Object.hasOwn(changed, field)).map((field) => t(`timeline.field.${field}`));
}

/** The sentence without what follows it: null when the entry cannot be told. */
function moved(type: string, data: Record<string, unknown> | undefined, task: string, t: Translate): string | null {
  const actor = text(data, 'actor');
  if (!actor || !task) return null;
  const previous = text(data, 'previous_holder');
  const target = text(data, 'target');
  const values = { actor, task, previous, target };
  // Whoever had the task is named only when it was not the one who acted.
  const forSomebody = previous !== '' && previous !== actor;

  switch (type) {
    case 'task_assigned':
      if (!target) return null;
      return t(previous ? 'timeline.reassigned' : 'timeline.assigned', values);
    case 'task_delegated':
      if (!target) return null;
      return t(forSomebody ? 'timeline.delegatedFor' : 'timeline.delegated', values);
    case 'task_resolved':
      if (!target) return null;
      return t(forSomebody ? 'timeline.handedBackFor' : 'timeline.handedBack', values);
    case 'task_unclaimed':
      if (!previous) return null;
      return t(forSomebody ? 'timeline.releasedFor' : 'timeline.released', values);
    case 'task_edited': {
      const fields = editedFields(data, t);
      return fields.length > 0 ? t('timeline.edited', { actor, task, fields: inWords(fields, t) }) : null;
    }
    default:
      return null;
  }
}

/** A hand-over told in two parts: who did what to whom, and the reason typed with it. */
export interface HandOverParts {
  sentence: string;
  /** What somebody typed, in whatever language they typed it; '' when none. */
  reason: string;
}

export function handOverParts(
  type: string,
  data: Record<string, unknown> | undefined,
  task: string,
  t: Translate,
): HandOverParts | null {
  let sentence = moved(type, data, task, t);
  if (sentence === null) return null;
  if (data?.candidate_override === true) {
    sentence = t('timeline.notOffered', { sentence });
  }
  return { sentence, reason: text(data, 'reason') };
}

/** The same sentence with the reason after a colon, as the server stores it. */
export function describeHandOver(
  type: string,
  data: Record<string, unknown> | undefined,
  task: string,
  t: Translate,
): string | null {
  const parts = handOverParts(type, data, task, t);
  if (parts === null) return null;
  return parts.reason ? `${parts.sentence}: ${parts.reason}` : parts.sentence;
}
