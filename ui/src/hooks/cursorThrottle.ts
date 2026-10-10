/**
 * Sends at most one cursor position per interval, always the latest one.
 *
 * The designer broadcast a POST on every mouse move — sixty or more a second
 * per person, each fanned out to every open stream in the project — and a
 * failed one was an unhandled rejection per move.
 */
export const CURSOR_BROADCAST_INTERVAL_MS = 100;

export interface CursorThrottle {
  move: (position: { x: number; y: number }) => void;
  cancel: () => void;
}

export function createCursorThrottle(
  send: (position: { x: number; y: number }) => Promise<unknown>,
  intervalMs = CURSOR_BROADCAST_INTERVAL_MS,
): CursorThrottle {
  let pending: { x: number; y: number } | null = null;
  let timer: ReturnType<typeof setTimeout> | null = null;

  const flush = () => {
    timer = null;
    if (!pending) return;
    const position = pending;
    pending = null;
    send(position).catch((error: unknown) => {
      // A cursor is ephemeral; the next move sends a newer one.
      console.debug('A cursor position was not broadcast', error);
    });
    timer = setTimeout(flush, intervalMs);
  };

  return {
    move(position) {
      pending = position;
      if (timer === null) flush();
    },
    cancel() {
      if (timer !== null) clearTimeout(timer);
      timer = null;
      pending = null;
    },
  };
}
