/**
 * One page of a list already in memory.
 *
 * The decision list loads every decision (the graph above it needs them all)
 * and shows them a page at a time, so the page stays light however many there
 * are. It used to show the first 25 and offer no way to the rest.
 */
export interface PageWindow<T> {
  items: T[];
  /** The page shown, 1-based: the one asked for, kept in range. */
  page: number;
  pageCount: number;
  /** 1-based positions of the first and last item shown, 0 when none. */
  first: number;
  last: number;
}

export function pageWindow<T>(items: T[], page: number, size: number): PageWindow<T> {
  const pageCount = Math.max(1, Math.ceil(items.length / size));
  // Derived rather than reset: a search that narrows the list leaves the page
  // number where it was, and the page shown is the last one that exists.
  const current = Math.min(Math.max(1, page), pageCount);
  const shown = items.slice((current - 1) * size, current * size);
  const first = shown.length > 0 ? (current - 1) * size + 1 : 0;
  return { items: shown, page: current, pageCount, first, last: first === 0 ? 0 : first + shown.length - 1 };
}
