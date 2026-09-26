import { useState } from 'react';
import {
  Popover,
  ActionIcon,
  Indicator,
  Text,
  Group,
  Stack,
  Button,
  Divider,
} from '@mantine/core';
import { Bell, CheckCheck } from 'lucide-react';

import { bellBadge, bellName } from '../domain/notificationBell';
import { useNotifications } from '../hooks/useNotification';
import { NotificationList } from './notifications/NotificationList';

/**
 * The bell in the header, and the list behind it.
 *
 * The bell shows how many of the signed-in person's notifications are unread,
 * as the server counts them over every one they have. The list is read only
 * while it is open, a page at a time.
 */
export function NotificationCenter() {
  const [opened, setOpened] = useState(false);
  const {
    unreadCount,
    notifications,
    isLoading,
    failed,
    hasOlder,
    isLoadingOlder,
    loadOlder,
    markAsRead,
    markAllAsRead,
    deleteNotification,
  } = useNotifications(opened);
  const badge = bellBadge(unreadCount);

  return (
    <Popover
      opened={opened}
      onChange={setOpened}
      width={400}
      position="bottom-end"
      withArrow
      shadow="md"
      radius="md"
    >
      <Popover.Target>
        <Indicator label={badge} size={16} offset={4} color="red" disabled={badge === undefined}>
          <ActionIcon
            aria-label={bellName(unreadCount)}
            variant="subtle"
            color="gray"
            size="lg"
            radius="xl"
            onClick={() => setOpened((open) => !open)}
          >
            <Bell size={20} />
          </ActionIcon>
        </Indicator>
      </Popover.Target>
      <Popover.Dropdown p={0}>
        <Stack gap={0}>
          <Group justify="space-between" p="md">
            <Text fw={700} size="md">Notifications</Text>
            {unreadCount > 0 && (
              <Button
                variant="subtle"
                size="compact-xs"
                leftSection={<CheckCheck size={14} />}
                onClick={() => markAllAsRead.mutate()}
              >
                Mark all as read
              </Button>
            )}
          </Group>

          <Divider />

          <NotificationList
            notifications={notifications}
            loading={isLoading}
            failed={failed}
            hasOlder={hasOlder}
            loadingOlder={isLoadingOlder}
            onLoadOlder={loadOlder}
            onRead={(id) => markAsRead.mutate(id)}
            onDelete={(id) => deleteNotification.mutate(id)}
          />
        </Stack>
      </Popover.Dropdown>
    </Popover>
  );
}
