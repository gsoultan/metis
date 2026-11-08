import { Alert, Badge, Button, Checkbox, Group, Stack, Table, Text, Tooltip } from '@mantine/core';
import { MoveRight, Users } from 'lucide-react';
import { useMemo, useState } from 'react';

import { behindRows, movable } from '../domain/approvalTakeover';
import { useApprovalsBehind, useMoveApprovals } from '../hooks/useApprovalTakeover';

interface ApprovalTakeoverPanelProps {
  processKey: string | null;
}

/**
 * Requests still on an approval line's older versions, for an administrator
 * to move to the live version one by one or all together.
 *
 * A new version never moves a running request. That is right until the
 * person a request waits for has left and the new line names someone else:
 * this is where those requests are picked out and moved. Shown only for an
 * approval line, to someone who may move them, and only when there is
 * something on an older version.
 */
export function ApprovalTakeoverPanel({ processKey }: ApprovalTakeoverPanelProps) {
  const { data: takeover } = useApprovalsBehind(processKey);
  const rows = useMemo(() => behindRows(takeover ?? undefined), [takeover]);
  const live = takeover?.live_version ?? 0;
  const move = useMoveApprovals(processKey, live);
  const [selected, setSelected] = useState<Set<string>>(new Set());

  if (!takeover || rows.length === 0) return null;

  const choosable = rows.filter((r) => !r.blocked);
  const chosen = movable(rows, selected);
  const allChosen = choosable.length > 0 && choosable.every((r) => selected.has(r.id));
  const toggle = (id: string) =>
    setSelected((current) => {
      const next = new Set(current);
      if (next.has(id)) next.delete(id);
      else next.add(id);
      return next;
    });
  const toggleAll = () => setSelected(allChosen ? new Set() : new Set(choosable.map((r) => r.id)));
  const submit = () => move.mutate(chosen, { onSuccess: () => setSelected(new Set()) });

  return (
    <Stack gap="xs" data-testid="approval-takeover">
      <Alert color="blue" variant="light" icon={<Users size={16} />} title={`${rows.length} ${rows.length === 1 ? 'request is' : 'requests are'} still on an older line`}>
        <Text size="sm">
          They keep the line they were submitted on. Move the ones that should follow v{live}: their open step
          goes to the new line's approvers. Leave the rest and they finish on their own line.
        </Text>
      </Alert>
      <Table verticalSpacing="xs">
        <Table.Thead>
          <Table.Tr>
            <Table.Th w={36}>
              <Checkbox aria-label="Choose every request that can move" checked={allChosen} onChange={toggleAll} disabled={choosable.length === 0} />
            </Table.Th>
            <Table.Th>Request</Table.Th>
            <Table.Th>On</Table.Th>
            <Table.Th>Where it is</Table.Th>
            <Table.Th>Waits for</Table.Th>
          </Table.Tr>
        </Table.Thead>
        <Table.Tbody>
          {rows.map((r) => (
            <Table.Tr key={r.id}>
              <Table.Td>
                {r.blocked ? (
                  <Tooltip label={r.blocked} multiline w={260}>
                    <Checkbox aria-label={`${r.title} cannot be moved`} checked={false} disabled onChange={() => undefined} />
                  </Tooltip>
                ) : (
                  <Checkbox aria-label={`Move ${r.title}`} checked={selected.has(r.id)} onChange={() => toggle(r.id)} />
                )}
              </Table.Td>
              <Table.Td>
                <Text size="sm" fw={500}>{r.title}</Text>
                <Text size="xs" c="dimmed">by {r.submittedBy}</Text>
              </Table.Td>
              <Table.Td><Badge color="gray" variant="light">v{r.version}</Badge></Table.Td>
              <Table.Td><Text size="sm">{r.where}</Text></Table.Td>
              <Table.Td><Text size="sm">{r.waitsFor}</Text></Table.Td>
            </Table.Tr>
          ))}
        </Table.Tbody>
      </Table>
      <Group justify="flex-end">
        <Button
          size="xs"
          color="orange"
          leftSection={<MoveRight size={14} />}
          disabled={chosen.length === 0}
          loading={move.isPending}
          onClick={submit}
        >
          {chosen.length ? `Move ${chosen.length} to v${live}` : `Move to v${live}`}
        </Button>
      </Group>
    </Stack>
  );
}
