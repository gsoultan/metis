import {
  ActionIcon,
  Alert,
  Badge,
  Button,
  Checkbox,
  Group,
  Select,
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
import { useEffect, useMemo, useState } from 'react';

import {
  actionConsequence,
  carriedNodes,
  heldTasksAffected,
  isApplicable,
  movedNodes,
  planSummary,
  removedNodesSummary,
  toNodeActions,
  toNodeMapping,
} from '../domain/instanceMigration';
import type { ActionRow } from '../domain/instanceMigration';
import { draftFor, editDraft, mappingOf, proposedRows, versionPair } from '../domain/migrationDraft';
import type { DraftEdit, MigrationDraft } from '../domain/migrationDraft';
import { migrationNotice } from '../domain/migrationOutcome';
import { diffSummary, diffVersions, landingChoices, proposeMapping, removedNodes } from '../domain/versionDiff';
import { useDefinition, useMigrateInstances, usePlanInstanceMigration } from '../hooks/useDefinitions';
import { VersionChangesTable } from './VersionChangesTable';
import { errorMessage } from '../services/shared/errors';
import type { ApiMigrationPlan, NodeActionKind } from '../services/types';

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
  const [plan, setPlan] = useState<ApiMigrationPlan | null>(null);
  const [refused, setRefused] = useState<string | null>(null);
  // What somebody has said here — the mapping, the holds they accepted, the
  // nodes they decided rather than moved — and the pair of versions they said
  // it about. Mapping and decisions stay separate instructions: the server
  // refuses a node that carries both, and merging them here would hide that.
  const [written, setWritten] = useState<MigrationDraft | null>(null);

  // Both versions, so the mapping can be picked from what actually exists
  // rather than typed. The node ids are on the definitions already; nothing new
  // is fetched that the designer does not fetch too.
  const before = useDefinition(source?.id ?? null);
  const after = useDefinition(target?.id ?? null);
  const diff = useMemo(
    () => diffVersions(before.data?.definition ?? null, after.data?.definition ?? null),
    [before.data?.definition, after.data?.definition],
  );
  const gone = removedNodes(diff);
  const landing = landingChoices(diff);
  // One step out and one in is a rename, and the only reading of it — so it is
  // proposed until somebody edits the mapping. Anything less certain is left
  // blank on purpose: a wrong guess pre-filled is the row nobody re-reads.
  const proposal = useMemo(() => proposedRows(proposeMapping(diff)), [diff]);

  // The draft for the pair on screen, derived rather than reset: a draft
  // written for another pair is never shown, or sent, for this one.
  const pair = source && target ? versionPair(source.id, target.id) : null;
  const draft = pair ? draftFor(written, pair) : null;
  const rows = draft ? mappingOf(draft, proposal) : [];
  const accepted = draft?.accepted ?? [];
  const actionRows = draft?.actions ?? [];
  const edit = (change: DraftEdit) => {
    if (!pair) return;
    setWritten((current) => editDraft(draftFor(current, pair), change, proposal));
  };
  // Closing is abandoning the plan: the next opening, of this pair or another,
  // starts from nothing.
  const close = () => {
    setWritten(null);
    onClose();
  };

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
      .mutateAsync({
        source: source.id,
        target: target.id,
        mapping: toNodeMapping(rows),
        acknowledge: accepted,
        actions: toNodeActions(actionRows),
      })
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
  }, [source?.id, target?.id, JSON.stringify(rows), JSON.stringify(accepted), JSON.stringify(actionRows)]);

  const handleApply = async () => {
    if (!source || !target) return;
    try {
      const reply = await apply.mutateAsync({
        source: source.id,
        target: target.id,
        mapping: toNodeMapping(rows),
        acknowledge: accepted,
        actions: toNodeActions(actionRows),
      });
      if (reply.err) {
        setRefused(reply.err);
        return;
      }
      // Said from the server's reply, not from the preview on screen: see
      // migrationNotice.
      const notice = migrationNotice(reply, target.version);
      notifications.show({ title: notice.title, message: notice.message, color: notice.color });
      if (notice.closes) close();
    } catch (error: unknown) {
      // Not "could not be moved": the server moves instances one at a time,
      // and one that stops part-way has moved some. Its message says how many.
      setRefused(errorMessage(error, 'The server did not confirm the move.'));
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
      onClose={close}
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

        {/*
          What actually changed between the two versions — steps, the paths
          between them, and every setting that changes what a step does.

          The mapping below asks where the work on a removed step should go, and
          answering that without seeing the diff meant reading node ids off a
          diagram in another tab.
        */}
        {(before.isLoading || after.isLoading) && (
          <Group gap="xs">
            <Loader size="xs" />
            <Text size="sm" c="dimmed">Comparing the two versions…</Text>
          </Group>
        )}
        {!before.isLoading && !after.isLoading && (diff.changes.length > 0 || diff.flows.length > 0) && (
          <Stack gap={4}>
            <Group gap="xs" justify="space-between">
              <Text size="sm" fw={600}>What changed</Text>
              <Text size="xs" c="dimmed">{diffSummary(diff)}</Text>
            </Group>
            <VersionChangesTable diff={diff} />
          </Stack>
        )}

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
                    edit({ type: 'accept', nodeId: hold.node_id, accepted: event.currentTarget.checked })
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

        {/*
          Deciding work, as opposed to moving it. A mapping can only answer
          "where does this go"; removing an approval asks whether the pending
          approval counts as given or as void, and the only way to say the first
          with a mapping alone is to point the task at somebody else's step.
        */}
        <Stack gap={6}>
          <Group gap="xs" justify="space-between">
            <Text size="sm" fw={600}>Decide instead of moving</Text>
            <Button
              size="compact-xs"
              variant="subtle"
              leftSection={<Plus size={12} />}
              onClick={() => edit({ type: 'actions', change: (current) => [...current, { from: '', kind: '', reason: '' }] })}
            >
              Add
            </Button>
          </Group>
          {actionRows.length === 0 && (
            <Text size="xs" c="dimmed">
              Skip a step to advance past it as though it had been done, or cancel to end the
              instances waiting there. Both are recorded against your name.
            </Text>
          )}
          {actionRows.map((row, index) => {
            const update = (patch: Partial<ActionRow>) =>
              edit({ type: 'actions', change: (current) => current.map((r, i) => (i === index ? { ...r, ...patch } : r)) });
            return (
              <Stack key={index} gap={4}>
                <Group gap="xs" wrap="nowrap" align="flex-start">
                  <Select
                    size="xs"
                    aria-label="Step to decide"
                    placeholder="Step"
                    data={(plan?.removed_nodes ?? []).map((node) => ({ value: node, label: node }))}
                    value={row.from === '' ? null : row.from}
                    onChange={(value) => update({ from: value ?? '' })}
                    searchable
                    style={{ flex: 1 }}
                  />
                  <Select
                    size="xs"
                    aria-label="What to do with it"
                    placeholder="Do what"
                    data={[
                      { value: 'skip', label: 'Skip it' },
                      { value: 'cancel', label: 'End the instance' },
                      { value: 'hold', label: 'Leave for a person' },
                    ]}
                    value={row.kind === '' ? null : row.kind}
                    onChange={(value) => update({ kind: (value ?? '') as NodeActionKind | '' })}
                    style={{ width: 150 }}
                  />
                  <TextInput
                    size="xs"
                    aria-label="Reason"
                    placeholder="Why — recorded on every instance"
                    value={row.reason}
                    onChange={(event) => update({ reason: event.currentTarget.value })}
                    style={{ flex: 2 }}
                  />
                  <ActionIcon
                    size="sm"
                    variant="subtle"
                    color="gray"
                    onClick={() => edit({ type: 'actions', change: (current) => current.filter((_, i) => i !== index) })}
                  >
                    <Trash2 size={14} />
                  </ActionIcon>
                </Group>
                {row.from !== '' && row.kind !== '' && (
                  <Text size="xs" c="dimmed">{actionConsequence(row.kind, row.from)}</Text>
                )}
              </Stack>
            );
          })}
        </Stack>

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
              onClick={() => {
                const id = nextRowId++;
                edit({ type: 'mapping', change: (current) => [...current, { id, from: '', to: '' }] });
              }}
            >
              Add
            </Button>
          </Group>
          <Text size="xs" c="dimmed">
            Only needed where a step changed id. Anything you do not list is carried across
            unchanged.
          </Text>
          {rows.map((row, index) => (
            <Group key={row.id} gap="xs" wrap="nowrap">
              <Select
                size="xs"
                placeholder={`step in v${source?.version}`}
                aria-label={`Node in the old version, row ${index + 1}`}
                data={gone.map((change) => ({
                  value: change.id,
                  label: change.before?.name ? `${change.before.name} (${change.id})` : change.id,
                }))}
                value={row.from === '' ? null : row.from}
                onChange={(value) => {
                  const next = value ?? '';
                  edit({ type: 'mapping', change: (current) => current.map((r, i) => (i === index ? { ...r, from: next } : r)) });
                }}
                searchable
                style={{ flex: 1 }}
              />
              <ArrowRight size={14} />
              <Select
                size="xs"
                placeholder={`step in v${target?.version}`}
                aria-label={`Node in the new version, row ${index + 1}`}
                data={landing.map((node) => ({
                  value: node.id,
                  label: node.name ? `${node.name} (${node.id})` : node.id,
                }))}
                value={row.to === '' ? null : row.to}
                onChange={(value) => {
                  const next = value ?? '';
                  edit({ type: 'mapping', change: (current) => current.map((r, i) => (i === index ? { ...r, to: next } : r)) });
                }}
                searchable
                style={{ flex: 1 }}
              />
              <Tooltip label="Remove this mapping">
                <ActionIcon
                  size="sm"
                  variant="subtle"
                  color="red"
                  aria-label={`Remove mapping row ${index + 1}`}
                  onClick={() => edit({ type: 'mapping', change: (current) => current.filter((_, i) => i !== index) })}
                >
                  <Trash2 size={14} />
                </ActionIcon>
              </Tooltip>
            </Group>
          ))}
        </Stack>

        <Group justify="flex-end">
          <Button variant="subtle" color="gray" onClick={close}>Cancel</Button>
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
