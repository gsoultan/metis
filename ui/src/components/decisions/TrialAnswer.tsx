/**
 * The answer Try it got, as the decision editor shows it.
 */
import { Card, Group, Stack, Text } from '@mantine/core';
import { AlertCircle, CircleCheck } from 'lucide-react';

import { formatOutputValue, type DecisionOutputColumn } from '../../domain/decisionTable';
import { describeMatchedLines, type TrialOutcome, type TrialStanding } from '../../domain/decisionTrial';

/**
 * What Try it decided, pinned to the table it ran.
 *
 * The line numbers are the lines on screen the answer points at. An answer
 * that no longer describes the screen says so, rather than pointing at lines
 * that have changed under it.
 */
export function TrialAnswer({
  outcome,
  standing,
  staleReason,
  lines,
  outputs,
}: {
  outcome: TrialOutcome;
  standing: TrialStanding;
  /** What to say when the answer is stale. */
  staleReason: string;
  lines: number[];
  outputs: DecisionOutputColumn[];
}) {
  if (standing === 'stale') {
    return (
      <Text size="xs" c="dimmed">
        {staleReason}
      </Text>
    );
  }

  const decided = outcome.positions.length > 0;
  return (
    <Card withBorder radius="sm" p="xs" bg={decided ? 'var(--mantine-color-gray-0)' : 'var(--mantine-color-yellow-0)'}>
      <Stack gap={6}>
        {/*
          Nothing matching is not a success. It used to be reported under a
          green tick beside an empty result, so a table that quietly decides
          nothing looked like a table that worked — and the process carries on
          with the variable unset.
        */}
        <Group gap={6}>
          {decided ? (
            <CircleCheck size={14} color="var(--mantine-color-green-6)" />
          ) : (
            <AlertCircle size={14} color="var(--mantine-color-yellow-7)" />
          )}
          <Text size="xs" fw={600}>
            {decided ? describeMatchedLines(lines) : 'No line matched'}
          </Text>
        </Group>

        {standing === 'saved-only' && (
          <Text size="xs" c="dimmed">
            This is what the saved version decides. Your changes are not saved yet, so they are not part of it.
          </Text>
        )}

        {decided ? (
          /* Results named the way the columns are, rather than raw JSON. */
          <Stack gap={2}>
            {outputs.map((output) => (
              <Group key={output.id} gap={6} wrap="nowrap">
                <Text size="xs" c="dimmed">
                  {output.label || output.name}:
                </Text>
                <Text size="xs" fw={600}>
                  {formatOutputValue(outcome.values[output.name])}
                </Text>
              </Group>
            ))}
          </Stack>
        ) : (
          <Text size="xs" c="dimmed">
            The process would get no value for{' '}
            {outputs.map((output) => output.label).filter(Boolean).join(', ') || 'this table'}. Add a line that
            catches this case, or a catch-all line at the bottom.
          </Text>
        )}
      </Stack>
    </Card>
  );
}
