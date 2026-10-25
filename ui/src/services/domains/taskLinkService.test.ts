import { describe, expect, it } from 'bun:test';
import { tokenFromHash } from './taskLinkService';

describe('tokenFromHash', () => {
  it('reads the token after the "#", where the browser never sends it', () => {
    expect(tokenFromHash('#mtl_abc-DEF_123')).toBe('mtl_abc-DEF_123');
  });

  it('is empty when the address carries none', () => {
    expect(tokenFromHash('')).toBe('');
    expect(tokenFromHash('#')).toBe('');
  });
});
