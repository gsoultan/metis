import type { SetupRequest } from '../services/types';

/** The wizard's steps, in the order they are shown. */
export const SETUP_STEPS = {
  database: 0,
  security: 1,
  administrator: 2,
  organization: 3,
  project: 4,
  done: 5,
} as const;

/** The part of setup only a person can answer: who runs this, and for whom. */
export type SetupPeople = Pick<
  SetupRequest,
  | 'admin_username'
  | 'admin_password'
  | 'admin_full_name'
  | 'admin_public_name'
  | 'admin_email'
  | 'organization_name'
  | 'project_name'
>;

/**
 * Where the wizard starts.
 *
 * A server started from DATABASE_URL, ENCRYPTION_KEY and JWT_SECRET already
 * has its database and both secrets. Asking for them again invited somebody to
 * type in different ones — which the wizard then tried to write to a
 * config.yaml the container's read-only root could not hold, so the first run
 * never finished.
 */
export function firstSetupStep(configuredByEnvironment: boolean): number {
  return configuredByEnvironment ? SETUP_STEPS.administrator : SETUP_STEPS.database;
}

/**
 * What the wizard sends.
 *
 * Only the people when the environment has said the rest: the server ignores a
 * database and secrets it did not ask for, and a generated key that was never
 * going to be used has no business crossing the network.
 */
export function setupRequestFor(values: SetupRequest, configuredByEnvironment: boolean): SetupPeople | SetupRequest {
  if (!configuredByEnvironment) {
    return values;
  }
  return {
    admin_username: values.admin_username,
    admin_password: values.admin_password,
    admin_full_name: values.admin_full_name,
    admin_public_name: values.admin_public_name,
    admin_email: values.admin_email,
    organization_name: values.organization_name,
    project_name: values.project_name,
  };
}
