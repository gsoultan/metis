import { Box, Group, Stack, Text } from '@mantine/core';
import type { ReactNode } from 'react';

interface PropertySectionProps {
  title: string;
  /** One line saying what the section is for, when the title is not enough. */
  hint?: string;
  children: ReactNode;
}

/**
 * One group of related settings.
 *
 * The forms had grown their own headings — a coloured icon tile, a bold title
 * at a size each form chose, spacing set per field — so moving between two node
 * types felt like moving between two applications, and the panel read as a
 * stack of unrelated boxes rather than one form.
 *
 * A heading is a small uppercase label here. The fields are what the eye should
 * land on, and a row of large icons down the left competes with them for the
 * same attention.
 */
export function PropertySection({ title, hint, children }: PropertySectionProps) {
  return (
    <Box>
      <Group gap="xs" mb={hint ? 2 : 10} align="baseline">
        <Text size="xs" fw={700} tt="uppercase" c="dimmed" style={{ letterSpacing: '0.04em' }}>
          {title}
        </Text>
      </Group>

      {hint && (
        <Text size="xs" c="dimmed" mb={10}>
          {hint}
        </Text>
      )}

      <Stack gap="sm">{children}</Stack>
    </Box>
  );
}
