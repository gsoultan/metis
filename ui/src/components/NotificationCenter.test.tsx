/**
 * The notification bell, rendered, with the server stood in for.
 *
 * What is tested is where the bell's number comes from. It was counted in the
 * browser among the notifications the list had been sent — the newest
 * thousand — so a person whose unread ones were older than those saw no count
 * at all. The number is the server's now, whatever the list holds.
 */
import { afterEach, describe, expect, it, mock } from 'bun:test';
import { QueryClient } from '@tanstack/react-query';

import { stubFetchAnswering } from '../services/shared/stubbedFetch';
import { appStoreDouble, resetAppStore, setAppState } from '../testing/appStoreDouble';
import { namedControl, visibleText } from '../testing/markup';
import { renderMarkup } from '../testing/renderMarkup';

mock.module('../store/useAppStore', () => ({ useAppStore: appStoreDouble }));

const { NotificationCenter } = await import('./NotificationCenter');

const alice = {
  id: 'u-1',
  name: 'alice',
  displayName: 'Alice',
  organization: 'Acme',
  username: 'alice',
  role: 'USER',
};

// Every notification the list is sent has been read: the newest are, and the
// unread ones are older than anything a first page or a thousand rows reach.
const readOnes = Array.from({ length: 20 }, (_, i) => ({
  id: `n-${i}`,
  type: 'TaskAssignment',
  title: `Approve request ${i}`,
  message: '',
  is_read: true,
  created_at: '2026-09-26T10:00:00Z',
}));

let restore = () => {};
afterEach(() => {
  restore();
  resetAppStore();
});

/**
 * The bell's markup once the server has answered it, as `unread` unread.
 *
 * Rendering on the server fetches nothing, so this renders once to learn which
 * reads the bell makes, answers each from the stubbed server, and renders again
 * with the answers in hand.
 */
async function renderBell(unread: number): Promise<string> {
  const stub = stubFetchAnswering((url) => {
    if (url.pathname.endsWith('/users/me/notifications/unread-count')) return { unread_count: unread };
    if (url.pathname.endsWith('/users/me/notifications')) {
      return { notifications: readOnes, page: { total: 1050, page: 1, page_size: 20, has_more: true } };
    }
    return { notifications: readOnes };
  });
  restore = stub.restore;
  setAppState({ user: alice });

  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  renderMarkup(<NotificationCenter />, client);
  await Promise.all(client.getQueryCache().getAll().map((query) => query.fetch()));
  return renderMarkup(<NotificationCenter />, client);
}

describe('the notification bell', () => {
  it("shows the server's unread count, though every notification it was sent is read", async () => {
    const html = await renderBell(20);

    expect(visibleText(html)).toBe('20');
    expect(namedControl(html, 'Notifications, 20 unread')).toBeDefined();
  });

  it('shows no count when nothing is unread', async () => {
    const html = await renderBell(0);

    expect(visibleText(html)).toBe('');
    expect(namedControl(html, 'Notifications')).toBeDefined();
  });

  it('shortens a count too wide for the badge, and says it in full', async () => {
    const html = await renderBell(1050);

    expect(visibleText(html)).toBe('99+');
    expect(namedControl(html, 'Notifications, 1050 unread')).toBeDefined();
  });
});
