/**
 * Every page of a paged list, up to a bound.
 *
 * Some views need the whole list and not a page of it: the decision graph
 * has to see every decision to say which ones another requires and cannot
 * find, and to find a loop. It was drawn from the first page, 25 decisions, so
 * past those it reported dependencies as missing and missed loops through
 * them.
 *
 * The first page says how many there are; the rest are asked for together.
 * They used to be asked for one after another, each waiting for the one
 * before, so a view waited for five round trips in a row before it showed
 * anything.
 *
 * Bounded, because the list is only paged in the first place so that nobody
 * loads all of an unbounded table. Past the bound the result says so, and the
 * view says what that means, rather than drawing a partial picture as whole.
 */

/** One page of a paged list, as much of it as collecting needs. */
export interface ListPage<T> {
  items: T[];
  /** How many the server counted in all. */
  total: number;
}

export interface CollectedPages<T> {
  items: T[];
  /** True when collecting stopped before the server ran out. */
  truncated: boolean;
  /** How many the server counted in all. */
  total: number;
}

export async function collectPages<T>(
  fetchPage: (page: number) => Promise<ListPage<T>>,
  pageSize: number,
  maxPages: number,
): Promise<CollectedPages<T>> {
  const first = await fetchPage(1);
  const pageCount = Math.min(maxPages, Math.ceil(first.total / pageSize));
  const rest = await Promise.all(
    Array.from({ length: Math.max(0, pageCount - 1) }, (_, index) => fetchPage(index + 2)),
  );
  const items = [first, ...rest].flatMap((page) => page.items);
  return { items, truncated: items.length < first.total, total: first.total };
}
