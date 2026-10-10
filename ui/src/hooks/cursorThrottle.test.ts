import { describe, expect, it } from 'bun:test';

import { createCursorThrottle } from './cursorThrottle';

describe('the cursor broadcast', () => {
  it('sends the first move at once, then at most one per interval, and the latest', async () => {
    const sent: Array<{ x: number; y: number }> = [];
    const throttle = createCursorThrottle(async (p) => { sent.push(p); }, 30);

    for (let i = 0; i < 100; i++) throttle.move({ x: i, y: i });
    expect(sent).toEqual([{ x: 0, y: 0 }]);

    await new Promise((resolve) => setTimeout(resolve, 60));
    expect(sent.length).toBe(2);
    expect(sent[1]).toEqual({ x: 99, y: 99 });
    throttle.cancel();
  });

  it('does not let a failed send escape as an unhandled rejection', async () => {
    const throttle = createCursorThrottle(() => Promise.reject(new Error('offline')), 10);
    throttle.move({ x: 1, y: 1 });
    await new Promise((resolve) => setTimeout(resolve, 20));
    throttle.cancel();
  });
});
