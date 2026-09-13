/**
 * What a row in the instance list says about an instance.
 *
 * A listed instance carries an id, a status, its active nodes and a shell of
 * its definition. None of that is what a person identifies it by, so each
 * function here turns one of those fields into something they can read.
 */

/** As much of an instance as the list needs. */
export interface ListedInstance {
  id: string;
  status?: string;
  definition?: { id?: string; key?: string; name?: string };
}

/** As much of a definition as resolving a name needs. */
export interface NamedDefinition {
  id: string;
  key?: string;
  name?: string;
}

/** Shown when neither the instance nor the directory can name the process. */
export const UNNAMED_PROCESS = 'Process';

/** The name of the process an instance belongs to, resolved through its id. */
export function definitionName(instance: ListedInstance, definitions: NamedDefinition[]): string {
  const fromInstance = instance.definition?.name || instance.definition?.key;
  if (fromInstance) return fromInstance;
  const match = definitions.find((d) => d.id === instance.definition?.id);
  return match?.name || match?.key || UNNAMED_PROCESS;
}

/**
 * Turns a node identifier into something readable.
 *
 * Node IDs are authored in the designer and usually carry the step's name in
 * them — "Activity_ApproveExpense", "approve-expense", "Task_1". Splitting the
 * generated prefix and the separators recovers a usable label without needing
 * the whole definition loaded just to render a row.
 */
export function humanizeNodeId(nodeId: string): string {
  const withoutPrefix = nodeId.replace(/^(Activity|Task|Event|Gateway|Flow|Node)[_-]/i, '');
  const spaced = withoutPrefix
    .replace(/[_-]+/g, ' ')
    .replace(/([a-z])([A-Z])/g, '$1 $2')
    .trim();
  if (!spaced || /^\d+$/.test(spaced)) return nodeId;
  return spaced.charAt(0).toUpperCase() + spaced.slice(1);
}

/** The version nibble a time-ordered UUID carries. */
const UUID_V7 = '7';
/** How many hex digits of a v7 id are the millisecond timestamp. */
const V7_TIMESTAMP_HEX_DIGITS = 12;
/** How many trailing hex digits make a reference short enough to read out. */
const REFERENCE_LENGTH = 6;

/**
 * When an instance started, read out of its id.
 *
 * Every primary key here is a UUIDv7, whose first 48 bits are the creation
 * time in milliseconds. The list does not carry a started-at field, and the
 * eight-character prefix it used to print was that timestamp's most
 * significant digits — identical on every row created the same week.
 *
 * Returns null for anything that is not a v7 id rather than inventing a date.
 */
export function startedAtFromId(id: string): Date | null {
  const hex = id.replace(/-/g, '');
  if (hex.length !== 32 || hex[V7_TIMESTAMP_HEX_DIGITS] !== UUID_V7) return null;
  const millis = Number.parseInt(hex.slice(0, V7_TIMESTAMP_HEX_DIGITS), 16);
  if (Number.isNaN(millis)) return null;
  return new Date(millis);
}

/**
 * A short reference that differs between rows.
 *
 * The tail of a v7 id is random, so its last digits distinguish two instances
 * started in the same millisecond where the head cannot. Short enough to read
 * over the phone to whoever is looking at the same list.
 */
export function instanceReference(id: string): string {
  const hex = id.replace(/-/g, '');
  return `#${hex.slice(-REFERENCE_LENGTH).toUpperCase()}`;
}

/**
 * How many instances the project holds in one state.
 *
 * Counted by the server across the whole project, which is the only count worth
 * showing. The page used to derive its status filter from the rows on screen —
 * so a project of 500,000 instances with twelve failures offered no way to
 * reach them, and offered no hint they existed.
 */
export interface StatusCount {
  status: string;
  total: number;
}

/**
 * The order the states are worth looking at in.
 *
 * Operational, not alphabetical and not the order the database returns. Somebody
 * opens this page to find out whether anything needs them; what needs them goes
 * first, what is still moving next, and what is already finished last.
 */
const STATUS_PRIORITY = ['failed', 'suspended', 'active', 'completed'];

/**
 * The filter chips to offer, in the order above.
 *
 * A state with no instances in it is dropped: a chip reading "Paused 0" is a
 * control that can only ever empty the table, and four of them crowd out the
 * one number somebody came here to read.
 */
export function statusChips(counts: StatusCount[]): StatusCount[] {
  const withRows = counts.filter((count) => count.total > 0);
  return withRows.sort((a, b) => {
    const rank = (status: string) => {
      const index = STATUS_PRIORITY.indexOf(status.toLowerCase());
      // A state this build has no opinion about — written by an older version —
      // sorts after the ones it does, rather than silently first.
      return index === -1 ? STATUS_PRIORITY.length : index;
    };
    const byPriority = rank(a.status) - rank(b.status);
    return byPriority !== 0 ? byPriority : a.status.localeCompare(b.status);
  });
}

/** How many instances the project holds across every state. */
export function totalAcrossStatuses(counts: StatusCount[]): number {
  return counts.reduce((sum, count) => sum + count.total, 0);
}

/** The states that mean an instance has stopped and will not move again. */
const SETTLED = new Set(['completed', 'terminated', 'cancelled']);

/**
 * Whether an instance is still moving.
 *
 * `failed` counts as unsettled on purpose: a failed instance is not finished,
 * it is waiting for somebody to retry it, and how long it has been waiting is
 * exactly the number that should be growing on screen.
 */
export function isRunning(status?: string): boolean {
  return !SETTLED.has((status ?? '').toLowerCase());
}

const MINUTE = 60_000;
const HOUR = 60 * MINUTE;
const DAY = 24 * HOUR;

/**
 * How long something has been going, in the shortest form that is still true.
 *
 * Two units at most, largest first — "2h 14m", not "2 hours, 14 minutes and 6
 * seconds". A duration in a table column is read at a glance to compare it with
 * the row above, and precision past the second unit costs width without
 * changing any decision.
 *
 * Returns null for a negative span rather than "in 3 minutes": the start comes
 * from an id generated by the server and the end from the browser's clock, and
 * a few seconds of skew between them should read as "just now", not as the
 * future.
 */
export function describeDuration(fromMillis: number, toMillis: number): string | null {
  const span = toMillis - fromMillis;
  if (Number.isNaN(span)) return null;
  if (span < MINUTE) return span < -MINUTE ? null : 'just now';

  const days = Math.floor(span / DAY);
  const hours = Math.floor((span % DAY) / HOUR);
  const minutes = Math.floor((span % HOUR) / MINUTE);

  if (days > 0) return hours > 0 ? `${days}d ${hours}h` : `${days}d`;
  if (hours > 0) return minutes > 0 ? `${hours}h ${minutes}m` : `${hours}h`;
  return `${minutes}m`;
}
