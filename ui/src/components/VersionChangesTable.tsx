import { Badge, Table, Text } from '@mantine/core';

import type { VersionDiff } from '../domain/versionDiff';

const KIND_COLOR = { removed: 'red', added: 'green', changed: 'yellow', unchanged: 'gray' } as const;

/**
 * What actually changed between two versions: steps first, then the paths
 * between them.
 *
 * Removed steps lead because those are the ones holding work; changed ones
 * next, because a step whose approver moved alters what a migration means even
 * though nothing has to be mapped for it. Paths come after the steps they join,
 * and each is named by those steps rather than by its id.
 */
export function VersionChangesTable({ diff }: { diff: VersionDiff }) {
  return (
    <Table verticalSpacing="xs" horizontalSpacing="sm">
      <Table.Tbody>
        {diff.changes
          .filter((change) => change.kind !== 'unchanged')
          .map((change) => (
            <Table.Tr key={`step:${change.id}`}>
              <Table.Td width={110}>
                <Badge size="sm" variant="light" color={KIND_COLOR[change.kind]}>
                  {change.kind}
                </Badge>
              </Table.Td>
              <Table.Td>
                <Text size="xs" ff="monospace">{change.id}</Text>
                {(change.before?.name ?? change.after?.name) && (
                  <Text size="xs" c="dimmed">{change.before?.name ?? change.after?.name}</Text>
                )}
              </Table.Td>
              <Table.Td>
                {change.differences.map((difference) => (
                  <Text key={difference} size="xs" c="dimmed">{difference}</Text>
                ))}
              </Table.Td>
            </Table.Tr>
          ))}
        {diff.flows.map((change) => (
          <Table.Tr key={`path:${change.id}`}>
            <Table.Td width={110}>
              <Badge size="sm" variant="light" color={KIND_COLOR[change.kind]}>
                {change.kind}
              </Badge>
            </Table.Td>
            <Table.Td>
              <Text size="xs">Path {change.route}</Text>
            </Table.Td>
            <Table.Td>
              {change.differences.map((difference) => (
                <Text key={difference} size="xs" c="dimmed">{difference}</Text>
              ))}
            </Table.Td>
          </Table.Tr>
        ))}
      </Table.Tbody>
    </Table>
  );
}
