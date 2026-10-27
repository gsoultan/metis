import { Alert, Badge, Button, Card, Group, Stack, Table, Text } from '@mantine/core';
import { ArrowRightLeft } from 'lucide-react';
import { useState } from 'react';

import { fillable, fillableNames, fillsFrom, usedBy } from '../domain/directoryGaps';
import { useDirectoryGaps, useFillDirectoryGaps } from '../hooks/useParticipants';
import { errorMessage } from '../services/shared/errors';

/**
 * What this project's People lack before Users stop doing tasks: each name an
 * open task or a live model uses that is not a person or a People group here.
 *
 * Read when asked for, since it reads every open task and live model.
 */
export function PeopleSwitchOver() {
  const [asked, setAsked] = useState(false);
  const { data: gaps = [], isFetching, error } = useDirectoryGaps(asked);
  const fill = useFillDirectoryGaps();
  const toFill = fillableNames(gaps);
  const count = toFill.people.length + toFill.groups.length;

  return (
    <Card withBorder radius="lg" p="md">
      <Stack gap="sm">
        <Group justify="space-between" wrap="nowrap">
          <Stack gap={2}>
            <Group gap="xs">
              <ArrowRightLeft size={16} />
              <Text fw={600} size="sm">Moving tasks from Users to People</Text>
            </Group>
            <Text size="sm" c="dimmed">
              Names this project's tasks and models use that are not in its People. Until they are,
              those tasks are done by Users, or by nobody once Users stop doing tasks.
            </Text>
          </Stack>
          {!asked && (
            <Button variant="light" size="xs" onClick={() => setAsked(true)}>
              Check
            </Button>
          )}
        </Group>
        {asked && error && <Alert color="red">{errorMessage(error, 'The check could not be run.')}</Alert>}
        {asked && !error && !isFetching && gaps.length === 0 && (
          <Text size="sm" c="teal">Every name this project's work uses is in its People.</Text>
        )}
        {gaps.length > 0 && (
          <>
            <Table.ScrollContainer minWidth={560}>
              <Table verticalSpacing="xs">
                <Table.Thead>
                  <Table.Tr>
                    <Table.Th>Name</Table.Th>
                    <Table.Th>Used by</Table.Th>
                    <Table.Th>Added from</Table.Th>
                  </Table.Tr>
                </Table.Thead>
                <Table.Tbody>
                  {gaps.map((gap) => (
                    <Table.Tr key={`${gap.kind}:${gap.name}`}>
                      <Table.Td>
                        <Group gap="xs" wrap="nowrap">
                          <Text size="sm" ff="monospace">{gap.name}</Text>
                          <Badge size="xs" variant="light" color={gap.kind === 'person' ? 'indigo' : 'blue'}>
                            {gap.kind === 'person' ? 'person' : 'group'}
                          </Badge>
                        </Group>
                      </Table.Td>
                      <Table.Td><Text size="sm" c="dimmed">{usedBy(gap)}</Text></Table.Td>
                      <Table.Td>
                        <Text size="sm" c={fillable(gap) ? undefined : 'orange'}>{fillsFrom(gap)}</Text>
                      </Table.Td>
                    </Table.Tr>
                  ))}
                </Table.Tbody>
              </Table>
            </Table.ScrollContainer>
            <Group justify="space-between">
              <Text size="xs" c="dimmed">
                People added this way have no password yet. Set one with the key button, or send them their task links.
              </Text>
              <Button
                size="xs"
                disabled={count === 0}
                loading={fill.isPending}
                onClick={() => fill.mutate(toFill)}
              >
                {count === 0 ? 'Nothing to add from the organization' : `Add ${count} to People`}
              </Button>
            </Group>
          </>
        )}
      </Stack>
    </Card>
  );
}
