/**
 * One column of the decision graph: the decisions decided at one step of the
 * evaluation order.
 *
 * A column can hold hundreds of decisions — every decision that depends on
 * nothing sits in the first one — and drawing a card for each of them put a
 * thousand cards in the page. A long column scrolls, and only the cards in view
 * are drawn.
 */
import { Badge, Box, Card, Group, Stack, Text } from '@mantine/core';
import { ArrowRight } from 'lucide-react';
import { useRef, type KeyboardEvent } from 'react';

import type { GraphNode } from '../../domain/decisionGraph';
import { useVirtualWindow } from '../virtualWindow';

/** Above this many decisions a column scrolls and draws only what is in view. */
export const VIRTUALIZE_COLUMN_ABOVE = 40;

/** Roughly how tall a card is, before any has been measured. */
const ESTIMATED_CARD_HEIGHT = 76;
const COLUMN_HEIGHT = 480;
const OVERSCAN = 6;
const COLUMN_WIDTH = 220;

export interface GraphLayerProps {
  heading: string;
  nodes: GraphNode[];
  /** The decisions each key is required by, for the arrow under its card. */
  requiredBy: Map<string, string[]>;
  onOpen?: (id: string) => void;
}

export function DecisionGraphLayer({ heading, nodes, requiredBy, onOpen }: GraphLayerProps) {
  const scrollRef = useRef<HTMLDivElement>(null);
  const long = nodes.length > VIRTUALIZE_COLUMN_ABOVE;
  const virtualizer = useVirtualWindow({
    count: nodes.length,
    scrollRef,
    estimatedSize: ESTIMATED_CARD_HEIGHT,
    overscan: OVERSCAN,
    enabled: long,
  });
  const card = (node: GraphNode) => (
    <NodeCard key={node.id} node={node} requiredBy={requiredBy.get(node.key) ?? []} onOpen={onOpen} />
  );

  if (!long) {
    return (
      <Stack gap="xs" style={{ minWidth: COLUMN_WIDTH }}>
        <Text size="xs" c="dimmed" fw={600}>
          {heading}
        </Text>
        {nodes.map(card)}
      </Stack>
    );
  }

  const visible = virtualizer.getVirtualItems();
  return (
    <Stack gap="xs" style={{ minWidth: COLUMN_WIDTH }}>
      <Text size="xs" c="dimmed" fw={600}>
        {heading} ({nodes.length})
      </Text>
      <Box ref={scrollRef} style={{ height: COLUMN_HEIGHT, overflowY: 'auto' }}>
        {visible.length === 0 ? (
          // Before the column has been measured: the first screenful, so it is
          // never briefly empty.
          <Stack gap="xs">{nodes.slice(0, OVERSCAN * 2).map(card)}</Stack>
        ) : (
          <Box style={{ height: virtualizer.getTotalSize(), position: 'relative' }}>
            {visible.map((row) => (
              <Box
                key={nodes[row.index].id}
                data-index={row.index}
                ref={virtualizer.measureElement}
                style={{ position: 'absolute', top: 0, left: 0, right: 0, transform: `translateY(${row.start}px)` }}
                pb="xs"
              >
                {card(nodes[row.index])}
              </Box>
            ))}
          </Box>
        )}
      </Box>
    </Stack>
  );
}

function NodeCard({ node, requiredBy, onOpen }: { node: GraphNode; requiredBy: string[]; onOpen?: (id: string) => void }) {
  // A card that opens its decision is a button to the keyboard as well as to
  // the mouse: it was clickable and could not be reached with Tab.
  const opens = onOpen
    ? {
        role: 'button',
        tabIndex: 0,
        'aria-label': `Open ${node.label}`,
        onClick: () => onOpen(node.id),
        onKeyDown: (event: KeyboardEvent<HTMLDivElement>) => {
          if (event.key !== 'Enter' && event.key !== ' ') return;
          event.preventDefault();
          onOpen(node.id);
        },
      }
    : {};
  return (
    <Card
      {...opens}
      withBorder
      radius="md"
      p="sm"
      style={{ cursor: onOpen ? 'pointer' : undefined }}
      bg={node.inCycle ? 'var(--mantine-color-red-0)' : undefined}
    >
      <Stack gap={4}>
        <Group gap="xs" wrap="nowrap" justify="space-between">
          <Text size="sm" fw={500} truncate>
            {node.label}
          </Text>
          {node.inCycle && (
            <Badge size="xs" color="red" variant="light">
              loop
            </Badge>
          )}
        </Group>
        {requiredBy.length > 0 && (
          <Group gap={4} wrap="nowrap">
            <ArrowRight size={11} color="var(--mantine-color-dimmed)" />
            <Text size="xs" c="dimmed" truncate>
              {requiredBy.join(', ')}
            </Text>
          </Group>
        )}
        {node.missing.length > 0 && (
          <Text size="xs" c="yellow.8">
            needs {node.missing.join(', ')}
          </Text>
        )}
      </Stack>
    </Card>
  );
}
