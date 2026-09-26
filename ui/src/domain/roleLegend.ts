/**
 * What a role is required for, grouped for reading.
 *
 * The server reads each role's actions from the gates that enforce them and
 * sends them already ordered, area by area. This groups them under one heading
 * per area, in the order they came, and names the catalogue key for each
 * heading and each action — so the legend beside a role says, in the
 * interface's language, what the server will let its holders do, and nothing
 * it was only ever said to.
 */
import type { ApiRoleAccess, ApiRoleAction } from '../services/domains/roleService';

/** One heading of a legend and the actions under it. */
export interface LegendArea {
  area: string;
  actions: ApiRoleAction[];
}

/**
 * The legend for one role, or undefined when the server did not list the role.
 *
 * Matched case-insensitively, as the server matches roles: an account written
 * by an older picker holds "admin", and it is the same role.
 */
export function legendFor(access: readonly ApiRoleAccess[], role: string): LegendArea[] | undefined {
  const wanted = role.trim().toLowerCase();
  const entry = access.find((candidate) => candidate.role.trim().toLowerCase() === wanted);
  return entry ? groupByArea(entry.actions) : undefined;
}

function groupByArea(actions: readonly ApiRoleAction[]): LegendArea[] {
  const areas = new Map<string, ApiRoleAction[]>();
  for (const action of actions) {
    const listed = areas.get(action.area);
    if (listed) listed.push(action);
    else areas.set(action.area, [action]);
  }
  return [...areas].map(([area, listed]) => ({ area, actions: listed }));
}

/** The area an action the server could not place is shown under. */
export const UNPLACED_AREA = 'other';

/** The catalogue key for an area's heading. */
export function areaHeadingKey(area: string): string {
  return `access.area.${area.trim() === '' ? UNPLACED_AREA : area}`;
}

/** The catalogue key for an action's words: the method the gate was built with. */
export function actionLabelKey(method: string): string {
  return `access.action.${method}`;
}

/**
 * An action in the interface's language.
 *
 * The server words each action from its method name, in English. The
 * catalogues word it by the same name; for a method they do not know yet — a
 * gate added since they were written — the server's words stand, because the
 * alternative is the bare key, and a legend listing "access.action.X" says
 * less than one in the wrong language. A missing key is recognised the way
 * the translator reports it, by giving the key back.
 */
export function actionLabel(action: ApiRoleAction, t: (key: string) => string): string {
  const key = actionLabelKey(action.method);
  const words = t(key);
  if (words !== key) return words;
  return action.label || action.method;
}
