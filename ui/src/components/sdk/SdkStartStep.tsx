/**
 * Starting an instance — the first call any integration makes.
 *
 * The instance id that comes back is shown rather than tucked away, because
 * every later step needs it and because it is the thing to quote when asking
 * somebody else what went wrong.
 */
import { Alert, Button, Code, CopyButton, Group, Stack, Text, TextInput, Tooltip } from '@mantine/core';
import { Check, Copy, Play } from 'lucide-react';

import { SdkAdvanced } from './SdkAdvanced';
import { VariablesField } from './VariablesField';

interface SdkStartStepProps {
  definitionKey: string;
  variables: string;
  variablesError: string | null;
  idempotencyKey: string;
  instanceId: string | null;
  starting: boolean;
  onVariablesChange: (next: string) => void;
  onIdempotencyKeyChange: (next: string) => void;
  onStart: () => void;
}

export function SdkStartStep({
  definitionKey,
  variables,
  variablesError,
  idempotencyKey,
  instanceId,
  starting,
  onVariablesChange,
  onIdempotencyKeyChange,
  onStart,
}: SdkStartStepProps) {
  const ready = definitionKey !== '' && variablesError === null;

  return (
    <Stack gap="md">
      <VariablesField
        label="Variables to start with"
        description="The business payload — what the process is about."
        value={variables}
        onChange={onVariablesChange}
        error={variablesError}
        disabled={definitionKey === ''}
      />

      <SdkAdvanced>
        <TextInput
          label="Idempotency key"
          description="Send the same key on a retry and Metis replays the first answer instead of starting a second instance."
          placeholder="order-4471-start"
          value={idempotencyKey}
          onChange={(event) => onIdempotencyKeyChange(event.currentTarget.value)}
          autoComplete="off"
        />
      </SdkAdvanced>

      <Group justify="space-between" wrap="nowrap">
        <Button
          leftSection={<Play size={15} />}
          onClick={onStart}
          loading={starting}
          disabled={!ready}
        >
          Start an instance
        </Button>
        {definitionKey === '' && (
          <Text size="xs" c="dimmed">
            Choose a process first.
          </Text>
        )}
      </Group>

      {instanceId !== null && (
        <Alert variant="light" color="teal" p="xs" radius="md">
          <Group gap="xs" wrap="nowrap" justify="space-between">
            <Group gap="xs" wrap="nowrap" style={{ minWidth: 0 }}>
              <Text size="xs" fw={500} style={{ whiteSpace: 'nowrap' }}>
                Running:
              </Text>
              <Code style={{ fontSize: '0.72rem' }}>{instanceId}</Code>
            </Group>
            <CopyButton value={instanceId} timeout={1200}>
              {({ copied, copy }) => (
                <Tooltip label={copied ? 'Copied' : 'Copy the instance id'} withArrow>
                  <Button
                    size="compact-xs"
                    variant="subtle"
                    color={copied ? 'teal' : 'gray'}
                    onClick={copy}
                    aria-label="Copy the instance id"
                  >
                    {copied ? <Check size={12} /> : <Copy size={12} />}
                  </Button>
                </Tooltip>
              )}
            </CopyButton>
          </Group>
        </Alert>
      )}
    </Stack>
  );
}
