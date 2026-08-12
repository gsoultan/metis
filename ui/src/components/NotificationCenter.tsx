import { 
  Popover, 
  ActionIcon, 
  Indicator, 
  ScrollArea, 
  Text, 
  Group, 
  Stack, 
  Paper, 
  Button, 
  ThemeIcon, 
  Divider,
  Box,
} from '@mantine/core';
import { Bell, CheckCheck, Trash2, Info, AlertTriangle, ClipboardList } from 'lucide-react';
import { useNotifications } from '../hooks/useNotification';
import dayjs from 'dayjs';
import relativeTime from 'dayjs/plugin/relativeTime';
import { ComingSoonButton } from './state/ComingSoon';

dayjs.extend(relativeTime);

export function NotificationCenter() {
  const { 
    notifications, 
    unreadCount, 
    markAsRead, 
    markAllAsRead, 
    deleteNotification,
  } = useNotifications();

  return (
    <Popover width={400} position="bottom-end" withArrow shadow="md" radius="md">
      <Popover.Target>
        <Indicator label={unreadCount > 0 ? unreadCount : undefined} size={16} offset={4} color="red" disabled={unreadCount === 0}>
          <ActionIcon variant="subtle" color="gray" size="lg" radius="xl">
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

          <ScrollArea.Autosize mah={500} type="hover">
            {notifications.length === 0 ? (
              <Stack align="center" py={40} gap="xs">
                <Bell size={32} color="var(--mantine-color-gray-4)" />
                <Text size="sm" c="dimmed">No notifications yet</Text>
              </Stack>
            ) : (
              <Stack gap={0}>
                {notifications.map((n: any) => (
                  <Paper 
                    key={n.id} 
                    p="md" 
                    radius={0} 
                    bg={n.is_read ? 'transparent' : 'blue.0'}
                    style={{ 
                      cursor: 'pointer',
                      borderBottom: '1px solid var(--mantine-color-gray-2)'
                    }}
                    onClick={() => !n.is_read && markAsRead.mutate(n.id)}
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
                          <ActionIcon 
                            variant="subtle" 
                            color="gray" 
                            size="xs" 
                            onClick={(e) => {
                              e.stopPropagation();
                              deleteNotification.mutate(n.id);
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
                ))}
              </Stack>
            )}
          </ScrollArea.Autosize>
          
          <Divider />
          
          <Box p="xs">
            <ComingSoonButton variant="subtle" fullWidth size="xs" color="gray" label="A full notification history view is not built yet">
              View all notification history
            </ComingSoonButton>
          </Box>
        </Stack>
      </Popover.Dropdown>
    </Popover>
  );
}
