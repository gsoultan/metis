/**
 * Who a step can be given to: the People of its project and their groups.
 *
 * People do the work processes give; Users run the platform. So the picker
 * lists the project's People directory, not the organization's accounts, and
 * a name a step carries that is no person or group of the project is said
 * before the process is deployed rather than discovered when nobody can take
 * the task.
 */

import type { ComboboxItem } from '@mantine/core';
import type { Participant } from './participantImport';

export interface DirectoryGroup {
  id: string;
  name: string;
}

/** What the checks know of the directory: the names a step may use. */
export interface AssignmentDirectory {
  people: ReadonlySet<string>;
  groups: ReadonlySet<string>;
}

/** The directory as the checks read it. A group is matched by name or id, as the server matches it. */
export function directoryOf(people: Participant[], groups: DirectoryGroup[]): AssignmentDirectory {
  return {
    people: new Set(people.filter((p) => p.active).map((p) => p.username)),
    groups: new Set(groups.flatMap((g) => [g.name, g.id])),
  };
}

/**
 * The picker's list of people: the active People of the project, then any name
 * the step already carries that is not one of them, said to be so rather than
 * dropped. A select whose value is not among its options shows nothing, and a
 * step that looked unassigned would be saved unassigned.
 */
export function peopleOptions(people: Participant[], current: string[]): ComboboxItem[] {
  const options = people
    .filter((p) => p.active)
    .map((p) => ({ value: p.username, label: p.display_name ? `${p.display_name} (${p.username})` : p.username }));
  return withCurrent(options, current);
}

/** The picker's list of teams: the project's People groups, then any the step carries that is not one. */
export function groupOptions(groups: DirectoryGroup[], current: string[]): ComboboxItem[] {
  const options = groups.map((g) => ({ value: g.name, label: g.name }));
  return withCurrent(options, current, groups.map((g) => g.id));
}

function withCurrent(options: ComboboxItem[], current: string[], alsoKnown: string[] = []): ComboboxItem[] {
  const known = new Set([...options.map((o) => o.value), ...alsoKnown]);
  const unknown = current
    .filter((name) => name.trim() !== '' && !known.has(name))
    .map((name) => ({ value: name, label: `${name} (not in this project's People)` }));
  return [...options, ...unknown];
}

/**
 * Whether a name is worked out when the step runs — `${initiator}`, `#{owner}`
 * — rather than written down. Those cannot be checked against the directory.
 */
export function isExpression(name: string): boolean {
  return /[$#]\{/.test(name);
}
