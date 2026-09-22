import {
  ActionIcon,
  Alert,
  Badge,
  Button,
  Checkbox,
  Group,
  Loader,
  Modal,
  Stack,
  Table,
  Text,
  TextInput,
  Tooltip,
} from '@mantine/core';
import { notifications } from '@mantine/notifications';
import { AlertTriangle, ArrowRight, Plus, ShieldAlert, Trash2 } from 'lucide-react';
import { useEffect, useState } from 'react';

import {
  carriedNodes,
  heldTasksAffected,
  isApplicable,
  movedNodes,
  planSummary,
  removedNodesSummary,
  toNodeMapping,
} from '../domain/instanceMigration';
import { useMigrateInstances, usePlanInstanceMigration } from '../hooks/useDefinitions';
import { errorMessage } from '../services/shared/errors';
import type { ApiMigrationPlan } from '../services/types';

/** A version, as this dialog needs to name it. */
export interface MigrationVersionRef {
  id: string;
  version: number;
}

interface MigrateInstancesModalProps {
  /** The version the work is on now, or null when the dialog is closed. */
  source: MigrationVersionRef | null;
  /** The version to move it to. */
  target: MigrationVersionRef | null;
  processKey: string;
  onClose: () => void;
}

interface MappingRow {
  /** Stable across edits, so a row keeps its focus when another is removed. */
  id: number;
  from: string;
  to: string;
}

/** Row ids are only unique within one open dialog, which is all they are for. */
let nextRowId = 0;

/**
 * Moves running instances from one version onto another.
 *
 * This is the escape hatch, not the normal path. Promoting a version and
 * letting the old one drain is what you do; this is for a version that must not
 * continue — a security fix, a calculation that was wrong. It rewrites work that
 * is already somebody's, so it previews by default: the plan is what you get
 * until you press the button that says apply.
 */
