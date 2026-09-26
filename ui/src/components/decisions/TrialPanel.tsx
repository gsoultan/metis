/**
 * "Try it": values for the table's conditions, a Run button, and the answer.
 *
 * It runs the stored table — the key and the version the editor loaded, which
 * the page passes in as the target — not what is on screen, and says so. What
 * it sends is built by trialRequest; the page owns the answer, because the grid
 * highlights the lines it points at.
 */
import { Alert, Button, Group, Paper, Select, Stack, Text, TextInput, Title } from '@mantine/core';
import { AlertCircle, FlaskConical, Play } from 'lucide-react';

import type { DecisionInputColumn, DecisionOutputColumn } from '../../domain/decisionTable';
import {
  trialValueOf,
  withTrialValue,
  type TrialOutcome,
  type TrialStanding,
  type TrialTarget,
  type TrialValues,
} from '../../domain/decisionTrial';
import { TrialAnswer } from './TrialAnswer';

/** An answer, and where it stands against the table on screen. */
export interface ShownTrial {
  outcome: TrialOutcome;
  standing: TrialStanding;
  staleReason: string;
  /** The lines on screen it points at. */
  lines: number[];
}

export interface TrialPanelProps {
  /** The stored table Try it runs; null for a table that was never saved. */
  target: TrialTarget | null;
  inputs: DecisionInputColumn[];
  outputs: DecisionOutputColumn[];
  values: TrialValues;
  onValues: (values: TrialValues) => void;
  onRun: () => void;
  running: boolean;
  error: string | null;
  answer: ShownTrial | null;
}

export function TrialPanel({ target, inputs, outputs, values, onValues, onRun, running, error, answer }: TrialPanelProps) {
  return (
    <Paper radius="md" withBorder p="md">
      <Stack gap="sm">
        <Group gap={6}>
          <FlaskConical size={15} color="var(--mantine-color-orange-6)" />
          <Title order={6}>Try it</Title>
        </Group>
        <Text size="xs" c="dimmed">
          {target
            ? `Runs the saved version (v${target.version}) and highlights the lines that matched.`
            : 'Save the table first: Try it runs the saved version.'}
        </Text>

        {inputs.map((input) => (
          <TrialField key={input.id} input={input} value={trialValueOf(values, input)} onChange={(text) => onValues(withTrialValue(values, input, text))} />
        ))}

        <Button size="xs" color="orange" leftSection={<Play size={14} />} onClick={onRun} loading={running} disabled={!target}>
          Run
        </Button>

        {error && (
          <Alert variant="light" color="red" icon={<AlertCircle size={14} />} py="xs">
            <Text size="xs">{error}</Text>
          </Alert>
        )}

        {answer && (
          <TrialAnswer
            outcome={answer.outcome}
            standing={answer.standing}
            staleReason={answer.staleReason}
            lines={answer.lines}
            outputs={outputs}
          />
        )}
      </Stack>
    </Paper>
  );
}

/** One condition's value: yes or no for a yes/no column, typed for the rest. */
function TrialField({
  input,
  value,
  onChange,
}: {
  input: DecisionInputColumn;
  value: string;
  onChange: (text: string) => void;
}) {
  if (input.type === 'boolean') {
    return (
      <Select
        size="xs"
        label={input.label}
        value={value}
        onChange={(next) => onChange(next ?? '')}
        data={[
          { value: 'true', label: 'Yes' },
          { value: 'false', label: 'No' },
        ]}
        placeholder="Not given"
        clearable
      />
    );
  }
  return (
    <TextInput
      size="xs"
      label={input.label}
      placeholder={input.type === 'number' ? '100' : 'Sample value'}
      value={value}
      onChange={(event) => onChange(event.currentTarget.value)}
    />
  );
}
