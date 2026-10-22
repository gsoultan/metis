import { Badge, Box, Button, Group, Popover, ScrollArea, Stack, Text, UnstyledButton } from '@mantine/core';
import { useDisclosure } from '@mantine/hooks';
import { AlertCircle, AlertTriangle, CheckCircle2, ChevronRight } from 'lucide-react';

import type { ValidationIssue } from '../../domain/processValidation';
import classes from './IssuesButton.module.css';

interface IssuesButtonProps {
  issues: ValidationIssue[];
  /** Selects the step or path an issue is about. */
  onSelect: (id: string) => void;
}

/**
 * What stands between this process and a deploy, one click from the step.
 *
 * The list used to float in the canvas's corner, over the diagram, with each
 * line truncated and nothing to click: it named a problem and left the
 * person to find the step on their own. It now sits beside Deploy — the
 * button it gates — and each line opens the step it is about.
 */
export function IssuesButton({ issues, onSelect }: IssuesButtonProps) {
  const [opened, { toggle, close }] = useDisclosure(false);
  const errors = issues.filter((issue) => issue.severity === 'error').length;
  const warnings = issues.length - errors;

  if (issues.length === 0) {
    return (
      <Badge
        variant="light"
        color="teal"
        size="lg"
        radius="sm"
        leftSection={<CheckCircle2 size={14} />}
        styles={{ label: { textTransform: 'none', fontWeight: 600 } }}
      >
        Ready to deploy
      </Badge>
    );
  }

  const summary =
    errors > 0
      ? `${errors} to fix${warnings > 0 ? `, ${warnings} to check` : ''}`
      : `${warnings} to check`;

  return (
    <Popover opened={opened} onChange={(open) => (open ? undefined : close())} width={380} position="bottom-end" shadow="md" radius="md" withArrow>
      <Popover.Target>
        <Button
          size="xs"
          variant="light"
          color={errors > 0 ? 'red' : 'orange'}
          leftSection={errors > 0 ? <AlertCircle size={14} /> : <AlertTriangle size={14} />}
          aria-label={`Checks: ${summary}`}
          onClick={toggle}
        >
          {summary}
        </Button>
      </Popover.Target>
      <Popover.Dropdown p={0}>
        <Box px="md" pt="sm" pb="xs">
          <Text size="sm" fw={700}>
            Before you deploy
          </Text>
          <Text size="xs" c="dimmed">
            {errors > 0
              ? 'Problems stop a deploy; warnings do not. Click one to open its step.'
              : 'None of these stop a deploy. Click one to open its step.'}
          </Text>
        </Box>
        <ScrollArea.Autosize mah={360} type="auto">
          <Stack gap={0} pb="xs">
            {issues.map((issue, index) => {
              const content = (
                <Group gap="sm" wrap="nowrap" align="flex-start">
                  <Box mt={2} c={issue.severity === 'error' ? 'red.6' : 'orange.6'} style={{ flexShrink: 0 }}>
                    {issue.severity === 'error' ? <AlertCircle size={16} /> : <AlertTriangle size={16} />}
                  </Box>
                  <Box style={{ flex: 1, minWidth: 0 }}>
                    <Text size="sm" fw={500}>
                      {issue.message}
                    </Text>
                    {issue.suggestion && (
                      <Text size="xs" c="dimmed">
                        {issue.suggestion}
                      </Text>
                    )}
                  </Box>
                  {issue.id && <ChevronRight size={14} className={classes.chevron} aria-hidden />}
                </Group>
              );
              return issue.id ? (
                <UnstyledButton
                  // Issues have no id of their own; the message and the
                  // position together are stable for one render of the list.
                  key={`${issue.id}-${index}`}
                  className={classes.row}
                  onClick={() => {
                    // Closed, so the step it opens is not hidden behind it.
                    close();
                    onSelect(issue.id as string);
                  }}
                >
                  {content}
                </UnstyledButton>
              ) : (
                <Box key={`whole-${index}`} className={classes.row}>
                  {content}
                </Box>
              );
            })}
          </Stack>
        </ScrollArea.Autosize>
      </Popover.Dropdown>
    </Popover>
  );
}
