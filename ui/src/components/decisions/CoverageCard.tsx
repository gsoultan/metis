/**
 * The decision editor's coverage card: the cases no line decides, and a line
 * for each in one click.
 */
import { Button, Group, Paper, Stack, Text, Title, Tooltip } from '@mantine/core';
import { CircleCheck, Info, Plus } from 'lucide-react';

import { whyNotChecked, type CoverageGap, type CoverageReport } from '../../domain/decisionCoverage';

/**
 * The cases no line decides.
 *
 * A table that leaves a case undecided returns nothing for it, and the process
 * carries on with the variable unset until something downstream fails for a
 * reason that looks unrelated. The analysis was written and never shown; this
 * is where it is shown, with a way to close each gap.
 */
export function CoverageCard({
  report,
  ruleCount,
  onAddLine,
}: {
  report: CoverageReport;
  ruleCount: number;
  onAddLine: (gap: CoverageGap) => void;
}) {
  return (
    <Paper radius="md" withBorder p="md">
      <Stack gap="sm">
        <Group gap={6}>
          <Title order={6}>Cases nothing decides</Title>
          <Tooltip
            label="Combinations of the values this table mentions that no line applies to. The process gets no value for them."
            multiline
            w={240}
            withArrow
          >
            <Info size={13} color="var(--mantine-color-dimmed)" />
          </Tooltip>
        </Group>
        <CoverageFindings report={report} ruleCount={ruleCount} onAddLine={onAddLine} />
      </Stack>
    </Paper>
  );
}

function CoverageFindings({
  report,
  ruleCount,
  onAddLine,
}: {
  report: CoverageReport;
  ruleCount: number;
  onAddLine: (gap: CoverageGap) => void;
}) {
  if (ruleCount === 0) {
    return (
      <Text size="xs" c="dimmed">
        Nothing to check until the table has a line.
      </Text>
    );
  }

  // Refusing to guess is reported as such, not as a clean bill of health.
  if (report.notAnalysed.length > 0) {
    return (
      <Text size="xs" c="dimmed">
        {whyNotChecked(report)}
      </Text>
    );
  }

  if (report.gaps.length === 0) {
    return report.truncated ? (
      <Text size="xs" c="dimmed">
        No undecided case turned up, but the table has more combinations than this check tries.
      </Text>
    ) : (
      <Group gap={6} wrap="nowrap">
        <CircleCheck size={14} color="var(--mantine-color-green-6)" />
        <Text size="xs">Every case has a line that decides it.</Text>
      </Group>
    );
  }

  return (
    <Stack gap="xs">
      {report.gaps.map((gap) => (
        <Group key={gap.description} justify="space-between" wrap="nowrap" gap="xs" align="flex-start">
          <Text size="xs">{gap.description}.</Text>
          <Button
            size="compact-xs"
            variant="light"
            leftSection={<Plus size={12} />}
            // The name starts with what the button shows, so "Add line" said
            // aloud finds it (WCAG 2.5.3, label in name).
            aria-label={`Add line for this case: ${gap.description}`}
            onClick={() => onAddLine(gap)}
            style={{ flexShrink: 0 }}
          >
            Add line
          </Button>
        </Group>
      ))}
      {report.truncated && (
        <Text size="xs" c="dimmed">
          There may be more: the check stopped before trying every combination.
        </Text>
      )}
    </Stack>
  );
}
