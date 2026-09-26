import { describe, expect, test } from 'bun:test';
import { firstSetupStep, SETUP_STEPS, setupRequestFor } from './setupWizard';
import type { SetupRequest } from '../services/types';

const everything: SetupRequest = {
  database_driver: 'postgres',
  db_host: 'db.internal',
  db_port: 5432,
  db_username: 'metis',
  db_password: 'database-password',
  db_name: 'metis',
  db_ssl_enabled: true,
  encryption_key: 'generated-encryption-key',
  jwt_secret: 'generated-jwt-secret',
  admin_username: 'admin',
  admin_password: 'admin-password',
  admin_full_name: 'Ada Admin',
  admin_public_name: 'Ada',
  admin_email: 'ada@example.com',
  organization_name: 'Acme',
  project_name: 'Finance',
};

describe('firstSetupStep', () => {
  test('a server configured by its environment starts at the administrator', () => {
    expect(firstSetupStep(true)).toBe(SETUP_STEPS.administrator);
  });

  test('otherwise the wizard starts with the database', () => {
    expect(firstSetupStep(false)).toBe(SETUP_STEPS.database);
  });
});

describe('setupRequestFor', () => {
  test('sends only the people when the environment names the database and secrets', () => {
    const sent = setupRequestFor(everything, true);
    expect(Object.keys(sent).sort()).toEqual(
      [
        'admin_email',
        'admin_full_name',
        'admin_password',
        'admin_public_name',
        'admin_username',
        'organization_name',
        'project_name',
      ].sort(),
    );
    // The secrets the form generated on the way in stay in the browser.
    expect(JSON.stringify(sent)).not.toContain('generated-encryption-key');
    expect(JSON.stringify(sent)).not.toContain('database-password');
  });

  test('sends everything to a wizard that writes its own configuration', () => {
    expect(setupRequestFor(everything, false)).toEqual(everything);
  });
});
