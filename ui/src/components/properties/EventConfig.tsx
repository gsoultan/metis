import { SegmentedControl, Select, Stack, Text, TextInput } from '@mantine/core';

import { asText } from '../../types/bpmn';
import type { NodeConfigProps } from '../PropertyPanel';
import { PropertySection } from './PropertySection';

/**
 * A step that waits for something.
 *
 * What it waits for decides which fields matter, so that choice comes first
 * and the rest follows it. The three options were "Timer", "Signal" and
 * "Message", which name the notation rather than the situation.
 */
export function EventConfig({ data, onUpdate }: NodeConfigProps) {
  const eventType =
    asText(data.eventType) ||
    (data.duration ? 'timer' : data.signalName ? 'signal' : data.messageName ? 'message' : 'timer');
  const timerType = asText(data.timerType, 'duration');

  return (
    <Stack gap="xl">
      <PropertySection title="What it waits for">
        <SegmentedControl
          fullWidth
          data={[
            { value: 'timer', label: 'Time' },
            { value: 'message', label: 'A message' },
            { value: 'signal', label: 'A signal' },
          ]}
          value={eventType}
          onChange={(val) => onUpdate({ eventType: val })}
        />
        <Text size="xs" c="dimmed">
          {eventType === 'timer' && 'The process pauses here and carries on by itself.'}
          {eventType === 'message' && 'Another system tells this one process to carry on.'}
          {eventType === 'signal' && 'A broadcast: every process waiting for this signal carries on.'}
        </Text>
      </PropertySection>

      {eventType === 'timer' && (
        <PropertySection title="How long">
          <Select
            label="Wait"
            data={[
              { value: 'duration', label: 'For a while' },
              { value: 'date', label: 'Until a date' },
              { value: 'cycle', label: 'Over and over' },
            ]}
            value={timerType}
            onChange={(val) => onUpdate({ timerType: val })}
            allowDeselect={false}
          />
          <TextInput
            label={timerType === 'date' ? 'Until' : 'For'}
            placeholder={timerType === 'date' ? '2026-01-01T12:00:00Z' : 'PT1H'}
            description={
              timerType === 'date'
                ? 'A date and time, e.g. 2026-01-01T12:00:00Z'
                : 'PT1H is one hour, PT10M ten minutes, P1D a day'
            }
            value={asText(data.duration)}
            onChange={(e) => onUpdate({ duration: e.target.value })}
          />
        </PropertySection>
      )}

      {eventType === 'signal' && (
        <PropertySection title="Which signal" hint="Every process waiting for this name carries on when it is broadcast.">
          <TextInput
            label="Signal name"
            placeholder="e.g. day-closed"
            value={asText(data.signalName)}
            onChange={(e) => onUpdate({ signalName: e.target.value })}
          />
        </PropertySection>
      )}

      {eventType === 'message' && (
        <PropertySection title="Which message" hint="Sent to one process, so it needs to say which one.">
          <TextInput
            label="Message name"
            placeholder="e.g. payment-received"
            value={asText(data.messageName)}
            onChange={(e) => onUpdate({ messageName: e.target.value })}
          />
          <TextInput
            label="Belonging to"
            placeholder="${orderId}"
            description="The value that picks out this process from the others waiting — usually the id it was started with."
            value={asText(data.correlationKey)}
            onChange={(e) => onUpdate({ correlationKey: e.target.value })}
          />
        </PropertySection>
      )}
    </Stack>
  );
}
