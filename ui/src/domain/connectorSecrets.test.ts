import { describe, expect, it } from 'bun:test';

import { displayValue, isSensitiveKey, isUnchanged, UNCHANGED_SECRET, unchangedKeys } from './connectorSecrets';

describe('connector secrets', () => {
  it('recognises the keys the server masks', () => {
    for (const key of ['api_key', 'apiKey', 'client_secret', 'password', 'bot_token', 'signing_key']) {
      expect(isSensitiveKey(key)).toBe(true);
    }
    expect(isSensitiveKey('base_url')).toBe(false);
    expect(isSensitiveKey('channel')).toBe(false);
  });

  it('recognises the fragments this list used to be missing', () => {
    // The server masked these and the form drew them as plain text.
    for (const key of ['smtp_passwd', 'credentials', 'private_key', 'signature']) {
      expect(isSensitiveKey(key)).toBe(true);
    }
  });

  it('treats a connection string as a secret however it is spelled', () => {
    // A database connection string carries the password to the whole database.
    for (const key of ['dsn', 'DSN', 'connection_string', 'connectionString', 'Connection-String', 'conn_string', 'connection_uri']) {
      expect(isSensitiveKey(key)).toBe(true);
    }
  });

  it('treats a webhook URL as a secret', () => {
    // Slack, Discord and Teams accept a post from anybody holding the URL.
    expect(isSensitiveKey('webhook_url')).toBe(true);
    expect(isSensitiveKey('webhookUrl')).toBe(true);
  });

  it('leaves the database lookup settings a person needs to read', () => {
    for (const key of ['driver', 'statement_timeout_ms', 'max_rows', 'max_result_bytes']) {
      expect(isSensitiveKey(key)).toBe(false);
    }
  });

  it('never displays the sentinel as though it were the value', () => {
    // The bug: the edit form pre-filled every field from instance.config, so
    // once the server started sending "__unchanged__" for secrets, that word
    // would have appeared in the password box and been re-sent on test.
    expect(displayValue(UNCHANGED_SECRET)).toBe('');
    expect(displayValue('hunter2')).toBe('hunter2');
    expect(displayValue(undefined)).toBe('');
    expect(displayValue(42)).toBe('42');
  });

  it('tells the sentinel from a real value', () => {
    expect(isUnchanged(UNCHANGED_SECRET)).toBe(true);
    expect(isUnchanged('__unchanged__ ')).toBe(false);
    expect(isUnchanged('')).toBe(false);
  });

  it('names the keys a test run would send the sentinel for', () => {
    const config = { base_url: 'https://x', api_key: UNCHANGED_SECRET, token: UNCHANGED_SECRET, channel: 'ops' };
    expect(unchangedKeys(config)).toEqual(['api_key', 'token']);
    expect(unchangedKeys(undefined)).toEqual([]);
  });
});