export function MigrateInstancesModal({ source, target, processKey, onClose }: MigrateInstancesModalProps) {
  const [rows, setRows] = useState<MappingRow[]>([]);
  const [plan, setPlan] = useState<ApiMigrationPlan | null>(null);
  const [refused, setRefused] = useState<string | null>(null);
  // Accepted holds, by node id. Cleared whenever the mapping changes: an
  // acknowledgement is of a specific plan, and carrying it across an edit is
  // how somebody accepts something they never read.
  const [accepted, setAccepted] = useState<string[]>([]);

  const preview = usePlanInstanceMigration();
  const apply = useMigrateInstances();

  const open = source !== null && target !== null;

  // The plan is fetched on open and re-fetched whenever the mapping changes,
  // because a mapping that strands a task must stop saying "ready to apply" the
  // moment it does.
  useEffect(() => {
    if (!source || !target) {
      setPlan(null);
      setRefused(null);
      return;
    }
    let cancelled = false;
    preview
      .mutateAsync({ source: source.id, target: target.id, mapping: toNodeMapping(rows), acknowledge: accepted })
      .then((result) => {
        if (cancelled) return;
        setPlan(result.plan ?? null);
        setRefused(result.err ?? null);
      })
      .catch((error: unknown) => {
        if (cancelled) return;
        setPlan(null);
        setRefused(errorMessage(error, 'The plan could not be worked out.'));
      });
    return () => {
      cancelled = true;
    };
    // preview is a stable mutation object; including it would refetch on every
    // render.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [source?.id, target?.id, JSON.stringify(rows), JSON.stringify(accepted)]);

  // A mapping edit invalidates every acknowledgement made against the old one.
  useEffect(() => {
    setAccepted([]);
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [JSON.stringify(rows.map((row) => `${row.from}->${row.to}`))]);

  const handleApply = async () => {
    if (!source || !target) return;
    try {
      const result = await apply.mutateAsync({
        source: source.id,
        target: target.id,
        mapping: toNodeMapping(rows),
        acknowledge: accepted,
      });
      if (result.err) {
        setRefused(result.err);
        return;
      }
      notifications.show({
        title: 'Moved',
        message: `${plan?.instances ?? 0} instances now run on v${target.version}.`,
        color: 'green',
      });
      onClose();
    } catch (error: unknown) {
      setRefused(errorMessage(error, 'The instances could not be moved.'));
    }
  };

  const moved = plan ? movedNodes(plan) : [];
  const carried = plan ? carriedNodes(plan) : [];
  const removedSummary = plan ? removedNodesSummary(plan) : null;
  const held = plan ? heldTasksAffected(plan) : 0;
  const ready = isApplicable(plan) && (plan?.instances ?? 0) > 0 && refused === null;

  return (
    <Modal
      opened={open}
      onClose={onClose}
      size="lg"
      radius="md"
      title={
        <Text fw={700}>
          Move running work to v{target?.version} — {processKey}
        </Text>
      }
    >
      <Stack gap="md">
        <Alert color="orange" icon={<AlertTriangle size={16} />} radius="md">
          <Text size="sm">
            This rewrites instances that have already started. The usual way to change version is to
            promote the new one and let v{source?.version} finish what it has — use this only when
            v{source?.version} must not continue.
          </Text>
        </Alert>

        {preview.isPending && !plan && (
          <Group gap="xs">
            <Loader size="xs" />
            <Text size="sm" c="dimmed">Working out what would move…</Text>
          </Group>
        )}

        {plan && <Text size="sm">{planSummary(plan)}</Text>}

        {refused && (
          <Alert color="red" icon={<AlertTriangle size={16} />} radius="md">
            <Text size="sm">{refused}</Text>
          </Alert>
        )}

        {plan?.refusals?.map((refusal) => (
          <Alert key={refusal} color="red" icon={<AlertTriangle size={16} />} radius="md">
            <Text size="sm">{refusal}</Text>
          </Alert>
        ))}

        {/*
          Yellow, not red, and never merged with the refusals above. These do
          not block the apply — they are the things that apply cleanly and then
          produce incidents, which is exactly the class somebody stops reading
          if it is dressed up as an error.
        */}
        {removedSummary && (
          <Alert color="yellow" icon={<AlertTriangle size={16} />} radius="md">
            <Text size="sm">{removedSummary}</Text>
          </Alert>
        )}

        {plan?.warnings?.map((warning) => (
          <Alert key={warning} color="yellow" icon={<AlertTriangle size={16} />} radius="md">
            <Text size="sm">{warning}</Text>
          </Alert>
        ))}

        {/*
          Holds are refusals somebody may accept, one at a time and by name.
          A single "I understand" for the whole list would be a button people
          learn to press without reading, which is the opposite of what an
          acknowledgement is for.
        */}
        {(plan?.compliance_holds?.length ?? 0) > 0 && (
          <Alert color="red" icon={<ShieldAlert size={16} />} radius="md">
            <Stack gap={6}>
              <Text size="sm" fw={600}>Steps that carry a control obligation</Text>
              {plan?.compliance_holds?.map((hold) => (
                <Checkbox
                  key={hold.node_id}
                  checked={accepted.includes(hold.node_id)}
                  onChange={(event) =>
                    setAccepted((current) =>
                      event.currentTarget.checked
                        ? [...current, hold.node_id]
                        : current.filter((id) => id !== hold.node_id),
                    )
                  }
                  label={
                    <Text size="xs">
                      {hold.instances === 1 ? '1 instance has' : `${hold.instances} instances have`} not
                      passed <Text span ff="monospace" size="xs">{hold.name || hold.node_id}</Text> yet.
                      {hold.note ? ` ${hold.note}.` : ''} Accept that they never will.
                    </Text>
                  }
                />
              ))}
              <Text size="xs" c="dimmed">
                Your name is recorded against each one on every instance's timeline.
              </Text>
            </Stack>
          </Alert>
        )}

        {moved.length > 0 && (
          <Stack gap={4}>
            <Text size="sm" fw={600}>Work that moves</Text>
            <Table verticalSpacing="xs" horizontalSpacing="sm">
              <Table.Thead>
                <Table.Tr>
                  <Table.Th>From</Table.Th>
                  <Table.Th>To</Table.Th>
                  <Table.Th ta="right">Tasks</Table.Th>
                  <Table.Th ta="right">Timers &amp; calls</Table.Th>
                </Table.Tr>
              </Table.Thead>
              <Table.Tbody>
                {moved.map((move) => (
                  <Table.Tr key={move.from}>
                    <Table.Td><Text size="xs" ff="monospace">{move.from}</Text></Table.Td>
                    <Table.Td>
                      <Group gap={4} wrap="nowrap">
                        <ArrowRight size={12} />
                        <Text size="xs" ff="monospace">{move.to}</Text>
                      </Group>
                    </Table.Td>
                    <Table.Td ta="right">
                      <Text size="xs">
                        {move.tasks}
                        {(move.tasks_claimed ?? 0) + (move.tasks_delegated ?? 0) > 0 && (
                          <Text span size="xs" c="orange">
                            {' '}({(move.tasks_claimed ?? 0) + (move.tasks_delegated ?? 0)} held)
                          </Text>
                        )}
                      </Text>
                    </Table.Td>
                    <Table.Td ta="right"><Text size="xs">{move.jobs}</Text></Table.Td>
                  </Table.Tr>
                ))}
              </Table.Tbody>
            </Table>
            {held > 0 && (
              <Text size="xs" c="dimmed">
                {held === 1 ? '1 task is' : `${held} tasks are`} open in somebody's hands right now.
                Moving re-derives who may do the work from the node it lands on, so
                {held === 1 ? ' it goes' : ' they go'} back to the queue.
              </Text>
            )}
          </Stack>
        )}

        {carried.length > 0 && (
          <Group gap={4}>
            <Text size="xs" c="dimmed">Unchanged, because v{target?.version} still has them:</Text>
            {carried.map((move) => (
              <Badge key={move.from} size="xs" variant="light" color="gray">{move.from}</Badge>
            ))}
          </Group>
        )}

        <Stack gap={4}>
          <Group justify="space-between">
            <Text size="sm" fw={600}>Node mapping</Text>
            <Button
              size="compact-xs"
              variant="subtle"
              leftSection={<Plus size={12} />}
              onClick={() => setRows((current) => [...current, { id: nextRowId++, from: '', to: '' }])}
            >
              Add
            </Button>
          </Group>
          <Text size="xs" c="dimmed">
            Only needed where a node changed id. Anything you do not list is carried across
            unchanged.
          </Text>
          {rows.map((row, index) => (
            <Group key={row.id} gap="xs" wrap="nowrap">
              <TextInput
                size="xs"
                placeholder="node in v{source?.version}"
                aria-label={`Node in the old version, row ${index + 1}`}
                value={row.from}
                onChange={(event) => {
                  const value = event.currentTarget.value;
                  setRows((current) => current.map((r, i) => (i === index ? { ...r, from: value } : r)));
                }}
                style={{ flex: 1 }}
              />
              <ArrowRight size={14} />
              <TextInput
                size="xs"
                placeholder="node in the new version"
                aria-label={`Node in the new version, row ${index + 1}`}
                value={row.to}
                onChange={(event) => {
                  const value = event.currentTarget.value;
                  setRows((current) => current.map((r, i) => (i === index ? { ...r, to: value } : r)));
                }}
                style={{ flex: 1 }}
              />
              <Tooltip label="Remove this mapping">
                <ActionIcon
                  size="sm"
                  variant="subtle"
                  color="red"
                  aria-label={`Remove mapping row ${index + 1}`}
                  onClick={() => setRows((current) => current.filter((_, i) => i !== index))}
                >
                  <Trash2 size={14} />
                </ActionIcon>
              </Tooltip>
            </Group>
          ))}
        </Stack>

        <Group justify="flex-end">
          <Button variant="subtle" color="gray" onClick={onClose}>Cancel</Button>
          <Button
            color="orange"
            disabled={!ready}
            loading={apply.isPending}
            onClick={handleApply}
          >
            Move {plan?.instances ?? 0} {plan?.instances === 1 ? 'instance' : 'instances'}
          </Button>
        </Group>
      </Stack>
    </Modal>
  );
}
