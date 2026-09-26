/**
 * The question a step is asking, answered on the step itself.
 *
 * The first version of this screen had a list of stubs in a sidebar, each one
 * starting with a text field where you typed the id of the node it applied to —
 * from memory, while the node sat on the canvas three inches away. This is that
 * feature with the memory test removed: click the step, answer the step.
 *
 * Two fields, never more. A person's task asks who and how long. A service asks
 * whether it works and what it gives back. A message asks when it arrives.
 * Anything else a simulation could take — retries, payload shapes, partial
 * failures — is a reason to write a real test, not a reason to grow this card.
 */
import {
  ActionIcon,
  Button,
  Card,
  Group,
  SegmentedControl,
  Select,
  Stack,
  Text,
  TextInput,
} from '@mantine/core';
import { NodeToolbar, Position } from '@xyflow/react';
import { Check, Trash2, X } from 'lucide-react';

import {
  DURATION_CHOICES,
  type SimulationAnswer,
} from '../../domain/simulationScenario';
import type { SimulationController } from '../../hooks/useSimulation';

export function SimulationAnswerCard({
  sim,
  nodeId,
  label,
}: {
  sim: SimulationController;
  nodeId: string;
  label: string;
}) {
  const answer = sim.answerAt(nodeId);
  if (answer === null) return null;

  return (
    <NodeToolbar nodeId={nodeId} isVisible position={Position.Bottom} offset={14}>
      <Card
        shadow="md"
        radius="md"
        withBorder
        padding="sm"
        w={268}
        // The canvas is behind this; without it clicks fall through to the pane
        // handler and close the card the moment you reach for a field.
        onClick={(event) => event.stopPropagation()}
      >
        <Stack gap="xs">
          <Group justify="space-between" wrap="nowrap" gap="xs">
            <Stack gap={0} style={{ minWidth: 0 }}>
              <Text size="xs" c="dimmed">
                {questionFor(answer)}
              </Text>
              <Text size="sm" fw={600} truncate>
                {label}
              </Text>
            </Stack>
            <ActionIcon variant="subtle" color="gray" size="sm" onClick={sim.stopAnswering} aria-label="Close">
              <X size={14} />
            </ActionIcon>
          </Group>

          <AnswerFields answer={answer} onChange={sim.saveAnswer} />

          <Group justify="space-between" wrap="nowrap" gap="xs" mt={2}>
            <ActionIcon
              variant="subtle"
              color="gray"
              size="sm"
              onClick={() => sim.clearAnswer(nodeId)}
              aria-label="Remove this answer"
            >
              <Trash2 size={14} />
            </ActionIcon>
            <Button
              size="compact-sm"
              leftSection={<Check size={14} />}
              onClick={() => {
                sim.stopAnswering();
                sim.start();
              }}
              loading={sim.running}
            >
              Run on
            </Button>
          </Group>
        </Stack>
      </Card>
    </NodeToolbar>
  );
}

/** The question, phrased for the kind of step this is. */
function questionFor(answer: SimulationAnswer): string {
  switch (answer.kind) {
    case 'person':
      return 'Who does this, and how long do they take?';
    case 'service':
      return 'What does this step do?';
    case 'message':
      return 'When does this arrive?';
  }
}

function AnswerFields({
  answer,
  onChange,
}: {
  answer: SimulationAnswer;
  onChange: (next: SimulationAnswer) => void;
}) {
  if (answer.kind === 'person') {
    return (
      <Stack gap="xs">
        <TextInput
          size="xs"
          placeholder="sarah"
          label="Completed by"
          value={answer.actor}
          onChange={(event) => onChange({ ...answer, actor: event.currentTarget.value })}
          data-autofocus
        />
        <DurationField
          label="After"
          value={answer.after}
          onChange={(after) => onChange({ ...answer, after })}
        />
      </Stack>
    );
  }

  if (answer.kind === 'message') {
    return (
      <DurationField
        label="Arrives after"
        value={answer.arrivesAfter}
        onChange={(arrivesAfter) => onChange({ ...answer, arrivesAfter })}
      />
    );
  }

  return (
    <Stack gap="xs">
      <SegmentedControl
        size="xs"
        fullWidth
        value={answer.outcome}
        onChange={(value) => onChange({ ...answer, outcome: value as 'succeed' | 'fail' })}
        data={[
          { value: 'succeed', label: 'It works' },
          { value: 'fail', label: 'It fails' },
        ]}
      />
      {answer.outcome === 'succeed' ? (
        <TextInput
          size="xs"
          label="And gives back"
          placeholder="score  720"
          value={answer.returns[0]?.value ?? ''}
          onChange={(event) =>
            onChange({
              ...answer,
              returns: [{ id: answer.returns[0]?.id ?? 'r', name: answer.returns[0]?.name ?? 'result', value: event.currentTarget.value }],
            })
          }
          description="Optional."
        />
      ) : (
        <TextInput
          size="xs"
          label="With error code"
          placeholder="TIMEOUT"
          value={answer.failureCode}
          onChange={(event) => onChange({ ...answer, failureCode: event.currentTarget.value })}
          description="A boundary error event catches this."
        />
      )}
    </Stack>
  );
}

/**
 * A duration in words.
 *
 * BPMN wants ISO-8601 and the engine is right to insist — but that is a wire
 * format, not something to make an analyst type. Picking "1 day" writes
 * `PT24H`, and the ISO string is never seen unless somebody types their own.
 */
function DurationField({
  label,
  value,
  onChange,
}: {
  label: string;
  value: string;
  onChange: (next: string) => void;
}) {
  return (
    <Select
      size="xs"
      label={label}
      data={DURATION_CHOICES}
      value={value}
      onChange={(next) => onChange(next ?? '')}
      allowDeselect={false}
      searchable
      // Typing something that is not on the list keeps it, so an odd SLA like
      // PT13M is still reachable without a second "custom" control.
      onSearchChange={(query) => {
        if (query !== '' && !DURATION_CHOICES.some((choice) => choice.label === query)) {
          onChange(query.toUpperCase());
        }
      }}
      comboboxProps={{ withinPortal: true }}
    />
  );
}
