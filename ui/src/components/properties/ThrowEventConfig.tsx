import { Alert, Select, Stack, Text, TextInput } from '@mantine/core';

import type { NodeConfigProps } from '../PropertyPanel';
import { asText } from '../../types/bpmn';
import { PropertySection } from './PropertySection';

/**
 * A step that tells the rest of the world something.
 *
 * The catching half of every one of these was drawable and this half was not:
 * the engine has run error, escalation, compensation and announcement events
 * since boundary events landed, but none could be placed on a canvas, so the
 * only way to author one was to write BPMN XML by hand and import it.
 *
 * Each kind needs exactly one thing said about it, and that thing is the code
 * a listener matches on. Left empty, most of them still work — a boundary with
 * no code catches anything — so the fields explain what naming one buys rather
 * than demanding it.
 */

/** How the announcement reaches whoever is waiting. */
const ANNOUNCEMENT_KINDS = [
  { value: 'signal', label: 'Anyone listening (a broadcast)' },
  { value: 'message', label: 'One particular process' },
];

/** Which of the four this is, from the node type the canvas gave us. */
function throwKind(data: NodeConfigProps['data']): string {
  return asText(data.nodeType);
}

export function ThrowEventConfig({ data, onUpdate }: NodeConfigProps) {
  switch (throwKind(data)) {
    case 'errorEndEvent':
      return <ErrorEndFields data={data} onUpdate={onUpdate} />;
    case 'escalationThrowEvent':
      return <EscalationFields data={data} onUpdate={onUpdate} />;
    case 'compensationThrowEvent':
      return <CompensationFields data={data} onUpdate={onUpdate} />;
    default:
      return <AnnouncementFields data={data} onUpdate={onUpdate} />;
  }
}

function ErrorEndFields({ data, onUpdate }: NodeConfigProps) {
  return (
    <Stack gap="xl">
      <PropertySection
        title="What went wrong"
        hint="The name a handler elsewhere in the process listens for."
      >
        <TextInput
          label="Problem name"
          placeholder="e.g. PAYMENT_DECLINED"
          value={asText(data.errorCode)}
          onChange={(e) => onUpdate({ errorCode: e.target.value })}
        />
      </PropertySection>

      <Alert variant="light" color="gray" p="sm">
        <Text size="xs">
          This path stops here and reports the problem. To react to it, attach an
          &ldquo;If this goes wrong&rdquo; step to the surrounding activity and give it the
          same name. A handler with no name set catches every problem, so leaving
          this empty still works — naming it is how you route two different
          failures down two different paths.
        </Text>
      </Alert>
    </Stack>
  );
}

function EscalationFields({ data, onUpdate }: NodeConfigProps) {
  return (
    <Stack gap="xl">
      <PropertySection
        title="What to raise"
        hint="The name the handler waiting for this listens for."
      >
        <TextInput
          label="Escalation name"
          placeholder="e.g. NEEDS_MANAGER"
          value={asText(data.escalationCode)}
          onChange={(e) => onUpdate({ escalationCode: e.target.value })}
        />
      </PropertySection>

      <Alert variant="light" color="gray" p="sm">
        <Text size="xs">
          Raising something is not the same as failing: this step carries on
          afterwards, and the work continues while somebody else picks the
          escalation up. Use &ldquo;Finish with a problem&rdquo; instead when this path
          should stop.
        </Text>
      </Alert>
    </Stack>
  );
}

function CompensationFields({ data, onUpdate }: NodeConfigProps) {
  return (
    <Stack gap="xl">
      <PropertySection
        title="What to undo"
        hint="Leave empty to undo everything that has already finished."
      >
        <TextInput
          label="Step to undo"
          placeholder="e.g. reserve_stock"
          value={asText(data.activityRef)}
          onChange={(e) => onUpdate({ activityRef: e.target.value })}
        />
      </PropertySection>

      <Alert variant="light" color="gray" p="sm">
        <Text size="xs">
          Undoing runs the compensation attached to steps that already completed,
          newest first — so stock is released before the deposit is refunded, not
          after. Naming one step undoes only that step.
        </Text>
      </Alert>
    </Stack>
  );
}

function AnnouncementFields({ data, onUpdate }: NodeConfigProps) {
  // A message names one recipient and a signal names none, so the field that
  // was filled in is what decides — the same rule the engine applies.
  const kind = data.messageName ? 'message' : 'signal';

  return (
    <Stack gap="xl">
      <PropertySection title="Who hears it">
        <Select
          label="Announce to"
          data={ANNOUNCEMENT_KINDS}
          value={kind}
          allowDeselect={false}
          onChange={(value) =>
            // Clearing the other field matters: with both set the engine would
            // broadcast *and* send, which is two events where the author drew one.
            value === 'message'
              ? onUpdate({ messageName: asText(data.signalName), signalName: '' })
              : onUpdate({ signalName: asText(data.messageName), messageName: '', correlationKey: '' })
          }
        />
      </PropertySection>

      {kind === 'signal' ? (
        <PropertySection
          title="What to announce"
          hint="Every process waiting for this name carries on."
        >
          <TextInput
            label="Signal name"
            placeholder="e.g. PAYMENT_CLEARED"
            value={asText(data.signalName)}
            onChange={(e) => onUpdate({ signalName: e.target.value })}
          />
        </PropertySection>
      ) : (
        <PropertySection
          title="What to send"
          hint="The matching value decides which single process receives it."
        >
          <TextInput
            label="Message name"
            placeholder="e.g. ORDER_READY"
            value={asText(data.messageName)}
            onChange={(e) => onUpdate({ messageName: e.target.value })}
          />
          <TextInput
            mt="sm"
            label="Matching value"
            placeholder="e.g. ${orderId}"
            description="Use ${variable} to take the value from this process."
            value={asText(data.correlationKey)}
            onChange={(e) => onUpdate({ correlationKey: e.target.value })}
          />
        </PropertySection>
      )}

      <Alert variant="light" color="gray" p="sm">
        <Text size="xs">
          The process does not wait here. It says the thing and carries straight
          on — use a waiting step if it should pause for a reply.
        </Text>
      </Alert>
    </Stack>
  );
}
