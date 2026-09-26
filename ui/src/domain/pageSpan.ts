/**
 * Where one page of a server-paged list sits in the whole: "26–50 of 60", and
 * how many pages there are to move between.
 *
 * The decision list pages on the server, so the page on screen is all the
 * browser holds. It used to load every decision — up to a thousand full
 * tables — to show twenty-five of them.
 */
export interface PageSpan {
  pageCount: number;
  /** 1-based positions of the first and last item shown, 0 when none. */
  first: number;
  last: number;
}

export function pageSpan(page: number, size: number, total: number, shown: number): PageSpan {
  const pageCount = Math.max(1, Math.ceil(total / size));
  const first = shown > 0 ? (page - 1) * size + 1 : 0;
  return { pageCount, first, last: first === 0 ? 0 : first + shown - 1 };
}
