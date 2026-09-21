/**
 * Doing a step as your worker would, without writing the worker first.
 *
 * This is the part that is hard to try any other way. A service task with a
 * topic is *published*, not called: the engine parks it and waits for somebody
 * to pull it. Until a worker exists, a process modelled that way simply stops,
 * and there is nothing on any screen to say why. Here you can pull it, hand
 * back a result, and watch the process carry on — which is also the fastest way
 * to find out that the topic string does not match.
 *
 */
import { Alert, Button, Code, Group, NumberInput, Select, Stack, Text, TextInput } from '@mantine/core';
import { Download, Info } from 'lucide-react';

import type { SurfacePoint } from '../../domain/sdkSurface';
import type { LockedTask } from '../../hooks/useSdkSandbox';
import { LockedTaskCard } from './LockedTaskCard';
import { SdkAdvanced } from './SdkAdvanced';

export interface SdkWorkStepProps {
  topics: SurfacePoint[];
  topic: string;
  workerId: string;
  maxTasks: number;
  lockDurationMs: number;
  lockedTask: LockedTask | null;
  noWorkOn: string | null;
  completeVariables: string;
  completeVariablesError: string | null;
  failMessage: string;
  retries: number;
  retryTimeoutMs: number;
  pending: string | null;
  hasInstance: boolean;
  onChange: (change: Partial<{
    topic: string;
    workerId: string;
    maxTasks: number;
    lockDurationMs: number;
    completeVariables: string;
    failMessage: string;
    retries: number;
    retryTimeoutMs: number;
  }>) => void;
  onFetch: () => void;
  onComplete: () => void;
  onFail: () => void;
}

export function SdkWorkStep(props: SdkWorkStepProps) {
  const { topics, topic, lockedTask, noWorkOn, pending, hasInstance, onChange, onFetch } = props;

  if (topics.length === 0) {
    return (
      <Alert variant="light" color="gray" icon={<Info size={16} />}>
        <Text size="sm">
          This process has no external-task steps, so there is nothing for a worker to pull. Skip
          to telling it something happened, below.
        </Text>
      </Alert>
    );
  }

  return (
    <Stack gap="md">
      <Group align="flex-end" gap="sm" wrap="nowrap">
        <Select
          label="Topic"
          description="The exact string your worker subscribes to."
          data={topics.map((point) => ({ value: point.name, label: point.name }))}
          value={topic === '' ? null : topic}
          onChange={(value) => onChange({ topic: value ?? '' })}
          style={{ flex: 1 }}
          allowDeselect={false}
        />
        <Button
          variant="light"
          leftSection={<Download size={15} />}
          onClick={onFetch}
          loading={pending === 'fetchAndLock'}
          disabled={topic === ''}
        >
          Ask for work
        </Button>
      </Group>

      <WorkerSettings {...props} />

      {noWorkOn !== null && lockedTask === null && (
        <Alert variant="light" color="gray" icon={<Info size={16} />}>
          <Text size="sm">
            Nothing waiting on <Code>{noWorkOn}</Code> right now.{' '}
            {hasInstance
              ? 'The instance you started may not have reached that step yet — or it took a different branch.'
              : 'Start an instance above and try again.'}
          </Text>
        </Alert>
      )}

      {lockedTask !== null && <LockedTaskCard {...props} lockedTask={lockedTask} />}
    </Stack>
  );
}

function WorkerSettings({ workerId, maxTasks, lockDurationMs, onChange }: SdkWorkStepProps) {
  return (
    <SdkAdvanced label="Worker settings">
      <Stack gap="sm">
        <TextInput
          label="Worker id"
          description="Recorded against the lock, so an operator can see who held this task."
          value={workerId}
          onChange={(event) => onChange({ workerId: event.currentTarget.value })}
          autoComplete="off"
        />
        <Group grow>
          <NumberInput
            label="Tasks per fetch"
            description="One at a time here — a sandbox that locked five would leave four invisible."
            value={maxTasks}
            onChange={(value) => onChange({ maxTasks: Number(value) || 1 })}
            min={1}
            max={10}
          />
          <NumberInput
            label="Lock duration (ms)"
            description="How long before the engine offers this task to somebody else."
            value={lockDurationMs}
            onChange={(value) => onChange({ lockDurationMs: Number(value) || 60_000 })}
            min={1000}
            step={1000}
            thousandSeparator=","
          />
        </Group>
      </Stack>
    </SdkAdvanced>
  );
}
