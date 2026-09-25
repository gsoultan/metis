/**
 * Every page of a paged list, up to a bound.
 *
 * Some views need the whole list and not a page of it: the decision graph
 * has to see every decision to say which ones another requires and cannot
 * find, and to find a loop. It was drawn from the first page, 25 decisions, so
 * past those it reported dependencies as missing and missed loops through
 * them.
 *
 * Bounded, because the list is only paged in the first place so that nobody
 * loads all of an unbounded table. Past the bound the result says so, and the
 * view says what that means, rather than drawing a partial picture as whole.
 */

/** One page of a paged list, as much of it as collecting needs. */
export interface ListPage<T> {
  items: T[];
  hasMore: boolean;
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
  maxPages: number,
): Promise<CollectedPages<T>> {
  const items: T[] = [];
  let last: ListPage<T> = { items: [], hasMore: true, total: 0 };
  for (let page = 1; page <= maxPages && last.hasMore; page += 1) {
    last = await fetchPage(page);
    items.push(...last.items);
    // An empty page that claims more would be asked for again, to the bound.
    if (last.items.length === 0) break;
  }
  return { items, truncated: last.hasMore, total: last.total };
}
