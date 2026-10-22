import { Badge, Collapse, Group, Stack, Text } from '@mantine/core';
import { ChevronDown, ChevronRight } from 'lucide-react';

/** A heading that folds, used only where folding earns its keep. */
export function Section({
  label,
  count,
  opened,
  collapsible,
  onToggle,
  children,
}: {
  label: string;
  count: number;
  opened: boolean;
  collapsible: boolean;
  onToggle: () => void;
  children: React.ReactNode;
}) {
  const heading = (
    <Group gap={6} wrap="nowrap">
      {collapsible && (opened ? <ChevronDown size={14} /> : <ChevronRight size={14} />)}
      <Text size="sm" fw={600}>
        {label}
      </Text>
      {count > 0 && (
        <Badge size="xs" variant="default" radius="sm" fw={500}>
          {count}
        </Badge>
      )}
    </Group>
  );

  return (
    <Stack gap={6}>
      {collapsible ? (
        <Group
          gap={6}
          onClick={onToggle}
          style={{ cursor: 'pointer', userSelect: 'none' }}
          role="button"
          tabIndex={0}
          aria-expanded={opened}
          onKeyDown={(event) => (event.key === 'Enter' || event.key === ' ') && onToggle()}
        >
          {heading}
        </Group>
      ) : (
        heading
      )}
      <Collapse expanded={opened}>{children}</Collapse>
    </Stack>
  );
}
