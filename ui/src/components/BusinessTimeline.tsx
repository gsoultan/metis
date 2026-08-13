import { Timeline, Text, Group, Badge, Stack, ScrollArea, ThemeIcon, Box } from '@mantine/core';
import { Check, Clock, User, AlertCircle, Play, Square, FastForward } from 'lucide-react';
import { useAuditLogs } from '../hooks/useProcess';
import dayjs from 'dayjs';
import relativeTime from 'dayjs/plugin/relativeTime';
import { asText } from '../types/bpmn';

dayjs.extend(relativeTime);

interface BusinessTimelineProps {
  instanceId: string;
}

const getEventIcon = (type: string) => {
  switch (type) {
    case 'ProcessStarted':
      return <Play size={14} />;
    case 'NodeReached':
      return <FastForward size={14} />;
    case 'TaskCreated':
      return <Clock size={14} />;
    case 'TaskClaimed':
      return <User size={14} />;
    case 'TaskCompleted':
      return <Check size={14} />;
    case 'ProcessCompleted':
      return <Square size={14} />;
    default:
      return <AlertCircle size={14} />;
  }
};

const getEventColor = (type: string) => {
  switch (type) {
    case 'ProcessStarted':
      return 'blue';
    case 'TaskCreated':
      return 'yellow';
    case 'TaskClaimed':
      return 'indigo';
    case 'TaskCompleted':
      return 'green';
    case 'ProcessCompleted':
      return 'teal';
    case 'IncidentCreated':
      return 'red';
    default:
      return 'gray';
  }
};

export function BusinessTimeline({ instanceId }: BusinessTimelineProps) {
  const { data, isLoading } = useAuditLogs(instanceId);

  if (isLoading) return <Text>Loading timeline...</Text>;
  if (!data?.entries || data.entries.length === 0) return <Text c="dimmed">No activity recorded yet.</Text>;

  // Filter out repetitive technical events for the business view if needed, 
  // but for now we use the narratives we generated.
  const entries = [...data.entries].sort((a, b) => 
    new Date(b.timestamp).getTime() - new Date(a.timestamp).getTime()
  );

  return (
    <ScrollArea h={500} offsetScrollbars>
      <Box p="md">
        <Timeline active={entries.length} bulletSize={24} lineWidth={2}>
          {entries.map((entry, index) => (
            <Timeline.Item
              key={entry.id || index}
              bullet={
                <ThemeIcon
                  size={22}
                  radius="xl"
                  color={getEventColor(entry.type)}
                >
                  {getEventIcon(entry.type)}
                </ThemeIcon>
              }
              title={
                <Group justify="space-between" align="flex-start">
                  <Text fw={500} size="sm">
                    {entry.narrative || entry.message}
                  </Text>
                  <Text size="xs" c="dimmed">
                    {dayjs(entry.timestamp).fromNow()}
                  </Text>
                </Group>
              }
            >
              <Stack gap={4} mt={4}>
                {entry.node?.name && (
                  <Text size="xs" c="dimmed">
                    Step: {entry.node.name}
                  </Text>
                )}
                {entry.type === 'TaskClaimed' && Boolean(entry.data?.assignee) && (
                  <Badge size="xs" variant="light" color="indigo">
                    Assignee: {asText(entry.data?.assignee)}
                  </Badge>
                )}
              </Stack>
            </Timeline.Item>
          ))}
        </Timeline>
      </Box>
    </ScrollArea>
  );
}
