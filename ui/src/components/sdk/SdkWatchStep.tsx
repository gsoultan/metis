/**
 * What the process did about it.
 *
 * Reads the same two endpoints an integration would — the instance and its
 * timeline — and refreshes from the event stream rather than polling, so the
 * result of the call you just made appears without anybody pressing anything.
 *
 * The timeline is the business narrative, not the engine's log: "Task
 * \"Approve the refund\" became available" rather than `TaskCreated`. That is
 * the same view an operator gets, which is the point — if your integration
 * misfires, this is where somebody will see it.
 */
import { Alert, Badge, Code, Group, Loader, Stack, Text } from '@mantine/core';
import { useQuery } from '@tanstack/react-query';
import { Info } from 'lucide-react';

import { BusinessTimeline } from '../BusinessTimeline';
import { useInvalidateOnEvents } from '../../hooks/useEventStream';
import { readInstance } from '../../services/domains/sdkSandboxService';

/** The engine's own event names, which is what the stream carries. */
const PROCESS_EVENTS = [
  'ProcessStarted',
  'NodeReached',
  'TaskCreated',
  'TaskCompleted',
  'TaskClaimed',
  'TaskCanceled',
  'ProcessCompleted',
] as const;

const STATUS_COLOUR: Record<string, string> = {
  active: 'blue',
  completed: 'teal',
  terminated: 'gray',
  suspended: 'yellow',
};

interface SdkWatchStepProps {
  instanceId: string | null;
}

export function SdkWatchStep({ instanceId }: SdkWatchStepProps) {
  useInvalidateOnEvents([...PROCESS_EVENTS], ['sandbox-instance', instanceId]);
  useInvalidateOnEvents([...PROCESS_EVENTS], ['audit-logs', instanceId]);

  const { data, isLoading } = useQuery({
    queryKey: ['sandbox-instance', instanceId],
    queryFn: ({ signal }) => (instanceId ? readInstance(instanceId, signal) : Promise.resolve(null)),
    enabled: instanceId !== null,
  });

  if (instanceId === null) {
    return (
      <Alert variant="light" color="gray" icon={<Info size={16} />}>
        <Text size="sm">
          Start an instance above and its status and timeline appear here, updating as the engine
          moves it along.
        </Text>
      </Alert>
    );
  }

  const variables = data?.variables ?? {};
  const hasVariables = Object.keys(variables).length > 0;

  return (
    <Stack gap="md">
      <Group gap="sm" wrap="nowrap">
        {isLoading ? (
          <Loader size="xs" />
        ) : (
          <Badge variant="light" color={STATUS_COLOUR[data?.status ?? ''] ?? 'gray'} radius="sm">
            {data?.status ?? 'unknown'}
          </Badge>
        )}
        {data?.definitionName !== undefined && (
          <Text size="sm" c="dimmed">
            {data.definitionName}
          </Text>
        )}
      </Group>

      {hasVariables && (
        <div>
          <Text size="xs" c="dimmed" mb={4}>
            Variables the instance carries now
          </Text>
          <Code block style={{ fontSize: '0.72rem', maxHeight: 200, overflow: 'auto' }}>
            {JSON.stringify(variables, null, 2)}
          </Code>
        </div>
      )}

      <div>
        <Text size="xs" c="dimmed" mb={4}>
          What has happened
        </Text>
        <BusinessTimeline instanceId={instanceId} />
      </div>
    </Stack>
  );
}
