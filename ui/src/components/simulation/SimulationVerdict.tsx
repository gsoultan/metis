/**
 * How the run ended, in one line, at the top where it is read first.
 *
 * Four tones, and the distinction between two of them is the design: a run that
 * stopped to *ask* something is the process working, and it gets a calm blue
 * with an obvious next action. A run that stopped because the *model* is wrong
 * is the finding somebody came here for, and it gets red. Collapsing those into
 * one "error" state is what makes a simulator feel like it is failing when it
 * is doing its job.
 */
import { Button, Group, Paper, Stack, Text, ThemeIcon } from '@mantine/core';
import { AlertTriangle, CircleCheck, HelpCircle, OctagonAlert } from 'lucide-react';

import type { Verdict } from '../../domain/simulationTrace';

const TONES = {
  good: { color: 'teal', Icon: CircleCheck },
  asking: { color: 'blue', Icon: HelpCircle },
  bad: { color: 'red', Icon: OctagonAlert },
  warn: { color: 'orange', Icon: AlertTriangle },
} as const;

export function SimulationVerdict({
  verdict,
  onAnswer,
}: {
  verdict: Verdict;
  /** Offered only when the run is waiting on a step that can be answered. */
  onAnswer?: () => void;
}) {
  const { color, Icon } = TONES[verdict.tone];

  return (
    <Paper
      radius="md"
      p="sm"
      style={{
        backgroundColor: `var(--mantine-color-${color}-light)`,
        border: `1px solid var(--mantine-color-${color}-light-hover)`,
      }}
    >
      <Group align="flex-start" wrap="nowrap" gap="sm">
        <ThemeIcon variant="transparent" color={color} size="sm">
          <Icon size={18} />
        </ThemeIcon>

        <Stack gap={4} style={{ minWidth: 0, flex: 1 }}>
          <Text size="sm" fw={600} lh={1.35}>
            {verdict.headline}
          </Text>
          {verdict.detail !== undefined && (
            <Text size="xs" c="dimmed" lh={1.45}>
              {verdict.detail}
            </Text>
          )}
          {onAnswer !== undefined && (
            <Button size="compact-xs" variant="light" color={color} onClick={onAnswer} mt={2} w="fit-content">
              Answer it
            </Button>
          )}
        </Stack>
      </Group>
    </Paper>
  );
}
