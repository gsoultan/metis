import { describe, expect, it } from 'bun:test';

import { collectPages, type ListPage } from './allPages';

/** A server holding `all`, answering `size` at a time, that records what it was asked. */
function serverOf<T>(all: T[], size: number) {
  const asked: number[] = [];
  const fetchPage = async (page: number): Promise<ListPage<T>> => {
    asked.push(page);
    return { items: all.slice((page - 1) * size, page * size), hasMore: page * size < all.length, total: all.length };
  };
  return { fetchPage, asked };
}

/**
 * The decision graph was drawn from the first page of the list, 25 decisions.
 * A decision past those that another one requires showed as missing, and a
 * loop through one of them was never found.
 */
describe('collectPages', () => {
  it('reads every page until the server says there are no more', async () => {
    const { fetchPage, asked } = serverOf([1, 2, 3, 4, 5], 2);
    expect(await collectPages(fetchPage, 10)).toEqual({ items: [1, 2, 3, 4, 5], truncated: false, total: 5 });
    expect(asked).toEqual([1, 2, 3]);
  });

  it('stops at its bound, and says it stopped short', async () => {
    const { fetchPage, asked } = serverOf([1, 2, 3, 4, 5], 2);
    expect(await collectPages(fetchPage, 2)).toEqual({ items: [1, 2, 3, 4], truncated: true, total: 5 });
    expect(asked).toEqual([1, 2]);
  });

  it('stops at an empty page, whatever the server says about more', async () => {
    const fetchPage = async (): Promise<ListPage<number>> => ({ items: [], hasMore: true, total: 3 });
    expect(await collectPages(fetchPage, 10)).toEqual({ items: [], truncated: true, total: 3 });
  });

  it('reads one page when that is all there is', async () => {
    const { fetchPage, asked } = serverOf([1, 2], 25);
    expect(await collectPages(fetchPage, 10)).toEqual({ items: [1, 2], truncated: false, total: 2 });
    expect(asked).toEqual([1]);
  });
});
