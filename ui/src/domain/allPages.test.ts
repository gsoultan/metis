import { describe, expect, it } from 'bun:test';

import { collectPages, type ListPage } from './allPages';

/**
 * A server holding `all`, answering `size` at a time. Every page after the
 * first is held until the test lets it go, so the test can see what was asked
 * for before any of it arrived.
 */
function serverOf<T>(all: T[], size: number) {
  const asked: number[] = [];
  const held: (() => void)[] = [];
  const pageOf = (page: number): ListPage<T> & { hasMore: boolean } => ({
    items: all.slice((page - 1) * size, page * size),
    total: all.length,
    hasMore: page * size < all.length,
  });
  const fetchPage = (page: number): Promise<ListPage<T>> => {
    asked.push(page);
    if (page === 1) return Promise.resolve(pageOf(1));
    return new Promise((resolve) => held.push(() => resolve(pageOf(page))));
  };
  const releaseAll = () => held.splice(0).forEach((release) => release());
  return { fetchPage, asked, releaseAll };
}

/** Lets every promise that can settle, settle. */
const settle = () => new Promise((resolve) => setTimeout(resolve, 0));

/**
 * The decision graph was drawn from the first page of the list, 25 decisions.
 * A decision past those that another one requires showed as missing, and a
 * loop through one of them was never found. The pages that fixed that were
 * then asked for one at a time, each waiting for the one before.
 */
describe('collectPages', () => {
  it('asks for every page after the first at once, not one after another', async () => {
    const server = serverOf([1, 2, 3, 4, 5], 2);
    const collecting = collectPages(server.fetchPage, 2, 10);
    await settle();
    expect(server.asked).toEqual([1, 2, 3]);
    server.releaseAll();
    expect(await collecting).toEqual({ items: [1, 2, 3, 4, 5], truncated: false, total: 5 });
  });

  it('stops at its bound, and says it stopped short', async () => {
    const server = serverOf([1, 2, 3, 4, 5], 2);
    const collecting = collectPages(server.fetchPage, 2, 2);
    await settle();
    server.releaseAll();
    expect(await collecting).toEqual({ items: [1, 2, 3, 4], truncated: true, total: 5 });
    expect(server.asked).toEqual([1, 2]);
  });

  it('reads one page when that is all there is', async () => {
    const server = serverOf([1, 2], 25);
    expect(await collectPages(server.fetchPage, 25, 10)).toEqual({ items: [1, 2], truncated: false, total: 2 });
    expect(server.asked).toEqual([1]);
  });

  it('reads one page of nothing for an empty list', async () => {
    const server = serverOf<number>([], 25);
    expect(await collectPages(server.fetchPage, 25, 10)).toEqual({ items: [], truncated: false, total: 0 });
    expect(server.asked).toEqual([1]);
  });
});
