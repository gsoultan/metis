import type { SetupRequest } from '../services/types';

/** The wizard's steps, in the order they are shown. */
export const SETUP_STEPS = {
  database: 0,
  security: 1,
  server: 2,
  administrator: 3,
  organization: 4,
  project: 5,
  done: 6,
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
  | 'setup_token'
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
    setup_token: values.setup_token,
  };
}

/**
 * What is wrong with a listen address, or null when the server will take it.
 *
 * The same rule the server applies before writing config.yaml: an optional
 * host, a colon, and a port from 1 to 65535. Empty is allowed and keeps the
 * default, every interface on port 8080.
 */
export function listenAddressError(address: string): string | null {
  const value = address.trim();
  if (value === '') return null;
  const colon = value.lastIndexOf(':');
  if (colon < 0) return 'Use host:port, such as :8080 or 0.0.0.0:8080';
  const host = value.slice(0, colon);
  const port = value.slice(colon + 1);
  if (!/^\d+$/.test(port) || Number(port) < 1 || Number(port) > 65535) {
    return 'The port must be a number from 1 to 65535';
  }
  if (/[\s/]/.test(host)) return 'The host must be a host name or an IP address';
  return null;
}

/** What is wrong with a public address, or null when it is usable or empty. */
export function publicURLError(publicURL: string): string | null {
  const value = publicURL.trim();
  if (value === '') return null;
  let parsed: URL;
  try {
    parsed = new URL(value);
  } catch {
    return 'Enter a full address, such as https://metis.example.com';
  }
  if (parsed.protocol !== 'http:' && parsed.protocol !== 'https:') {
    return 'The address must start with http:// or https://';
  }
  if (parsed.search || parsed.hash || parsed.username || parsed.password) {
    return 'The address must have no query, fragment or credentials';
  }
  return null;
}

/**
 * Where to sign in once a wizard-only server has started on its new
 * configuration, or null to stay on this address.
 *
 * The public address when one was given. Otherwise this page's host on the
 * new port, if the port changed: the wizard's own address stops answering the
 * moment the server moves.
 */
export function signInAddress(current: URL, httpAddress: string, publicURL: string): string | null {
  const publicAddress = publicURL.trim().replace(/\/+$/, '');
  if (publicAddress !== '' && publicAddress !== current.origin) {
    return `${publicAddress}/login`;
  }
  const value = httpAddress.trim();
  const port = value.slice(value.lastIndexOf(':') + 1);
  const currentPort = current.port || (current.protocol === 'https:' ? '443' : '80');
  if (value === '' || port === currentPort) {
    return null;
  }
  return `${current.protocol}//${current.hostname}:${port}/login`;
}
