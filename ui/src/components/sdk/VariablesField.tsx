/**
 * The box you type process variables into.
 *
 * Variables are JSON on the wire, and pretending otherwise — a key/value grid —
 * loses nested objects and the difference between `"42"` and `42`, which is
 * exactly the difference a process gets wrong. So it is JSON, with the errors
 * written for somebody who is testing a process rather than writing a parser.
 *
 * The error appears under the box and never blocks typing: a half-typed object
 * is invalid for as long as it takes to finish typing it, and a field that
 * shouts on every keystroke is a field people stop reading.
 */
import { Button, Group, Stack, Text, Textarea } from '@mantine/core';
import { Braces, WandSparkles } from 'lucide-react';
import type { ReactNode } from 'react';

import { formatVariables } from '../../domain/variablesInput';

interface VariablesFieldProps {
  label: string;
  description?: string;
  value: string;
  onChange: (next: string) => void;
  error: string | null;
  placeholder?: string;
  disabled?: boolean;
  /** Extra actions beside Format — "copy what the task was given", say. */
  actions?: ReactNode;
  minRows?: number;
}

export function VariablesField({
  label,
  description,
  value,
  onChange,
  error,
  placeholder = '{\n  "amount": 42.5\n}',
  disabled = false,
  actions,
  minRows = 3,
}: VariablesFieldProps) {
  return (
    <Stack gap={6}>
      <Group justify="space-between" align="flex-end" wrap="nowrap" gap="sm">
        <div style={{ minWidth: 0 }}>
          <Text size="sm" fw={500} component="label" htmlFor={fieldId(label)}>
            {label}
          </Text>
          {description && (
            <Text size="xs" c="dimmed">
              {description}
            </Text>
          )}
        </div>
        <Group gap={6} wrap="nowrap">
          {actions}
          <Button
            size="compact-xs"
            variant="subtle"
            color="gray"
            leftSection={<WandSparkles size={13} />}
            onClick={() => onChange(formatVariables(value))}
            disabled={disabled || value.trim() === ''}
          >
            Tidy up
          </Button>
        </Group>
      </Group>

      <Textarea
        id={fieldId(label)}
        aria-label={label}
        value={value}
        onChange={(event) => onChange(event.currentTarget.value)}
        placeholder={placeholder}
        autosize
        minRows={minRows}
        maxRows={14}
        disabled={disabled}
        error={error ?? undefined}
        spellCheck={false}
        autoComplete="off"
        styles={{
          input: {
            fontFamily: 'var(--mantine-font-family-monospace)',
            fontSize: '0.8rem',
            lineHeight: 1.6,
          },
        }}
      />

      {error === null && value.trim() === '' && (
        <Group gap={6} wrap="nowrap">
          <Braces size={12} aria-hidden />
          <Text size="xs" c="dimmed">
            Leave this empty to send no variables.
          </Text>
        </Group>
      )}
    </Stack>
  );
}

/** A stable id from the label, so the label element actually points at the box. */
function fieldId(label: string): string {
  return `vars-${label.toLowerCase().replace(/[^a-z0-9]+/g, '-')}`;
}
