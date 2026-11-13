import { describe, expect, test } from 'bun:test';
import { firstSetupStep, listenAddressError, publicURLError, signInAddress, SETUP_STEPS, setupRequestFor } from './setupWizard';
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
  setup_token: 'the-token-from-the-log',
  http_address: '0.0.0.0:8080',
  public_url: 'https://metis.example.com',
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
        // Without it the server refuses: the wizard is the operator's.
        'setup_token',
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

describe('listenAddressError', () => {
  test('empty keeps the default', () => {
    expect(listenAddressError('')).toBeNull();
  });

  test('takes a port alone, or a host and a port', () => {
    for (const address of [':8080', '0.0.0.0:8080', '10.0.0.5:9000', '[::]:8080', 'metis.internal:80']) {
      expect(listenAddressError(address)).toBeNull();
    }
  });

  test('refuses what the server could not bind', () => {
    for (const address of ['8080', ':0', ':70000', ':http', 'bad host:8080']) {
      expect(listenAddressError(address)).not.toBeNull();
    }
  });
});

describe('publicURLError', () => {
  test('takes an http or https address', () => {
    expect(publicURLError('https://metis.example.com')).toBeNull();
    expect(publicURLError('http://10.0.0.5:8080')).toBeNull();
    expect(publicURLError('')).toBeNull();
  });

  test('refuses what the server refuses', () => {
    for (const url of ['metis.example.com', 'ftp://metis.example.com', 'https://metis.example.com/?a=1', 'https://u:p@metis.example.com']) {
      expect(publicURLError(url)).not.toBeNull();
    }
  });
});

describe('signInAddress', () => {
  const here = new URL('http://10.0.0.5:8080/setup');

  test('stays here when nothing about the address changed', () => {
    expect(signInAddress(here, '', '')).toBeNull();
    expect(signInAddress(here, '0.0.0.0:8080', '')).toBeNull();
    expect(signInAddress(here, '', 'http://10.0.0.5:8080/')).toBeNull();
  });

  test('goes to the public address when there is one', () => {
    expect(signInAddress(here, '', 'https://metis.example.com/')).toBe('https://metis.example.com/login');
  });

  test('follows the server to its new port', () => {
    expect(signInAddress(here, ':9000', '')).toBe('http://10.0.0.5:9000/login');
  });
});
