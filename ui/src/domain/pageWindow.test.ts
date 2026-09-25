import { describe, expect, it } from 'bun:test';

import { pageWindow } from './pageWindow';

const thirty = Array.from({ length: 30 }, (_, i) => i + 1);

/**
 * The decision list showed its first 25 decisions and offered no way to the
 * rest; searching only searched those 25.
 */
describe('pageWindow', () => {
  it('reaches every item, a page at a time', () => {
    expect(pageWindow(thirty, 2, 25)).toEqual({ items: [26, 27, 28, 29, 30], page: 2, pageCount: 2, first: 26, last: 30 });
  });

  it('keeps a page that no longer exists in range, as after a search narrows the list', () => {
    expect(pageWindow(thirty.slice(0, 3), 2, 25)).toEqual({ items: [1, 2, 3], page: 1, pageCount: 1, first: 1, last: 3 });
  });

  it('shows nothing, as page 1 of 1, for an empty list', () => {
    expect(pageWindow([], 1, 25)).toEqual({ items: [], page: 1, pageCount: 1, first: 0, last: 0 });
  });
});
