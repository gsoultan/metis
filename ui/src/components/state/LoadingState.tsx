import { Card, Group, Skeleton, SimpleGrid, Stack, Table } from '@mantine/core';

/**
 * The state a surface shows while its request is in flight.
 *
 * Three of eighteen pages had one. The rest rendered nothing, or — worse, on
 * the dashboard — rendered zeros, which is indistinguishable from a real and
 * genuinely idle system. "We don't know yet" and "we know, and it's none" are
 * opposite meanings and must not share a visual.
 *
 * These are skeletons rather than spinners on purpose: a skeleton in the shape
 * of the incoming content preserves layout, so nothing jumps when data lands,
 * and it communicates *what* is coming, not merely that something is.
 *
 * Every variant is `aria-busy` with a polite live region, so a screen reader
 * announces the wait once rather than reading a wall of placeholder boxes.
 */

function Busy({ label, children }: { label: string; children: React.ReactNode }) {
  return (
    <div role="status" aria-busy aria-live="polite" aria-label={label}>
      {children}
    </div>
  );
}

/** Placeholder rows matching a data table. */
export function TableLoadingState({ rows = 5, columns = 4 }: { rows?: number; columns?: number }) {
  return (
    <Busy label="Loading table contents">
      <Table>
        <Table.Thead>
          <Table.Tr>
            {Array.from({ length: columns }, (_, i) => (
              <Table.Th key={i}>
                <Skeleton height={10} width="55%" radius="sm" />
              </Table.Th>
            ))}
          </Table.Tr>
        </Table.Thead>
        <Table.Tbody>
          {Array.from({ length: rows }, (_, row) => (
            <Table.Tr key={row}>
              {Array.from({ length: columns }, (_, col) => (
                <Table.Td key={col}>
                  {/* Varying widths read as text rather than as a grid of bars. */}
                  <Skeleton height={12} width={col === 0 ? '70%' : '45%'} radius="sm" />
                </Table.Td>
              ))}
            </Table.Tr>
          ))}
        </Table.Tbody>
      </Table>
    </Busy>
  );
}

/** Placeholder cards matching a grid of items. */
export function CardGridLoadingState({ count = 6, cols = 3 }: { count?: number; cols?: number }) {
  return (
    <Busy label="Loading items">
      <SimpleGrid cols={{ base: 1, sm: 2, md: cols }} spacing="lg">
        {Array.from({ length: count }, (_, i) => (
          <Card key={i} padding="lg">
            <Group justify="space-between" mb="md">
              <Skeleton height={36} circle />
              <Skeleton height={20} width={56} radius="sm" />
            </Group>
            <Skeleton height={14} width="65%" radius="sm" mb={8} />
            <Skeleton height={10} width="90%" radius="sm" mb={6} />
            <Skeleton height={10} width="45%" radius="sm" />
          </Card>
        ))}
      </SimpleGrid>
    </Busy>
  );
}

/** Placeholder for a row of headline numbers. */
export function StatsLoadingState({ count = 4 }: { count?: number }) {
  return (
    <Busy label="Loading statistics">
      <SimpleGrid cols={{ base: 1, sm: 2, md: count }} spacing="lg">
        {Array.from({ length: count }, (_, i) => (
          <Card key={i} padding="lg">
            <Group justify="space-between" align="flex-start">
              <Stack gap={8}>
                <Skeleton height={9} width={84} radius="sm" />
                <Skeleton height={26} width={60} radius="sm" />
              </Stack>
              <Skeleton height={44} width={44} radius="md" />
            </Group>
          </Card>
        ))}
      </SimpleGrid>
    </Busy>
  );
}

/** Placeholder for a single record's detail panel. */
export function DetailLoadingState() {
  return (
    <Busy label="Loading details">
      <Stack gap="md">
        <Skeleton height={22} width="35%" radius="sm" />
        <Skeleton height={11} width="60%" radius="sm" />
        <Skeleton height={1} mt="sm" />
        {Array.from({ length: 4 }, (_, i) => (
          <Stack key={i} gap={6}>
            <Skeleton height={9} width={110} radius="sm" />
            <Skeleton height={13} width={`${70 - i * 8}%`} radius="sm" />
          </Stack>
        ))}
      </Stack>
    </Busy>
  );
}
