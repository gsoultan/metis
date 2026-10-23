/**
 * "Try a case", as a line of the table itself: a value under each condition,
 * the answer under each result, and Run at the end of the line.
 *
 * It is Try it put where the eye already is. It runs the same stored table the
 * Try it tab runs, from the same values, and the lines it matched light up
 * just below it.
 */
import { ActionIcon, Badge, Select, Table, Text, TextInput, Tooltip } from '@mantine/core';
import { FlaskConical, Play } from 'lucide-react';

import { formatOutputValue, type DecisionInputColumn, type DecisionOutputColumn } from '../../domain/decisionTable';
import {
  describeMatchedLines,
  trialValueOf,
  withTrialValue,
  type TrialTarget,
  type TrialValues,
} from '../../domain/decisionTrial';
import type { ShownTrial } from './TrialPanel';
import classes from './TrialRow.module.css';

export interface TrialRowProps {
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

export function TrialRow({ target, inputs, outputs, values, onValues, onRun, running, error, answer }: TrialRowProps) {
  // An answer about a table that has since changed is not shown in the cells:
  // it would sit beside results it was not about.
  const shown = answer && answer.standing !== 'stale' ? answer : null;
  const decided = shown !== null && shown.outcome.positions.length > 0;

  return (
    <Table.Tr className={classes.row}>
      <Table.Td ta="center">
        <Tooltip label="Try a case: fill in values and press Enter" withArrow>
          <FlaskConical size={15} color="var(--mantine-color-orange-6)" aria-hidden />
        </Tooltip>
      </Table.Td>

      {inputs.map((input) => (
        <Table.Td key={input.id}>
          <TrialCell
            input={input}
            value={trialValueOf(values, input)}
            onChange={(text) => onValues(withTrialValue(values, input, text))}
            onEnter={() => target && !running && onRun()}
          />
        </Table.Td>
      ))}

      {outputs.map((output) => (
        <Table.Td key={output.id}>
          {decided ? (
            <Text size="sm" fw={700} c="orange.8">
              {formatOutputValue(shown.outcome.values[output.name])}
            </Text>
          ) : (
            <Text size="sm" c="dimmed">
              —
            </Text>
          )}
        </Table.Td>
      ))}

      <Table.Td>
        <TrialStatus target={target} error={error} shown={shown} answer={answer} />
      </Table.Td>

      <Table.Td ta="center">
        <Tooltip label={target ? `Run against v${target.version}` : 'Save the table first'} withArrow>
          <ActionIcon
            aria-label="Run this case"
            color="orange"
            variant="filled"
            radius="xl"
            size="sm"
            onClick={onRun}
            loading={running}
            disabled={!target}
          >
            <Play size={12} />
          </ActionIcon>
        </Tooltip>
      </Table.Td>
    </Table.Tr>
  );
}

/** One line of what happened, in the note column. */
function TrialStatus({
  target,
  error,
  shown,
  answer,
}: {
  target: TrialTarget | null;
  error: string | null;
  shown: ShownTrial | null;
  answer: ShownTrial | null;
}) {
  if (!target) {
    return (
      <Text size="xs" c="dimmed">
        Save the table to try a case
      </Text>
    );
  }
  if (error) {
    return (
      <Text size="xs" c="red.7" lineClamp={2}>
        {error}
      </Text>
    );
  }
  if (answer && !shown) {
    return (
      <Text size="xs" c="dimmed" lineClamp={2}>
        {answer.staleReason}
      </Text>
    );
  }
  if (!shown) {
    return (
      <Text size="xs" c="dimmed">
        Try a case against v{target.version}
      </Text>
    );
  }
  if (shown.outcome.positions.length === 0) {
    return (
      <Badge color="yellow" variant="light" size="sm" styles={{ label: { textTransform: 'none' } }}>
        No line matched
      </Badge>
    );
  }
  return (
    <Badge color="orange" variant="light" size="sm" styles={{ label: { textTransform: 'none' } }}>
      {describeMatchedLines(shown.lines)}
      {shown.standing === 'saved-only' ? ' · saved version' : ''}
    </Badge>
  );
}

/** A value for one condition, compact enough to sit in a cell. */
function TrialCell({
  input,
  value,
  onChange,
  onEnter,
}: {
  input: DecisionInputColumn;
  value: string;
  onChange: (text: string) => void;
  onEnter: () => void;
}) {
  const label = `Try a value for ${input.label || input.expression}`;
  if (input.type === 'boolean') {
    return (
      <Select
        size="xs"
        variant="filled"
        aria-label={label}
        value={value}
        onChange={(next) => onChange(next ?? '')}
        data={[
          { value: 'true', label: 'Yes' },
          { value: 'false', label: 'No' },
        ]}
        placeholder="Yes or no"
        clearable
      />
    );
  }
  return (
    <TextInput
      size="xs"
      variant="filled"
      aria-label={label}
      placeholder={input.type === 'number' ? 'e.g. 100' : 'A value'}
      value={value}
      onChange={(event) => onChange(event.currentTarget.value)}
      onKeyDown={(event) => {
        if (event.key === 'Enter') onEnter();
      }}
    />
  );
}
