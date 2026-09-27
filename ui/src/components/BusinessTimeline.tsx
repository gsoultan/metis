import { Timeline, Text, Group, Badge, Stack, ScrollArea, ThemeIcon, Box } from '@mantine/core';
import { Check, Clock, User, AlertCircle, Play, Square, FastForward, Scale } from 'lucide-react';
import { useAuditLogs } from '../hooks/useProcess';
import dayjs from 'dayjs';
import relativeTime from 'dayjs/plugin/relativeTime';
import { asText } from '../types/bpmn';
import { describeDecision } from '../domain/decisionNarrative';
import { timelineKind } from '../domain/timelineKind';

dayjs.extend(relativeTime);

interface BusinessTimelineProps {
  instanceId: string;
}

const getEventIcon = (type: string) => {
  switch (timelineKind(type)) {
    case 'started':
      return <Play size={14} />;
    case 'reached':
      return <FastForward size={14} />;
    case 'available':
      return <Clock size={14} />;
    case 'claimed':
    case 'released':
    case 'assigned':
    case 'delegated':
      return <User size={14} />;
    case 'completed':
      return <Check size={14} />;
    case 'ended':
      return <Square size={14} />;
    case 'decision':
      return <Scale size={14} />;
    default:
      return <AlertCircle size={14} />;
  }
};

const getEventColor = (type: string) => {
  switch (timelineKind(type)) {
    case 'started':
      return 'blue';
    case 'available':
      return 'yellow';
    case 'claimed':
    case 'assigned':
    case 'delegated':
      return 'indigo';
    case 'completed':
      return 'green';
    case 'ended':
      return 'teal';
    case 'incident':
      return 'red';
    case 'decision':
      return 'grape';
    default:
      return 'gray';
  }
};

/** A decision, as a sentence and the version of the policy that made it. */
function DecisionDetail({ data }: { data?: Record<string, unknown> }) {
  const narrative = describeDecision(data);
  return (
    <Group gap={6} wrap="wrap">
      <Text size="xs">{narrative.sentence}</Text>
      {narrative.version && (
        <Badge size="xs" variant="light" color="grape" styles={{ label: { textTransform: 'none' } }}>
          {narrative.version}
        </Badge>
      )}
    </Group>
  );
}

export function BusinessTimeline({ instanceId }: BusinessTimelineProps) {
  const { data, isLoading } = useAuditLogs(instanceId);

  if (isLoading) return <Text>Loading timeline...</Text>;
  if (!data?.entries || data.entries.length === 0) return <Text c="dimmed">No activity recorded yet.</Text>;

  // Newest first: the trail arrives oldest first, in the order it was written,
  // so this is that order reversed. Not a sort by timestamp — the entries one
  // step writes share its timestamp, and a stable sort left them oldest first
  // under the newest-first ones, the step's last entry below its first.
  const entries = [...data.entries].reverse();

  /*
   * Height follows the content up to a cap, rather than always being 500px.
   * A fixed height left roughly 350px of empty card under a three-event
   * timeline — the dashboard's largest element was mostly blank, which reads
   * as a failed load rather than a quiet system.
   */
  return (
    <ScrollArea.Autosize mah={500} offsetScrollbars>
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
                {/* A decision is the one entry where "what changed" is not the
                    interesting part. Which table decided, which version of it
                    was in force, and what it decided are what somebody comes
                    back to this timeline for — said in words, where it used to
                    be the audit record verbatim: a key, rule ids and JSON. */}
                {entry.type === 'decision_evaluated' && <DecisionDetail data={entry.data} />}
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
    </ScrollArea.Autosize>
  );
}
