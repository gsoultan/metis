import {
  ActionIcon,
  Box,
  Button,
  Center,
  Group,
  Loader,
  Paper,
  ScrollArea,
  Stack,
  Text,
  ThemeIcon,
} from '@mantine/core';
import { AlertTriangle, Bell, ClipboardList, Info, Trash2 } from 'lucide-react';
import dayjs from 'dayjs';
import relativeTime from 'dayjs/plugin/relativeTime';

import type { Notification } from '../../services/domains/notificationService';

dayjs.extend(relativeTime);

interface NotificationListProps {
  /** The pages read so far, newest first. */
  notifications: Notification[];
  /** The first page is still on its way. */
  loading: boolean;
  /** The notifications could not be read. */
  failed: boolean;
  /** The server has older notifications than the ones shown. */
  hasOlder: boolean;
  loadingOlder: boolean;
  onLoadOlder: () => void;
  onRead: (id: string) => void;
  onDelete: (id: string) => void;
}

/**
 * The notifications behind the bell, a page at a time.
 *
 * It shows what has been read so far and offers the next, older page while
 * the server has one, instead of being handed a person's newest thousand on
 * every poll. Presentational: the bell reads the pages and says what happens
 * when one is opened, read or deleted.
 */
export function NotificationList({
  notifications,
  loading,
  failed,
  hasOlder,
  loadingOlder,
  onLoadOlder,
  onRead,
  onDelete,
}: NotificationListProps) {
  if (notifications.length === 0) {
    return <NothingToShow loading={loading} failed={failed} />;
  }
  return (
    <ScrollArea.Autosize mah={500} type="hover">
      <Stack gap={0}>
        {notifications.map((n) => (
          <NotificationItem key={n.id} notification={n} onRead={onRead} onDelete={onDelete} />
        ))}
        {hasOlder && (
          <Box p="xs">
            <Button variant="subtle" fullWidth size="xs" color="gray" loading={loadingOlder} onClick={onLoadOlder}>
              Load older notifications
            </Button>
          </Box>
        )}
      </Stack>
    </ScrollArea.Autosize>
  );
}

function NothingToShow({ loading, failed }: { loading: boolean; failed: boolean }) {
  if (loading) {
    return (
      <Center py={40}>
        <Loader size="sm" aria-label="Loading notifications" />
      </Center>
    );
  }
  return (
    <Stack align="center" py={40} gap="xs">
      <Bell size={32} color="var(--mantine-color-gray-4)" />
      <Text size="sm" c="dimmed" ta="center" px="md">
        {failed
          ? 'Your notifications could not be loaded. Close this and open it again to try again.'
          : 'No notifications yet'}
      </Text>
    </Stack>
  );
}

function NotificationItem({
  notification: n,
  onRead,
  onDelete,
}: {
  notification: Notification;
  onRead: (id: string) => void;
  onDelete: (id: string) => void;
}) {
  return (
    <Paper
      p="md"
      radius={0}
      bg={n.is_read ? 'transparent' : 'blue.0'}
      style={{
        cursor: 'pointer',
        borderBottom: '1px solid var(--mantine-color-gray-2)',
      }}
      onClick={() => !n.is_read && onRead(n.id)}
    >
      <Group align="flex-start" wrap="nowrap">
        <ThemeIcon
          variant="light"
          color={n.type === 'Incident' ? 'red' : n.type === 'TaskAssignment' ? 'blue' : 'gray'}
          radius="md"
        >
          {n.type === 'Incident' ? <AlertTriangle size={16} /> :
           n.type === 'TaskAssignment' ? <ClipboardList size={16} /> :
           <Info size={16} />}
        </ThemeIcon>

        <Stack gap={2} style={{ flex: 1 }}>
          <Group justify="space-between" wrap="nowrap">
            <Text size="sm" fw={700}>{n.title}</Text>
            <ActionIcon aria-label="Delete notification"
              variant="subtle"
              color="gray"
              size="xs"
              onClick={(e) => {
                e.stopPropagation();
                onDelete(n.id);
              }}
            >
              <Trash2 size={12} />
            </ActionIcon>
          </Group>
          <Text size="xs" lineClamp={2}>{n.message}</Text>
          <Text size="xs" c="dimmed" mt={4}>{dayjs(n.created_at).fromNow()}</Text>
        </Stack>
      </Group>
    </Paper>
  );
}
