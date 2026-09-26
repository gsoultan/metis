/**
 * The roles a platform account can hold, with words for each.
 *
 * These are the roles the server enforces, spelled the way it spells them.
 * They used to be five lowercase tokens — admin, manager, developer, user,
 * viewer — of which exactly one matched anything: `admin`, and only because the
 * server compares case-insensitively. Granting "developer" looked like letting
 * somebody deploy a process model and granted nothing at all.
 *
 * tests/roledrift asserts this list against the Go constants in both
 * directions, so a role added on one side and not the other fails the build
 * rather than quietly granting nothing.
 */

/** One role as the picker offers it. */
export interface RoleOption {
  value: string;
  label: string;
  description: string;
}

export const ROLE_OPTIONS: readonly RoleOption[] = [
  {
    value: 'ADMIN',
    label: 'Administrator',
    description: 'Manages accounts, organizations, projects, environments and connectors',
  },
  {
    value: 'DESIGNER',
    label: 'Designer',
    description: 'Authors and deploys process and decision models',
  },
  {
    value: 'OPERATOR',
    label: 'Operator',
    description: 'Resolves incidents, starts ad hoc tasks, broadcasts signals to running instances, and takes the tasks nobody was named for',
  },
  {
    value: 'QUERY_AUTHOR',
    label: 'Query author',
    description:
      'Deploys process models that look things up in a connected database — held beside Designer, since those steps carry their own SQL',
  },
];

/** The administrator's role: the badge that stands out, and what administrative pages ask for. */
export const PRIVILEGED_ROLE = 'ADMIN';

/** The role that authors and deploys models, and imports the people they assign work to. */
export const DESIGNER_ROLE = 'DESIGNER';

/** The role that runs the system day to day, and takes the tasks nobody was named for. */
export const OPERATOR_ROLE = 'OPERATOR';

/**
 * The word for a role token.
 *
 * Matched case-insensitively because existing accounts carry whatever case they
 * were written with — the setup seeder writes uppercase, older rows and tokens
 * may not — and a badge reading "admin" beside one reading "Administrator" is
 * two roles as far as anybody looking can tell.
 */
export function roleLabel(role: string): string {
  const trimmed = role.trim();
  const found = ROLE_OPTIONS.find((option) => option.value.toLowerCase() === trimmed.toLowerCase());
  return found?.label ?? trimmed;
}

/** Whether a role token is the privileged one, whatever case it was written in. */
export function isPrivilegedRole(role: string): boolean {
  return role.toLowerCase() === PRIVILEGED_ROLE.toLowerCase();
}

const ROLE_SEPARATOR = ',';

/** The roles in a comma-joined list, trimmed, with the empty ones dropped. */
export function splitRoles(roles: string): string[] {
  return roles
    .split(ROLE_SEPARATOR)
    .map((role) => role.trim())
    .filter((role) => role !== '');
}

/**
 * The labels for a comma-joined list of roles, or "" when there are none.
 *
 * The store holds whatever the login response carried, joined with commas, and
 * the profile page rendered that string as-is — so somebody saw "ADMIN" where
 * they expected to be told what they are.
 */
export function roleLabels(roles: string): string {
  return splitRoles(roles)
    .map((role) => roleLabel(role))
    .join(', ');
}
