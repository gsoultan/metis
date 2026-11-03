/**
 * How an organization is named and summed up on its own page and in lists.
 * Pure, so the wording is tested once rather than read off a screenshot.
 */
import { roleLabel } from './roles';

/** Up to two letters standing for an organization: the first of its first two words. */
export function organizationInitials(name: string): string {
  const words = name.trim().split(/\s+/).filter(Boolean);
  if (words.length === 0) return '?';
  const letters = words.length === 1 ? words[0].slice(0, 2) : words[0][0] + words[1][0];
  return letters.toUpperCase();
}

function count(n: number, one: string, many: string): string {
  return `${n} ${n === 1 ? one : many}`;
}

/**
 * The line under an organization's name: how many people and projects it has
 * and what the person looking may do there. A count not yet read is left out
 * rather than shown as zero, which would say the organization is empty.
 */
export function organizationSummary(members: number | undefined, projects: number | undefined, roles: readonly string[]): string {
  const parts: string[] = [];
  if (members !== undefined) parts.push(count(members, 'member', 'members'));
  if (projects !== undefined) parts.push(count(projects, 'project', 'projects'));
  const held = [...new Set(roles.map(roleLabel))];
  parts.push(held.length > 0 ? `you are ${held.join(', ')}` : 'you hold no role here');
  return parts.join(' · ');
}
