import { describe, expect, it } from 'bun:test';

import {
  canSaveEnvironment,
  emptyEnvironment,
  needsServerFields,
  portsTakenByOthers,
  validateEnvironment,
  type EnvironmentDraft,
} from './environmentForm';

const draft = (over: Partial<EnvironmentDraft> = {}): EnvironmentDraft => ({
  name: 'staging',
  port: 8081,
  driver: 'postgres',
  connection: { host: 'db.internal', port: 5432, username: 'metis', password: 'x', db_name: 'metis_staging' },
  enabled: true,
  protected: false,
  ...over,
});

describe('validateEnvironment', () => {
  it('accepts a complete draft', () => {
    expect(validateEnvironment(draft())).toEqual({});
  });

  it('needs a name', () => {
    expect(validateEnvironment(draft({ name: '   ' })).name).toBeTruthy();
  });

  it('refuses a privileged port, which the process cannot bind', () => {
    expect(validateEnvironment(draft({ port: 80 })).port).toBeTruthy();
  });

  it('refuses a port outside the range', () => {
    expect(validateEnvironment(draft({ port: 70000 })).port).toBeTruthy();
  });

  it('refuses a port a sibling already holds', () => {
    expect(validateEnvironment(draft(), [8081]).port).toBe(
      'Another environment is already served on this port.',
    );
  });

  it('refuses an engine it cannot open', () => {
    expect(validateEnvironment(draft({ driver: 'oracle' })).driver).toBeTruthy();
  });

  it('needs a host and database name for a server engine', () => {
    const errors = validateEnvironment(draft({ connection: {} }));
    expect(errors.host).toBeTruthy();
    expect(errors.db_name).toBeTruthy();
  });

  it('refuses an engine this does not run on', () => {
    // A stored environment can still name one: installations that predate the
    // move to PostgreSQL have rows saying sqlite or mysql, and the form has to
    // say so rather than accept a save that the server will refuse.
    const errors = validateEnvironment(draft({ driver: 'sqlite', connection: { db_name: 'staging.db' } }));
    expect(errors.driver).toBeTruthy();
  });
});

describe('needsServerFields', () => {
  it('is true for the engine this runs on and false for anything else', () => {
    expect(needsServerFields('postgres')).toBe(true);
    expect(needsServerFields('sqlite')).toBe(false);
    expect(needsServerFields('mysql')).toBe(false);
  });
});

describe('portsTakenByOthers', () => {
  const existing = [
    { id: 'a', port: 8081 },
    { id: 'b', port: 8082 },
  ];

  it('excludes the environment being edited, which does not collide with itself', () => {
    expect(portsTakenByOthers(existing, 'a')).toEqual([8082]);
  });

  it('includes every port when creating a new one', () => {
    expect(portsTakenByOthers(existing)).toEqual([8081, 8082]);
  });
});

describe('canSaveEnvironment', () => {
  it('is true only when nothing is wrong', () => {
    expect(canSaveEnvironment(draft())).toBe(true);
    expect(canSaveEnvironment(draft({ port: 0 }))).toBe(false);
  });
});

describe('emptyEnvironment', () => {
  it('leaves the serving port unset rather than guessing an address', () => {
    // A plausible default is one nobody checks until two environments collide.
    expect(emptyEnvironment('postgres').port).toBe(0);
    expect(canSaveEnvironment(emptyEnvironment('postgres'))).toBe(false);
  });

  it("pre-fills the engine's port, which is not the port it is served on", () => {
    expect(emptyEnvironment('postgres').connection.port).toBe(5432);
    expect(emptyEnvironment('postgres').port).not.toBe(5432);
  });
});
