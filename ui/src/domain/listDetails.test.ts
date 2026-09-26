import { describe, expect, it } from 'bun:test';

import { listDetails } from './listDetails';

/**
 * Expert mode adds a list item's ID; it never takes another line's place.
 *
 * The project list showed each project's ID instead of its organization, so
 * turning expert mode on hid which organization every project was in.
 */
describe('listDetails', () => {
  it('shows what the item belongs to in basic mode', () => {
    expect(listDetails(false, 'Acme', 'p-1')).toEqual({ detail: 'Acme' });
  });

  it('adds the ID in expert mode, keeping what it belongs to', () => {
    expect(listDetails(true, 'Acme', 'p-1')).toEqual({ detail: 'Acme', id: 'p-1' });
  });

  it('shows no empty line when the item belongs to nothing named', () => {
    expect(listDetails(false, undefined, 'p-1')).toEqual({});
    expect(listDetails(false, '', 'p-1')).toEqual({});
    expect(listDetails(true, '', 'p-1')).toEqual({ id: 'p-1' });
  });
});
