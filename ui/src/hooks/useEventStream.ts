/**
 * One authenticated connection to `/api/v1/events`, shared by every subscriber.
 *
 * `EventSource` cannot send headers and the endpoint requires a bearer token,
 * so every page that used it got a 401 and live updates never worked. This
 * reads the stream through `fetch` with the same headers the rest of the API
 * client sends, frames the `data:` lines itself, and reconnects with a capped
 * exponential backoff when the connection drops.
 *
 * The connection is opened when the first subscriber arrives and closed when
 * the last one leaves. Three screens listening is one socket, not three.
 */
import { useEffect, useRef } from 'react';
import { useQueryClient, type QueryKey } from '@tanstack/react-query';
import { backoffDelay, parseEventStreamChunk, type StreamEvent } from '../domain/eventStream';
import { API_BASE_URL } from '../services/shared/config';
import { getAuthToken } from '../services/shared/auth';
import { reportSessionRefused } from '../services/shared/sessionRefusal';

export type EventHandler = (event: StreamEvent) => void;

/** Subscribe to every event type. */
export const ALL_EVENTS = '*';

interface Subscription {
  types: ReadonlySet<string> | typeof ALL_EVENTS;
  handler: EventHandler;
}

const subscribers = new Set<Subscription>();
let controller: AbortController | null = null;
let reconnectTimer: ReturnType<typeof setTimeout> | null = null;
let failedAttempts = 0;

function dispatch(event: StreamEvent): void {
  subscribers.forEach((subscription) => {
    if (subscription.types !== ALL_EVENTS && !subscription.types.has(event.type)) {
      return;
    }
    try {
      subscription.handler(event);
    } catch (error) {
      // One listener's bug must not stop the others from hearing the event.
      console.error('An event stream handler threw', error);
    }
  });
}

async function readStream(body: ReadableStream<Uint8Array>): Promise<void> {
  const reader = body.getReader();
  const decoder = new TextDecoder();
  let buffer = '';
  for (;;) {
    const { value, done } = await reader.read();
    if (done) {
      return;
    }
    buffer += decoder.decode(value, { stream: true });
    const parsed = parseEventStreamChunk(buffer);
    buffer = parsed.rest;
    parsed.events.forEach(dispatch);
  }
}

async function connect(): Promise<void> {
  // Signed out, there is nobody to stream for: the next subscriber after
  // signing in opens it again.
  const token = getAuthToken();
  if (!token) {
    return;
  }
  const own = new AbortController();
  controller = own;
  try {
    const response = await fetch(`${API_BASE_URL}/events`, {
      headers: { Accept: 'text/event-stream', Authorization: `Bearer ${token}` },
      signal: own.signal,
    });
    if (response.status === 401) {
      // The token expired or was revoked. Retrying it only fails again, every
      // thirty seconds, for as long as the tab is open.
      if (controller === own) controller = null;
      reportSessionRefused(token);
      return;
    }
    if (!response.ok || !response.body) {
      throw new Error(`The event stream was refused (HTTP ${response.status})`);
    }
    failedAttempts = 0;
    await readStream(response.body);
  } catch (error) {
    if (own.signal.aborted) {
      return;
    }
    console.warn('The event stream dropped; reconnecting', error instanceof Error ? error.message : error);
  }
  if (!own.signal.aborted) {
    scheduleReconnect();
  }
}

function scheduleReconnect(): void {
  if (subscribers.size === 0 || reconnectTimer) {
    return;
  }
  const delay = backoffDelay(failedAttempts);
  failedAttempts += 1;
  reconnectTimer = setTimeout(() => {
    reconnectTimer = null;
    void connect();
  }, delay);
}

function ensureConnected(): void {
  if (controller || reconnectTimer) {
    return;
  }
  void connect();
}

function disconnect(): void {
  controller?.abort();
  controller = null;
  if (reconnectTimer) {
    clearTimeout(reconnectTimer);
    reconnectTimer = null;
  }
  failedAttempts = 0;
}

/**
 * Hears every event whose `type` is in `types` (or every event, with
 * ALL_EVENTS). Returns the function that stops listening.
 */
export function subscribeToEvents(types: readonly string[] | typeof ALL_EVENTS, handler: EventHandler): () => void {
  const subscription: Subscription = {
    types: types === ALL_EVENTS ? ALL_EVENTS : new Set(types),
    handler,
  };
  subscribers.add(subscription);
  ensureConnected();
  return () => {
    subscribers.delete(subscription);
    if (subscribers.size === 0) {
      disconnect();
    }
  };
}

/** Listens for the component's lifetime; the latest handler is always the one called. */
export function useEventStream(types: readonly string[] | typeof ALL_EVENTS, handler: EventHandler): void {
  const latest = useRef(handler);
  // Keep the ref current after render rather than during it: the subscription
  // below is set up once per `typesKey`, and always calls whatever handler was
  // last committed.
  useEffect(() => {
    latest.current = handler;
  });
  const typesKey = types === ALL_EVENTS ? ALL_EVENTS : types.join('|');

  useEffect(() => {
    const listenFor = typesKey === ALL_EVENTS ? ALL_EVENTS : typesKey.split('|');
    return subscribeToEvents(listenFor, (event) => latest.current(event));
  }, [typesKey]);
}

/** How long after the last event a refetch is issued. */
export const INVALIDATE_DEBOUNCE_MS = 300;

/**
 * Refetches the queries under `queryKey` when any of `types` arrives — at most
 * once per burst. A bulk claim of forty tasks is forty events in a few
 * milliseconds; without the trailing debounce it was forty refetches.
 */
export function useInvalidateOnEvents(types: readonly string[], queryKey: QueryKey): void {
  const queryClient = useQueryClient();
  const timer = useRef<ReturnType<typeof setTimeout> | null>(null);
  const keyText = JSON.stringify(queryKey);

  useEffect(() => () => {
    if (timer.current) clearTimeout(timer.current);
  }, []);

  useEventStream(types, () => {
    if (timer.current) clearTimeout(timer.current);
    timer.current = setTimeout(() => {
      timer.current = null;
      queryClient.invalidateQueries({ queryKey: JSON.parse(keyText) as QueryKey });
    }, INVALIDATE_DEBOUNCE_MS);
  });
}
