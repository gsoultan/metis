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
import { useMemo, useRef, useState } from 'react';

import {
  actionConsequence,
  canApply,
  carriedNodes,
  heldTasksAffected,
  migrationRequestKey,
  movedNodes,
  planSummary,
  removedNodesSummary,
  toNodeActions,
  toNodeMapping,
} from '../domain/instanceMigration';
import type { ActionRow, MigrationRequest } from '../domain/instanceMigration';
import { draftFor, editDraft, mappingOf, proposedRows, versionPair } from '../domain/migrationDraft';
import type { DraftEdit, MigrationDraft } from '../domain/migrationDraft';
import { applyLabel, approvalNeeded } from '../domain/migrationApproval';
import { afterApply, answeredApply, failureToast, formatExpiry, migrationOutcome, outcomeOnScreen } from '../domain/migrationOutcome';
import type { MigrationNotice } from '../domain/migrationOutcome';
import { compareLoaded, comparisonFailure, diffOf, diffSummary, landingChoices, proposeMapping, removedNodes } from '../domain/versionDiff';
import { useDefinition, useMigrateInstances } from '../hooks/useDefinitions';
import { useMigrationPlan } from '../hooks/useMigrationPlan';
import { useTranslation } from '../i18n/context';
import { MigrationApplyFooter } from './MigrationApplyFooter';
import { VersionChangesTable } from './VersionChangesTable';
import { errorMessage } from '../services/shared/errors';
import type { NodeActionKind } from '../services/types';

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
  // Why the last apply did not finish, kept apart from the plan's own refusal:
  // the plan is worked out again afterwards, and must not wipe the reason.
  const [applyError, setApplyError] = useState<string | null>(null);
  const { t, locale } = useTranslation();
  const expiry = (iso: string) => formatExpiry(iso, locale);
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
  const comparison = useMemo(
    () => compareLoaded(
      {
        label: `v${source?.version ?? ''}`,
        loading: before.isLoading,
        failed: before.isError || !!before.data?.err,
        definition: before.data?.definition,
      },
      {
        label: `v${target?.version ?? ''}`,
        loading: after.isLoading,
        failed: after.isError || !!after.data?.err,
        definition: after.data?.definition,
      },
    ),
    [source?.version, target?.version, before.isLoading, before.isError, before.data, after.isLoading, after.isError, after.data],
  );
  // Nothing is proposed from a comparison that could not be made: a mapping
  // worked out against nothing would move work somewhere it should not go.
  const diff = useMemo(() => diffOf(comparison), [comparison]);
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
  // What pressing "Move" would send, and the plan that answers it — kept
  // together, so a plan for an earlier edit can be shown but never applied.
  const request: MigrationRequest | null = source && target
    ? {
      source: source.id,
      target: target.id,
      mapping: toNodeMapping(rows),
      acknowledge: accepted,
      actions: toNodeActions(actionRows),
    }
    : null;
  const planned = useMigrationPlan(request);
  const plan = planned.plan;
  const apply = useMigrateInstances();
  // Set in the press itself, before anything is sent. `apply.isPending`
  // disables the button a render later, and a double click or a held Enter
  // lands in between: the move was sent twice, and every instance the second
  // run re-read as still running got a second "migrated" entry on its trail.
  const applying = useRef(false);
  // Whether the dialog was closed since the last press. An answer that
  // arrives after that has no dialog to be shown in — and the dialog it finds
  // open may be another: opened again since, perhaps for another version.
  const abandoned = useRef(false);

  const open = source !== null && target !== null;
  const ready = canApply({ plan, fresh: planned.fresh, error: planned.error, applying: apply.isPending });
  // What the last apply answered, when the answer is one to read rather than
  // a toast: a request sent to a second administrator, instances the run did
  // not move, an answer nobody could read. Read from the apply itself, and
  // said only for as long as it is true of what is on screen (outcomeOnScreen).
  const requestKey = request ? migrationRequestKey(request) : null;
  const answered = answeredApply(apply.variables, apply.data, target?.version ?? 0);
  const outcome = outcomeOnScreen(answered, pair, requestKey, t, expiry);

  const say = (notice: MigrationNotice) =>
    notifications.show({
      title: notice.title,
      message: notice.message,
      color: notice.color,
      ...(notice.stays ? { autoClose: false as const } : {}),
    });

  // Closing is abandoning the plan: the next opening, of this pair or another,
  // starts from nothing.
  const close = () => {
    // A request that waits is said here and nowhere else, and the press that
    // sent it can be what closes the dialog: a second click, a key still
    // held. So the confirmation leaves with the dialog, as a toast — however
    // it is closed: this is the "×", Escape and a click outside as well.
    if (outcome?.waits) say(outcome.toast);
    abandoned.current = true;
    setWritten(null);
    setApplyError(null);
    // And forgets the last apply's answer, which is otherwise still the
    // mutation's when the dialog opens again for the same two versions. Not
    // while an apply is on its way: that one is forgotten when it answers.
    if (!applying.current) apply.reset();
    planned.reset();
    onClose();
  };

  // An apply that was refused, or did not finish. In the dialog, with a plan
  // of what is left — unless the dialog has closed since, when an alert left
  // for its next opening would be about a press nobody remembers.
  const failed = (message: string) => {
    if (abandoned.current) {
      say(failureToast(message, t));
      return;
    }
    setApplyError(message);
    // Whatever it moved has left the source version, so the plan in hand
    // counts instances that are no longer there. Running the same move again
    // carries on from where it stopped, and that needs a plan of what is left.
    planned.replan();
  };

  const handleApply = async () => {
    const pressable = canApply({ plan, fresh: planned.fresh, error: planned.error, applying: applying.current });
    if (!request || !target || !pressable) return;
    applying.current = true;
    abandoned.current = false;
    setApplyError(null);
    try {
      const reply = await apply.mutateAsync(request);
      if (reply.err) {
        failed(errorMessage(reply.err));
        return;
      }
      // Said from the server's reply, not from the preview on screen: see
      // migrationOutcome. `applied: false` is not "nothing was moved" when the
      // reply carries a request that waits for a second administrator.
      const said = migrationOutcome(reply, target.version, t, expiry);
      // An answer that is kept is shown at the foot of the dialog, from the
      // apply's own data — if the dialog is still open, and still shows what
      // the answer is to. That is asked of the screen as it is now, with the
      // screen's own test: the form may have been edited since the press.
      const stillOpen = !abandoned.current;
      const now = planned.onScreen();
      const shown = stillOpen
        && outcomeOnScreen(answeredApply(request, reply, target.version), now.pair, now.requestKey, t, expiry) !== null;
      const next = afterApply(said, shown, stillOpen);
      if (next.toast) say(next.toast);
      if (next.replan) planned.replan();
      if (next.close) close();
    } catch (error: unknown) {
      // Not "could not be moved": the server moves instances one at a time,
      // and one that stops part-way has moved some. Its message says how many.
      failed(errorMessage(error, 'The server did not confirm the move.'));
    } finally {
      applying.current = false;
      if (abandoned.current) apply.reset();
    }
  };

  const moved = plan ? movedNodes(plan) : [];
  const carried = plan ? carriedNodes(plan) : [];
  const removedSummary = plan ? removedNodesSummary(plan) : null;
  const held = plan ? heldTasksAffected(plan) : 0;

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

        {!planned.fresh && !plan && (
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
        {comparison.kind === 'loading' && (
          <Group gap="xs">
            <Loader size="xs" />
            <Text size="sm" c="dimmed">Comparing the two versions…</Text>
          </Group>
        )}
        {comparison.kind === 'failed' && (
          <Alert color="red" icon={<AlertTriangle size={16} />} radius="md">
            <Text size="sm">{comparisonFailure(comparison.missing)}</Text>
          </Alert>
        )}
        {comparison.kind === 'ready' && (diff.changes.length > 0 || diff.flows.length > 0) && (
          <Stack gap={4}>
            <Group gap="xs" justify="space-between">
              <Text size="sm" fw={600}>What changed</Text>
              <Text size="xs" c="dimmed">{diffSummary(diff)}</Text>
            </Group>
            <VersionChangesTable diff={diff} />
          </Stack>
        )}

        {applyError && (
          <Alert color="red" icon={<AlertTriangle size={16} />} radius="md">
            <Text size="sm">{applyError}</Text>
          </Alert>
        )}

        {planned.error && (
          <Alert color="red" icon={<AlertTriangle size={16} />} radius="md">
            <Text size="sm">{planned.error}</Text>
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

        <MigrationApplyFooter
          outcome={outcome}
          needed={approvalNeeded(plan, t)}
          label={applyLabel(plan, t)}
          ready={ready}
          applying={apply.isPending}
          planning={!planned.fresh && plan !== null}
          onApply={handleApply}
          onClose={close}
        />
      </Stack>
    </Modal>
  );
}
