import { Checkbox, Select, Stack, Text, TextInput } from '@mantine/core';

import { asText } from '../../types/bpmn';
import type { NodeConfigProps } from '../PropertyPanel';
import { PropertySection } from './PropertySection';

/**
 * A step that waits for something.
 *
 * What it waits for decides which fields matter, so that choice comes first
 * and the rest follows it. The options name the situation rather than the
 * notation — "Something failed", not "Error boundary event".
 *
 * A step attached to the side of another one can wait for more kinds of thing
 * than a step in the flow can: a failure, a problem worth raising, or a request
 * to undo the work. The engine has always run all of those; only the three in
 * the middle of a flow could be drawn.
 */

type EventChoice = { value: string; label: string; blurb: string };

/** What a step sitting in the flow can wait for. */
const FLOW_EVENTS: EventChoice[] = [
  { value: 'timer', label: 'Time', blurb: 'The process pauses here and carries on by itself.' },
  { value: 'message', label: 'A message', blurb: 'Another system tells this one process to carry on.' },
  { value: 'signal', label: 'A signal', blurb: 'A broadcast: every process waiting for this signal carries on.' },
];

/** What a step attached to another one can additionally wait for. */
const BOUNDARY_ONLY_EVENTS: EventChoice[] = [
  { value: 'error', label: 'A failure', blurb: 'The step it is attached to failed, and this is what to do instead.' },
  { value: 'escalation', label: 'Something to raise', blurb: 'The work is still going, but somebody needs to know.' },
  { value: 'compensation', label: 'Undoing the work', blurb: 'How to reverse this step if the process is rolled back later.' },
];

/** Guess what an existing step waits for, from whatever was filled in. */
function inferEventType(data: NodeConfigProps['data']): string {
  const explicit = asText(data.eventType);
  if (explicit) return explicit;
  if (data.errorCode) return 'error';
  if (data.escalationCode) return 'escalation';
  if (data.signalName) return 'signal';
  if (data.messageName) return 'message';
  return 'timer';
}

export function EventConfig({ data, onUpdate }: NodeConfigProps) {
  const isBoundary = data.nodeType === 'boundaryEvent';
  const choices = isBoundary ? [...FLOW_EVENTS, ...BOUNDARY_ONLY_EVENTS] : FLOW_EVENTS;
  const eventType = inferEventType(data);
  const timerType = asText(data.timerType, 'duration');
  const blurb = choices.find((c) => c.value === eventType)?.blurb ?? '';

  return (
    <Stack gap="xl">
      <PropertySection title="What it waits for">
        <Select
          label="Waits for"
          data={choices.map(({ value, label }) => ({ value, label }))}
          value={eventType}
          onChange={(val) => val && onUpdate({ eventType: val })}
          allowDeselect={false}
        />
        <Text size="xs" c="dimmed">
          {blurb}
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
            onChange={(val) => val && onUpdate({ timerType: val })}
            allowDeselect={false}
          />
          <TextInput
            label={timerType === 'date' ? 'Until' : 'For'}
            placeholder={
              timerType === 'date' ? '2026-01-01T12:00:00Z' : timerType === 'cycle' ? 'R3/PT1H' : 'PT1H'
            }
            description={
              timerType === 'date'
                ? 'A date and time, e.g. 2026-01-01T12:00:00Z'
                : timerType === 'cycle'
                  ? 'R3/PT1H is three times, an hour apart. R/PT1H keeps going.'
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

      {eventType === 'error' && (
        <PropertySection title="Which failure" hint="Leave this blank to catch anything that goes wrong.">
          <TextInput
            label="Failure code"
            placeholder="e.g. payment-declined"
            description="The code the failing step reports. Blank catches every failure."
            value={asText(data.errorCode)}
            onChange={(e) => onUpdate({ errorCode: e.target.value })}
          />
        </PropertySection>
      )}

      {eventType === 'escalation' && (
        <PropertySection title="What is being raised" hint="Leave this blank to catch anything raised here.">
          <TextInput
            label="Reason code"
            placeholder="e.g. over-approval-limit"
            description="Matches the code the step raises. Blank catches whatever is raised."
            value={asText(data.escalationCode)}
            onChange={(e) => onUpdate({ escalationCode: e.target.value })}
          />
        </PropertySection>
      )}

      {eventType === 'compensation' && (
        <PropertySection
          title="Undoing the work"
          hint="This runs only if the process is rolled back, and never stops the step it is attached to."
        >
          <Text size="xs" c="dimmed">
            Draw the flow from here to the step that reverses the work — cancelling the booking, refunding the payment.
          </Text>
        </PropertySection>
      )}

      {isBoundary && eventType !== 'compensation' && (
        <PropertySection title="What happens to the step it is attached to">
          <Checkbox
            label="Let the step carry on"
            description="Off, the step is stopped and the process takes this path instead. On, the step keeps going and this path runs alongside it — what a reminder needs."
            checked={Boolean(data.nonInterrupting)}
            onChange={(e) => onUpdate({ nonInterrupting: e.currentTarget.checked })}
          />
        </PropertySection>
      )}
    </Stack>
  );
}
