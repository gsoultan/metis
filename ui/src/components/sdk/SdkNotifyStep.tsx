/**
 * Telling a running process that something happened.
 *
 * The distinction between the two buttons is the whole lesson: a **message**
 * is addressed — it carries a correlation key and wakes the one instance that
 * is waiting for that key — while a **signal** is broadcast and wakes every
 * instance listening for it. Getting that backwards is how one customer's
 * payment confirmation completes four hundred refunds.
 */
import { Alert, Autocomplete, Button, Group, SegmentedControl, Stack, Text, TextInput } from '@mantine/core';
import { Info, Send } from 'lucide-react';

import type { SurfacePoint } from '../../domain/sdkSurface';
import { VariablesField } from './VariablesField';

interface SdkNotifyStepProps {
  inbound: SurfacePoint[];
  kind: 'message' | 'signal';
  name: string;
  correlationKey: string;
  variables: string;
  variablesError: string | null;
  pending: string | null;
  onChange: (change: Partial<{
    notifyKind: 'message' | 'signal';
    notifyName: string;
    correlationKey: string;
    notifyVariables: string;
  }>) => void;
  onSend: () => void;
}

export function SdkNotifyStep({
  inbound,
  kind,
  name,
  correlationKey,
  variables,
  variablesError,
  pending,
  onChange,
  onSend,
}: SdkNotifyStepProps) {
  const suggestions = inbound.map((point) => point.name);
  const matching = inbound.find((point) => point.name === name);

  return (
    <Stack gap="md">
      <SegmentedControl
        size="xs"
        value={kind}
        onChange={(value) => onChange({ notifyKind: value as 'message' | 'signal' })}
        data={[
          { value: 'message', label: 'Message — one instance' },
          { value: 'signal', label: 'Signal — everyone listening' },
        ]}
        aria-label="How to tell the process"
      />

      <Autocomplete
        label={kind === 'message' ? 'Message name' : 'Signal name'}
        description={
          suggestions.length > 0
            ? 'Names this process waits for are suggested; any other name is accepted too.'
            : 'The exact name the process is waiting for.'
        }
        placeholder={kind === 'message' ? 'payment.received' : 'quarter.closed'}
        data={suggestions}
        value={name}
        onChange={(value) => onChange({ notifyName: value })}
        autoComplete="off"
      />

      {kind === 'message' && (
        <TextInput
          label="Correlation key"
          description={
            matching?.correlationKey !== undefined
              ? `This event is addressed by ${matching.correlationKey} — send that instance's value.`
              : 'Which instance this is for. Leave it empty and every instance waiting for this message hears it.'
          }
          placeholder="order-4471"
          value={correlationKey}
          onChange={(event) => onChange({ correlationKey: event.currentTarget.value })}
          autoComplete="off"
        />
      )}

      {kind === 'signal' && (
        <Alert variant="light" color="yellow" p="xs" icon={<Info size={14} />}>
          <Text size="xs">
            A signal reaches every instance in this project that is waiting for it, not only the
            one you started.
          </Text>
        </Alert>
      )}

      <VariablesField
        label="Variables to carry"
        description="Merged into the instance that receives this."
        value={variables}
        onChange={(next) => onChange({ notifyVariables: next })}
        error={variablesError}
        placeholder={'{\n  "paid": true\n}'}
      />

      <Group>
        <Button
          leftSection={<Send size={15} />}
          onClick={onSend}
          loading={pending === 'message' || pending === 'signal'}
          disabled={name.trim() === '' || variablesError !== null}
        >
          {kind === 'message' ? 'Send the message' : 'Broadcast the signal'}
        </Button>
      </Group>
    </Stack>
  );
}
