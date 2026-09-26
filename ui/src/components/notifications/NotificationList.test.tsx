/**
 * The list inside the bell, rendered with the pages it has been given.
 *
 * It shows what has been read so far and offers the next, older page while the
 * server says there is one — instead of being sent every notification there is.
 */
import { describe, expect, it } from 'bun:test';

import type { Notification } from '../../services/domains/notificationService';
import { visibleText } from '../../testing/markup';
import { renderMarkup } from '../../testing/renderMarkup';
import { NotificationList } from './NotificationList';

const notification = (i: number, isRead = false): Notification => ({
  id: `n-${i}`,
  user_id: 'alice',
  type: 'TaskAssignment',
  title: `Approve request ${i}`,
  message: `Request ${i} is waiting for you`,
  is_read: isRead,
  created_at: '2026-09-26T10:00:00Z',
});

function render(overrides: Partial<Parameters<typeof NotificationList>[0]> = {}): string {
  return renderMarkup(
    <NotificationList
      notifications={[notification(1), notification(2, true)]}
      loading={false}
      failed={false}
      hasOlder={false}
      loadingOlder={false}
      onLoadOlder={() => {}}
      onRead={() => {}}
      onDelete={() => {}}
      {...overrides}
    />,
  );
}

describe('the notification list', () => {
  it('shows the notifications it has been given', () => {
    const text = visibleText(render());

    expect(text).toContain('Approve request 1');
    expect(text).toContain('Approve request 2');
  });

  it('offers older notifications while the server has more', () => {
    expect(visibleText(render({ hasOlder: true }))).toContain('Load older notifications');
  });

  it('offers nothing older once the oldest has been shown', () => {
    expect(visibleText(render({ hasOlder: false }))).not.toContain('Load older notifications');
  });

  it('says there are none rather than showing an empty box', () => {
    expect(visibleText(render({ notifications: [] }))).toContain('No notifications yet');
  });

  it('says so, and what to do, when they could not be read', () => {
    const text = visibleText(render({ notifications: [], failed: true }));

    expect(text).toContain('Your notifications could not be loaded');
    expect(text).not.toContain('No notifications yet');
  });
});
