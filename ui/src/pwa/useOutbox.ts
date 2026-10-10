/**
 * The queue, as the interface sees it.
 *
 * Flushes when the connection returns and once on load, and reports what
 * happened per entry so a refusal reaches the person who made it rather than
 * disappearing.
 */

import { notifications } from '@mantine/notifications';
import { useCallback, useEffect, useMemo, useState } from 'react';
import { useQueryClient } from '@tanstack/react-query';

import { describeQueue, ownedBy, type OutboxEntry } from '../domain/outbox';
import { useAppStore } from '../store/useAppStore';
import { flushOutbox } from './outbox';
import { watchOutbox } from './outboxStore';
import { errorMessage } from '../services/shared/errors';

export interface OutboxState {
  entries: OutboxEntry[];
  /** What to say about the queue, or "" when it is empty. */
  summary: string;
  /** Sends everything waiting now. */
  flush: () => Promise<void>;
}

export function useOutbox(): OutboxState {
  const [allEntries, setEntries] = useState<OutboxEntry[]>([]);
  const queryClient = useQueryClient();
  // The header counts only the signed-in person's work; anybody else's on this
  // device is not theirs to send or to worry about.
  const userId = useAppStore((state) => state.user?.id ?? null);
  const entries = useMemo(() => ownedBy(allEntries, userId), [allEntries, userId]);

  useEffect(() => watchOutbox(setEntries), []);

  const flush = useCallback(async () => {
    const results = await flushOutbox();
    if (results.length === 0) return;

    const sent = results.filter((r) => r.outcome.kind === 'sent');
    if (sent.length > 0) {
      notifications.show({
        title: sent.length === 1 ? 'Your change was sent' : `${sent.length} changes were sent`,
        message: sent.map((r) => r.entry.label).join(', '),
        color: 'green',
      });
      // The lists were showing what the server knew before this landed.
      void queryClient.invalidateQueries();
    }

    /*
     * A refusal is the case this whole queue exists to handle honestly. The
     * task was completed by somebody else, or reassigned, while the work sat in
     * a pocket. It must be said out loud, and it must not auto-dismiss: this is
     * somebody being told the approval they made did not take effect.
     */
    for (const { entry, outcome } of results) {
      if (outcome.kind !== 'refused') continue;
      notifications.show({
        title: 'A change made offline could not be applied',
        message: `${entry.label}: ${errorMessage(outcome.reason)}`,
        color: 'red',
        autoClose: false,
      });
    }
  }, [queryClient]);

  useEffect(() => {
    // On load, because the tab may have been closed while offline, and on each
    // sign-in: work queued before the session expired goes out once the
    // person is back, rather than at the next time the connection drops.
    if (userId) void flush();
  }, [flush, userId]);

  useEffect(() => {
    const onOnline = () => void flush();
    window.addEventListener('online', onOnline);
    return () => window.removeEventListener('online', onOnline);
  }, [flush]);

  return { entries, summary: describeQueue(entries), flush };
}
