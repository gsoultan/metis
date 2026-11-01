/**
 * What an environment needs before it can be served.
 *
 * An environment names a database and a port. Both are checked here as well as
 * on the server, for different reasons: the server's check is the one that
 * counts, and this one is what lets somebody find out before pressing Save
 * rather than after. Neither replaces the other.
 *
 * The rules are the server's, restated — a port below 1024 usually needs
 * privileges the process does not have, and a port already claimed cannot be
 * bound twice.
 */

/** The lowest port an environment can be served on. Below this needs privileges. */
export const MIN_ENVIRONMENT_PORT = 1024;
export const MAX_ENVIRONMENT_PORT = 65535;

/**
 * The database engines an environment can use.
 *
 * One, and the list stays a list: the validation below checks membership, and a
 * check that reads "is this the one allowed value" would have to be rewritten
 * rather than extended if a second ever appears.
 */
export const ENVIRONMENT_DRIVERS = ['postgres'] as const;
export type EnvironmentDriver = (typeof ENVIRONMENT_DRIVERS)[number];

/** The port the engine listens on, for pre-filling the form. */
export const DRIVER_DEFAULT_PORT: Record<EnvironmentDriver, number> = {
  postgres: 5432,
};

export interface EnvironmentConnection {
  host?: string;
  port?: number;
  username?: string;
  password?: string;
  db_name?: string;
  ssl_enabled?: boolean;
  // The server stores this as a map and each driver reads different keys from
  // it, so the named fields above are the ones this form knows about rather
  // than the only ones that may be present. A stored connection is round-tripped
  // whole; narrowing it here would silently drop whatever a driver added.
  [key: string]: unknown;
}

export interface EnvironmentDraft {
  id?: string;
  name: string;
  port: number;
  driver: string;
  connection: EnvironmentConnection;
  enabled: boolean;
  /** Production and the like: only a releaser or an administrator may change its models. */
  protected: boolean;
}

/**
 * Whether a driver needs host, port, credentials and TLS.
 *
 * Every supported one does now — SQLite was the exception, a file rather than a
 * server, and asking it for a host was asking for information with nowhere to
 * go. The function stays because the form branches on it and because an engine
 * that is not a server is the kind of thing that comes back.
 */
export function needsServerFields(driver: string): boolean {
  return ENVIRONMENT_DRIVERS.includes(driver as EnvironmentDriver);
}

/** Field-keyed problems, empty when the draft can be saved. */
export type EnvironmentErrors = Partial<Record<'name' | 'port' | 'driver' | 'host' | 'db_name', string>>;

/**
 * Checks a draft, optionally against the ports its siblings already hold.
 *
 * `takenPorts` should exclude the draft's own port when editing: keeping the
 * port you already have is not a collision, and reporting it as one makes
 * renaming an environment impossible.
 */
export function validateEnvironment(draft: EnvironmentDraft, takenPorts: number[] = []): EnvironmentErrors {
  const errors: EnvironmentErrors = {};

  if (!draft.name.trim()) {
    errors.name = 'Give this environment a name — "staging", "production".';
  } else if (draft.name.trim().length > 63) {
    errors.name = 'Names are limited to 63 characters.';
  }

  if (!ENVIRONMENT_DRIVERS.includes(draft.driver as EnvironmentDriver)) {
    errors.driver = 'Choose a database engine.';
  }

  if (!Number.isInteger(draft.port) || draft.port < MIN_ENVIRONMENT_PORT || draft.port > MAX_ENVIRONMENT_PORT) {
    errors.port = `Pick a port between ${MIN_ENVIRONMENT_PORT} and ${MAX_ENVIRONMENT_PORT}.`;
  } else if (takenPorts.includes(draft.port)) {
    errors.port = 'Another environment is already served on this port.';
  }

  if (needsServerFields(draft.driver)) {
    if (!draft.connection.host?.trim()) {
      errors.host = 'Where does this database live?';
    }
    if (!draft.connection.db_name?.trim()) {
      errors.db_name = 'Which database on that server?';
    }
  }

  return errors;
}

export function canSaveEnvironment(draft: EnvironmentDraft, takenPorts: number[] = []): boolean {
  return Object.keys(validateEnvironment(draft, takenPorts)).length === 0;
}

/**
 * The ports a draft would collide with — every sibling's, minus its own.
 *
 * Its own is excluded by id rather than by value so that an environment being
 * edited does not report a collision with itself, which would make every field
 * on it unsaveable.
 */
export function portsTakenByOthers(
  existing: Array<{ id: string; port: number }>,
  draftID?: string,
): number[] {
  return existing.filter((e) => e.id !== draftID).map((e) => e.port);
}

/**
 * A blank environment, pre-filled for the engine chosen.
 *
 * The serving port is left at zero rather than guessed: it is the address a
 * person will type into a browser, and a plausible-looking default is one
 * nobody checks until two environments collide.
 */
export function emptyEnvironment(driver: EnvironmentDriver = 'postgres'): EnvironmentDraft {
  return {
    name: '',
    port: 0,
    driver,
    connection: needsServerFields(driver)
      ? { host: 'localhost', port: DRIVER_DEFAULT_PORT[driver], username: '', password: '', db_name: '', ssl_enabled: false }
      : { db_name: '' },
    enabled: true,
    protected: false,
  };
}
