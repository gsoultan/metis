import { describe, expect, it } from 'bun:test';

import { pageSpan } from './pageSpan';

describe('pageSpan', () => {
  it('places a page in the whole list', () => {
    expect(pageSpan(2, 25, 60, 25)).toEqual({ pageCount: 3, first: 26, last: 50 });
    expect(pageSpan(3, 25, 60, 10)).toEqual({ pageCount: 3, first: 51, last: 60 });
  });

  it('shows nothing, as page 1 of 1, for an empty list', () => {
    expect(pageSpan(1, 25, 0, 0)).toEqual({ pageCount: 1, first: 0, last: 0 });
  });
});
