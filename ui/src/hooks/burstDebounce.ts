/**
 * Runs once a burst of calls goes quiet — but never waits longer than
 * `maxWaitMs` from the first call of the burst.
 *
 * A trailing debounce alone is reset by every call, so events arriving more
 * often than its wait (a busy project emits one every hundred milliseconds or
 * so) postponed the refetch for as long as the stream stayed busy, and the
 * screen showed data that grew older the more was happening.
 */
export interface BurstDebounce {
  call: () => void;
  cancel: () => void;
}

export function createBurstDebounce(run: () => void, waitMs: number, maxWaitMs: number): BurstDebounce {
  let timer: ReturnType<typeof setTimeout> | null = null;
  let burstStartedAt: number | null = null;

  const fire = () => {
    timer = null;
    burstStartedAt = null;
    run();
  };

  return {
    call() {
      const now = Date.now();
      if (burstStartedAt === null) burstStartedAt = now;
      if (timer !== null) clearTimeout(timer);
      const untilMaxWait = burstStartedAt + maxWaitMs - now;
      timer = setTimeout(fire, Math.max(0, Math.min(waitMs, untilMaxWait)));
    },
    cancel() {
      if (timer !== null) clearTimeout(timer);
      timer = null;
      burstStartedAt = null;
    },
  };
}
