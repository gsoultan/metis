/**
 * What went wrong with a process instance, and the button that tries again.
 *
 * A job that runs out of retries raises an incident and stops. Resolving that
 * incident already puts the job back on the queue with a clean slate — the
 * engine has had that capability all along. Nothing in the interface called it,
 * so failed work was invisible: an instance sat there looking stuck, and the
 * only way to recover it was a database write.
 *
 * This is the missing half. It lists what failed, says why in words an
 * operations person can act on, and offers the retry the engine already
 * supports.
 */
import { Alert, Badge, Button, Card, Code, Group, Loader, Stack, Text, Tooltip } from '@mantine/core';
import { notifications } from '@mantine/notifications';
import { AlertCircle, CheckCircle2, RotateCcw } from 'lucide-react';
import dayjs from 'dayjs';

import { explainIncident, incidentStep, type ApiIncident } from '../domain/incidents';
import { useIncidents, useResolveIncident } from '../hooks/useTasks';

export function IncidentInbox({ instanceId }: { instanceId: string }) {
  const { data, isLoading } = useIncidents(instanceId);
  const resolve = useResolveIncident();

  if (isLoading) return <Loader size="sm" />;

  const incidents = ((data?.incidents ?? []) as ApiIncident[]).filter((i) => i.status !== 'resolved');

  if (incidents.length === 0) {
    return (
      <Group gap="xs" c="dimmed">
        <CheckCircle2 size={16} />
        <Text size="sm">Nothing has failed on this process.</Text>
      </Group>
    );
  }

  const tryAgain = (incident: ApiIncident) => {
    resolve.mutate(incident.id, {
      onSuccess: () =>
        notifications.show({
          title: 'Queued again',
          message: `${incidentStep(incident)} will run again shortly.`,
          color: 'green',
        }),
      onError: (err: unknown) =>
        notifications.show({
          title: 'Could not queue it again',
          message: err instanceof Error ? err.message : 'The retry could not be started.',
          color: 'red',
        }),
    });
  };

  return (
    <Stack gap="sm">
      {incidents.map((incident) => {
        const explained = explainIncident(incident.error ?? '');
        return (
          <Card key={incident.id} withBorder radius="md" p="md">
            <Stack gap="xs">
              <Group justify="space-between" wrap="nowrap" align="flex-start">
                <Group gap="xs" wrap="nowrap">
                  <AlertCircle size={16} color="var(--mantine-color-red-6)" />
                  <Text fw={600} size="sm">
                    {incidentStep(incident)} failed
                  </Text>
                </Group>
                <Text size="xs" c="dimmed">
                  {dayjs(incident.created_at).fromNow()}
                </Text>
              </Group>

              {/* The cause first, because it decides whether the button below is
                  worth pressing. */}
              <Text size="sm">{explained.cause}</Text>
              <Text size="xs" c="dimmed">
                {explained.suggestion}
              </Text>

              <Group justify="space-between" align="flex-end" wrap="nowrap">
                {/* The raw error stays, in smaller type. Somebody eventually
                    needs it, and hiding it entirely turns a support call into an
                    archaeology exercise. */}
                <Code block fz={10} style={{ flex: 1, maxHeight: 90, overflow: 'auto' }}>
                  {incident.error}
                </Code>
                <Tooltip
                  label={
                    explained.worthRetrying
                      ? 'Put this step back on the queue'
                      : 'Retrying repeats exactly what failed — deal with the cause first'
                  }
                  multiline
                  w={220}
                  withArrow
                >
                  <Button
                    size="xs"
                    variant={explained.worthRetrying ? 'filled' : 'light'}
                    color={explained.worthRetrying ? 'blue' : 'gray'}
                    leftSection={<RotateCcw size={14} />}
                    loading={resolve.isPending}
                    onClick={() => tryAgain(incident)}
                  >
                    Try again
                  </Button>
                </Tooltip>
              </Group>

              {typeof incident.job?.retries === 'number' && incident.job.retries > 0 && (
                <Badge size="xs" variant="light" color="gray">
                  gave up after {incident.job.retries} attempt{incident.job.retries === 1 ? '' : 's'}
                </Badge>
              )}
            </Stack>
          </Card>
        );
      })}

      <Alert variant="light" color="gray" py="xs">
        <Text size="xs">
          Trying again puts the step back on the queue with a fresh set of attempts. The process carries on from
          where it stopped — nothing already done is repeated.
        </Text>
      </Alert>
    </Stack>
  );
}
