/**
 * A condition cell of the decision editor: free text, with the notation
 * available from a menu and read back in words on hover.
 */
import { ActionIcon, Code, Group, Menu, Text, TextInput, Tooltip, rem } from '@mantine/core';
import { ChevronDown } from 'lucide-react';

import { ANY_VALUE, CELL_TEMPLATES, describeCell, validateCell, type ColumnHeadings } from '../../domain/decisionTable';
import type { GridCellProps } from './gridCell';

export function ConditionCell({
  value,
  type,
  columnLabel,
  headings,
  cellProps,
  onChange,
}: {
  value: string;
  type: string;
  columnLabel: string;
  /** The table's condition columns, so a cell naming one reads as that column. */
  headings: ColumnHeadings;
  cellProps: GridCellProps;
  onChange: (next: string) => void;
}) {
  const templates = CELL_TEMPLATES[type] ?? CELL_TEMPLATES.string;
  const problem = validateCell(value);
  const meaning = problem ?? describeCell(value, columnLabel, headings);

  return (
    <Group gap={0} wrap="nowrap" align="center">
      <Tooltip label={meaning} openDelay={problem ? 0 : 400} position="top-start" withArrow color={problem ? 'red' : undefined}>
        <TextInput
          {...cellProps}
          variant="unstyled"
          px="sm"
          aria-label={`${columnLabel} condition`}
          // Mantine writes the input's aria-invalid from `error`, over any
          // aria-invalid passed to it, so this is how a screen reader hears
          // that the condition is wrong. A bare `true` shows no message, and
          // the red error styling stays off for the underline below.
          error={problem ? true : undefined}
          withErrorStyles={false}
          placeholder={ANY_VALUE}
          value={value}
          onChange={(event) => onChange(event.currentTarget.value)}
          styles={{
            input: {
              fontSize: rem(13),
              // A wavy underline rather than a red box: the cell is still being
              // typed, and a box round every half-written condition is noise.
              textDecoration: problem ? 'underline wavy var(--mantine-color-red-6)' : undefined,
            },
            root: { flex: 1 },
          }}
        />
      </Tooltip>
      <Menu position="bottom-end" shadow="md" width={260}>
        <Menu.Target>
          <ActionIcon aria-label={`Condition choices for ${columnLabel}`} size="xs" variant="subtle" color="gray" mr={4}>
            <ChevronDown size={12} />
          </ActionIcon>
        </Menu.Target>
        <Menu.Dropdown>
          <Menu.Label>{columnLabel}</Menu.Label>
          {templates.map((template) => (
            <Menu.Item key={template.value} onClick={() => onChange(template.value)}>
              <Group justify="space-between" wrap="nowrap" gap="sm">
                <Text size="xs">{template.label}</Text>
                <Code fz={10}>{template.value}</Code>
              </Group>
            </Menu.Item>
          ))}
        </Menu.Dropdown>
      </Menu>
    </Group>
  );
}
