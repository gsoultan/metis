import type { DirectoryGap } from '../services/domains/participantService';

/** Whether a gap can be filled from the organization: an account, or a group. */
export function fillable(gap: DirectoryGap): boolean {
  return gap.kind === 'person' ? !!gap.has_account : !!gap.has_platform_group;
}

/** The names to send to fill every gap that can be filled. */
export function fillableNames(gaps: readonly DirectoryGap[]): { people: string[]; groups: string[] } {
  const people: string[] = [];
  const groups: string[] = [];
  for (const gap of gaps) {
    if (!fillable(gap)) continue;
    (gap.kind === 'person' ? people : groups).push(gap.name);
  }
  return { people, groups };
}

/** Where a gap is used, in a few words. */
export function usedBy(gap: DirectoryGap): string {
  const parts: string[] = [];
  if (gap.open_tasks > 0) parts.push(`${gap.open_tasks} open ${gap.open_tasks === 1 ? 'task' : 'tasks'}`);
  const models = gap.models ?? [];
  if (models.length > 0) parts.push(`${models.length === 1 ? 'model' : 'models'} ${models.join(', ')}`);
  return parts.join(' · ');
}

/** What filling a gap would make it from, or why it cannot be. */
export function fillsFrom(gap: DirectoryGap): string {
  if (gap.kind === 'person') {
    return gap.has_account
      ? `From the User ${gap.display_name ? `${gap.display_name} ` : ''}(${gap.name})${gap.email ? `, ${gap.email}` : ''}`
      : 'No User of this name in the organization: import them';
  }
  return gap.has_platform_group
    ? `From the organization's group, with ${(gap.members ?? []).join(', ') || 'no members'}`
    : 'No group of this name in the organization: import it';
}
