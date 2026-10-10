import { afterEach, beforeEach, describe, expect, it, jest } from 'bun:test';

import { createBurstDebounce } from './burstDebounce';

describe('the refetch debounce', () => {
  beforeEach(() => jest.useFakeTimers());
  afterEach(() => jest.useRealTimers());

  it('runs once for a short burst, after it goes quiet', () => {
    let runs = 0;
    const debounce = createBurstDebounce(() => { runs += 1; }, 300, 2_000);

    for (let i = 0; i < 40; i++) debounce.call();
    jest.advanceTimersByTime(299);
    expect(runs).toBe(0);
    jest.advanceTimersByTime(1);
    expect(runs).toBe(1);
  });

  it('still runs at least every max wait while events never stop', () => {
    let runs = 0;
    const debounce = createBurstDebounce(() => { runs += 1; }, 300, 2_000);

    // An event every 100 ms for six seconds: never quiet for 300 ms.
    for (let elapsed = 0; elapsed < 6_000; elapsed += 100) {
      debounce.call();
      jest.advanceTimersByTime(100);
    }
    expect(runs).toBe(3);
    debounce.cancel();
  });

  it('runs nothing once cancelled', () => {
    let runs = 0;
    const debounce = createBurstDebounce(() => { runs += 1; }, 300, 2_000);
    debounce.call();
    debounce.cancel();
    jest.advanceTimersByTime(5_000);
    expect(runs).toBe(0);
  });
});
