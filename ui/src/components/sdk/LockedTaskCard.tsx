/**
 * The task this sandbox currently holds, and the two ways to give it back.
 *
 * The lock is shown counting down on purpose. It is the one piece of the
 * external-task protocol people are surprised by: hold a task past its lock and
 * the engine offers it to somebody else, because two workers doing the same
 * step — charging the same card twice — is the failure the lock exists to
 * prevent. Watching the number fall teaches that in a way a paragraph does not.
 */
import {
  Alert,
  Badge,
  Button,
  Code,
  Divider,
  Group,
  NumberInput,
  Paper,
  Stack,
  Text,
  Textarea,
} from '@mantine/core';
import { CircleAlert, CircleCheck, TriangleAlert } from 'lucide-react';
import { useEffect, useState } from 'react';

import type { LockedTask } from '../../hooks/useSdkSandbox';
import { SdkAdvanced } from './SdkAdvanced';
import { VariablesField } from './VariablesField';
import type { SdkWorkStepProps } from './SdkWorkStep';

export function LockedTaskCard({
  lockedTask,
  completeVariables,
  completeVariablesError,
  failMessage,
  retries,
  retryTimeoutMs,
  pending,
  onChange,
  onComplete,
  onFail,
}: SdkWorkStepProps & { lockedTask: LockedTask }) {
  const remaining = useLockCountdown(lockedTask.lockExpiration);
  const given = JSON.stringify(lockedTask.variables, null, 2);
  const hasGiven = Object.keys(lockedTask.variables).length > 0;

  return (
    <Paper withBorder radius="md" p="md" bg="light-dark(var(--mantine-color-blue-0), var(--mantine-color-dark-6))">
      <Stack gap="sm">
        <Group justify="space-between" wrap="nowrap" align="flex-start">
          <div style={{ minWidth: 0 }}>
            <Text size="sm" fw={600}>
              {lockedTask.nodeName}
            </Text>
            <Text size="xs" c="dimmed">
              You hold this task. The process is waiting on you.
            </Text>
          </div>
          {remaining !== null && (
            <Badge
              variant="light"
              color={remaining <= 0 ? 'red' : remaining < 10 ? 'yellow' : 'blue'}
              radius="sm"
            >
              {remaining <= 0 ? 'Lock expired' : `Lock: ${remaining}s`}
            </Badge>
          )}
        </Group>

        {remaining !== null && remaining <= 0 && (
          <Alert variant="light" color="red" p="xs" icon={<TriangleAlert size={14} />}>
            <Text size="xs">
              The lock ran out, so another worker may already have this task. Reporting now may be
              refused — ask for work again.
            </Text>
          </Alert>
        )}

        {hasGiven && (
          <div>
            <Text size="xs" c="dimmed" mb={4}>
              What the step was given
            </Text>
            <Code block style={{ fontSize: '0.72rem', maxHeight: 160, overflow: 'auto' }}>
              {given}
            </Code>
          </div>
        )}

        <Divider />

        <VariablesField
          label="Hand back"
          description="What your handler returns. The engine writes this into the instance and carries on."
          value={completeVariables}
          onChange={(next) => onChange({ completeVariables: next })}
          error={completeVariablesError}
          placeholder={'{\n  "reversed": true\n}'}
          actions={
            hasGiven ? (
              <Button
                size="compact-xs"
                variant="subtle"
                color="gray"
                onClick={() => onChange({ completeVariables: given })}
              >
                Start from what it was given
              </Button>
            ) : undefined
          }
        />

        <Group gap="sm">
          <Button
            color="teal"
            leftSection={<CircleCheck size={15} />}
            onClick={onComplete}
            loading={pending === 'complete'}
            disabled={completeVariablesError !== null}
          >
            Report it done
          </Button>
        </Group>

        <SdkAdvanced label="Report it failed instead">
          <Stack gap="sm">
            <Textarea
              label="What went wrong"
              description="An operator reads this on the incident, so write it for them."
              placeholder="card declined"
              value={failMessage}
              onChange={(event) => onChange({ failMessage: event.currentTarget.value })}
              autosize
              minRows={2}
            />
            <Group grow>
              <NumberInput
                label="Retries left after this"
                description="Zero means give up and raise an incident."
                value={retries}
                onChange={(value) => onChange({ retries: Math.max(0, Number(value) || 0) })}
                min={0}
                max={10}
              />
              <NumberInput
                label="Wait before retrying (ms)"
                value={retryTimeoutMs}
                onChange={(value) => onChange({ retryTimeoutMs: Math.max(0, Number(value) || 0) })}
                min={0}
                step={1000}
                thousandSeparator=","
              />
            </Group>
            <Button
              color="red"
              variant="light"
              leftSection={<CircleAlert size={15} />}
              onClick={onFail}
              loading={pending === 'fail'}
              style={{ alignSelf: 'flex-start' }}
            >
              Report it failed
            </Button>
          </Stack>
        </SdkAdvanced>
      </Stack>
    </Paper>
  );
}

/**
 * Seconds left on the lock, ticking.
 *
 * An interval rather than a derived value because time is the one thing that
 * changes without anybody touching the page — and it only runs while a task is
 * actually held, so an idle sandbox re-renders nothing.
 */
function useLockCountdown(expiration: string | null): number | null {
  const [now, setNow] = useState(() => Date.now());

  useEffect(() => {
    if (expiration === null) {
      return;
    }
    const timer = setInterval(() => setNow(Date.now()), 1000);
    return () => clearInterval(timer);
  }, [expiration]);

  if (expiration === null) {
    return null;
  }
  const expiresAt = Date.parse(expiration);
  if (Number.isNaN(expiresAt)) {
    return null;
  }
  return Math.ceil((expiresAt - now) / 1000);
}
