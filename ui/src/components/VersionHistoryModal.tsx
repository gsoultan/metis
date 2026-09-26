import {
  Alert,
  Badge,
  Button,
  Group,
  Loader,
  Modal,
  Skeleton,
  Stack,
  Table,
  Text,
  Tooltip,
} from '@mantine/core';
import { notifications } from '@mantine/notifications';
import dayjs from 'dayjs';
import relativeTime from 'dayjs/plugin/relativeTime';
// Imported here rather than at the root: the date-picker stylesheet is only
// needed where a picker is, and importing it eagerly pulled the whole
// @mantine/dates chunk (~19 kB gzipped) onto the critical path for every
// visitor, picker or not.
import '@mantine/dates/styles.css';
import { DateTimePicker } from '@mantine/dates';
import { AlertTriangle, CalendarClock, CircleDot, Eye, History, MoveRight, Play, Trash2, Undo2, X } from 'lucide-react';
import { useMemo, useState } from 'react';

import { diffVersions, rolloutEffect } from '../domain/versionDiff';
import {
  canDelete,
  canSchedule,
  drainingVersions,
  isFutureCutover,
  isRollback,
  liveVersion,
  pendingCutovers,
  promotionFacts,
  versionState,
} from '../domain/versionRollout';
import {
  useCancelScheduledVersion,
  useDefinition,
  useDefinitionVersions,
  useDeleteDefinition,
  usePromoteDefinitionVersion,
  useScheduleDefinitionVersion,
} from '../hooks/useDefinitions';
import { useStartProcess } from '../hooks/useProcess';
import { MigrateInstancesModal } from './MigrateInstancesModal';
import { errorMessage } from '../services/shared/errors';
import type { ApiDefinitionVersion } from '../services/types';

/** Enough of a version to open it in the viewer. */
export type DefinitionRef = { id: string; key: string; name: string; version: number };

// Extended here rather than relied on from whichever page happened to load
// first: this component says "in 3 days" about a scheduled cutover, and
// dayjs.extend is global and idempotent.
dayjs.extend(relativeTime);

const formatCutover = (at: Date) => dayjs(at).format('D MMM YYYY, HH:mm');

interface VersionHistoryModalProps {
  /** The process key whose history to show, or null when closed. */
  processKey: string | null;
  onClose: () => void;
  onView: (version: DefinitionRef) => void;
}

const STATE_LABEL = {
  live: { label: 'Live', color: 'green', hint: 'New instances start on this version.' },
  draining: {
    label: 'Draining',
    color: 'orange',
    hint: 'Replaced, but still finishing the instances it started. They are never moved to another version.',
  },
  staged: {
    label: 'Staged',
    color: 'blue',
    hint: 'Deployed but not taking work. Promote it, or schedule it to take over at a time you choose.',
  },
  retired: { label: 'Retired', color: 'gray', hint: 'Was live, and nothing is running on it any more.' },
} as const;

/**
 * One process key's version history, and the control that changes which version
 * is live.
 *
 * The counts are the reason this is not just a list of dates. Replacing a
 * version does not stop it: every instance already running finishes on the graph
 * it started with, so the previous version keeps executing — with no new
 * arrivals — until its last instance is done. "Is it safe to delete v2 yet?" is
 * a question about that number, and before this it could only be answered by
 * reading the instance list and counting by eye.
 */
export function VersionHistoryModal({ processKey, onClose, onView }: VersionHistoryModalProps) {
  const { data, isLoading, error } = useDefinitionVersions(processKey);
  const promote = usePromoteDefinitionVersion();
  const schedule = useScheduleDefinitionVersion();
  const cancelScheduled = useCancelScheduledVersion();
  const startProcess = useStartProcess();
  const removeVersion = useDeleteDefinition();
  const [trying, setTrying] = useState<number | null>(null);
  const [confirming, setConfirming] = useState<ApiDefinitionVersion | null>(null);
  // The version a cutover is being arranged for, and the moment chosen for it.
  const [scheduling, setScheduling] = useState<ApiDefinitionVersion | null>(null);
  const [cutoverAt, setCutoverAt] = useState<Date | null>(null);
  // The draining version whose work is being moved onto the live one.
  const [migrating, setMigrating] = useState<ApiDefinitionVersion | null>(null);

  const versions = data?.versions ?? [];
  const live = liveVersion(versions);
  // The same version as `live`, but the row from the API — it carries the
  // definition id, which the domain type deliberately does not.
  const liveRow = versions.find((v) => v.live) ?? null;

  // Only while a confirmation is open: the two versions being compared, so the
  // dialog can say what actually changes rather than only which number goes
  // live. Nothing is fetched until somebody asks the question.
  const liveDefinition = useDefinition(confirming ? (liveRow?.id ?? null) : null);
  const targetDefinition = useDefinition(confirming?.id ?? null);
  const rolloutDiff = useMemo(
    () => diffVersions(liveDefinition.data?.definition ?? null, targetDefinition.data?.definition ?? null),
    [liveDefinition.data?.definition, targetDefinition.data?.definition],
  );
  const draining = drainingVersions(versions);
  const upcoming = pendingCutovers(versions);

  const close = () => {
    setConfirming(null);
    setScheduling(null);
    setCutoverAt(null);
    setMigrating(null);
    onClose();
  };

  const openScheduler = (version: ApiDefinitionVersion) => {
    setConfirming(null);
    setScheduling(version);
    setCutoverAt(null);
  };

  const runSchedule = async () => {
    if (!processKey || !scheduling || !cutoverAt) return;
    try {
      await schedule.mutateAsync({ key: processKey, version: scheduling.version, activateAt: cutoverAt });
      notifications.show({
        title: `v${scheduling.version} is scheduled`,
        message: `It takes over on ${dayjs(cutoverAt).format('D MMM YYYY, HH:mm')}. Nothing changes until then.`,
        color: 'blue',
      });
      setScheduling(null);
      setCutoverAt(null);
    } catch (err: unknown) {
      notifications.show({
        title: `Could not schedule v${scheduling.version}`,
        message: errorMessage(err, 'The cutover was not arranged.'),
        color: 'red',
      });
    }
  };

  // Starts one instance on a named version without promoting it. The point of
  // staging is to see a version work before it takes real traffic, and until the
  // start path could name a version the only way to exercise one was to make it
  // live for everybody first.
  const runVersion = async (version: number) => {
    if (!processKey) return;
    setTrying(version);
    try {
      await startProcess.mutateAsync({ definitionKey: processKey, version });
      notifications.show({
        title: `Started one instance on v${version}`,
        message: 'Follow it under Instances. The live version is unchanged.',
        color: 'green',
      });
    } catch (err: unknown) {
      notifications.show({
        title: `Could not start v${version}`,
        message: errorMessage(err, 'It did not start.'),
        color: 'red',
      });
    } finally {
      setTrying(null);
    }
  };

  const runDelete = async (version: ApiDefinitionVersion) => {
    try {
      await removeVersion.mutateAsync(version.id);
      notifications.show({
        title: `v${version.version} deleted`,
        message: 'Nothing had run on it.',
        color: 'gray',
      });
    } catch (err: unknown) {
      notifications.show({
        title: `Could not delete v${version.version}`,
        message: errorMessage(err, 'It was not removed.'),
        color: 'red',
      });
    }
  };

  const runCancel = async (releaseId: string, version: number) => {
    if (!processKey) return;
    try {
      await cancelScheduled.mutateAsync({ key: processKey, releaseId });
      notifications.show({
        title: `Cutover to v${version} cancelled`,
        message: 'The live version is unchanged.',
        color: 'gray',
      });
    } catch (err: unknown) {
      notifications.show({
        title: 'Could not cancel that cutover',
        message: errorMessage(err, 'It may have already taken effect.'),
        color: 'red',
      });
    }
  };

  const runPromote = async (version: ApiDefinitionVersion) => {
    if (!processKey) return;
    try {
      await promote.mutateAsync({ key: processKey, version: version.version });
      notifications.show({
        title: `v${version.version} is live`,
        message: 'New instances start on it. Anything already running finishes on its own version.',
        color: 'green',
      });
      setConfirming(null);
    } catch (err: unknown) {
      notifications.show({
        title: `Could not make v${version.version} live`,
        message: errorMessage(err, 'The live version was not changed.'),
        color: 'red',
      });
    }
  };

  return (
    <Modal
      opened={!!processKey}
      onClose={close}
      title={
        <Group gap="xs">
          <History size={20} color="orange" />
          <Text fw={800}>Version history: {processKey}</Text>
        </Group>
      }
      size="xl"
      radius="lg"
    >
      <Stack gap="md">
        {error && (
          <Alert color="red" icon={<AlertTriangle size={16} />} radius="md">
            <Text size="sm">{errorMessage(error, 'The version history could not be loaded.')}</Text>
          </Alert>
        )}

        {!isLoading && !error && draining.length > 0 && (
          <Alert color="orange" icon={<AlertTriangle size={16} />} radius="md">
            <Text size="sm">
              {draining.length === 1
                ? `v${draining[0].version} has been replaced but is still finishing ${draining[0].running_instances} ${draining[0].running_instances === 1 ? 'instance' : 'instances'}.`
                : `${draining.length} older versions are still finishing work.`}{' '}
              They keep running until they are done — deleting one would strand the instances on it.
            </Text>
          </Alert>
        )}

        {!isLoading && !error && upcoming.length > 0 && (
          <Alert color="blue" icon={<CalendarClock size={16} />} radius="md">
            <Stack gap={4}>
              <Text size="sm" fw={600}>Scheduled changes</Text>
              {upcoming.map((cutover) => (
                <Group key={cutover.releaseId} gap="xs" justify="space-between" wrap="nowrap">
                  <Text size="xs">
                    v{cutover.version} takes over on {dayjs(cutover.at).format('D MMM YYYY, HH:mm')} ({dayjs(cutover.at).fromNow()}).
                  </Text>
                  <Button
                    size="compact-xs"
                    variant="subtle"
                    color="gray"
                    leftSection={<X size={12} />}
                    loading={cancelScheduled.isPending}
                    onClick={() => runCancel(cutover.releaseId, cutover.version)}
                  >
                    Cancel
                  </Button>
                </Group>
              ))}
            </Stack>
          </Alert>
        )}

        {scheduling && (
          <Alert color="blue" icon={<CalendarClock size={16} />} radius="md">
            <Stack gap="xs">
              <Text size="sm" fw={600}>Schedule v{scheduling.version} to take over</Text>
              <DateTimePicker
                label="Goes live at"
                description="Your local time. Until then, the current version keeps taking new instances."
                placeholder="Pick a date and time"
                value={cutoverAt}
                onChange={(value) => setCutoverAt(value ? new Date(value) : null)}
                minDate={new Date()}
                clearable
              />
              <Text size="xs" c="dimmed">
                Instances running when it takes over are not affected — each finishes on the version it
                started with.
              </Text>
              <Group gap="xs">
                <Button size="compact-sm" variant="default" onClick={() => setScheduling(null)} disabled={schedule.isPending}>
                  Cancel
                </Button>
                <Button
                  size="compact-sm"
                  color="blue"
                  loading={schedule.isPending}
                  disabled={!isFutureCutover(cutoverAt, new Date())}
                  onClick={runSchedule}
                >
                  Schedule
                </Button>
              </Group>
              {cutoverAt !== null && !isFutureCutover(cutoverAt, new Date()) && (
                <Text size="xs" c="red">
                  Pick a time in the future. To change the live version now, use "Make live".
                </Text>
              )}
            </Stack>
          </Alert>
        )}

        {confirming && (
          <Alert
            color={isRollback(versions, confirming.version) ? 'orange' : 'blue'}
            icon={<CircleDot size={16} />}
            radius="md"
          >
            <Stack gap="xs">
              <Text size="sm" fw={600}>
                {isRollback(versions, confirming.version)
                  ? `Roll back to v${confirming.version}?`
                  : `Make v${confirming.version} the live version?`}
              </Text>
              {/* What the server does, checked against it: see promotionFacts. */}
              <Stack gap={2}>
                {promotionFacts(versions, confirming.version, { now: new Date(), formatTime: formatCutover }).map((fact) => (
                  <Text key={fact} size="xs">{fact}</Text>
                ))}
              </Stack>
              {(() => {
                // What changes for instances started after this. Phrased in the
                // direction being travelled: a step the older version still has
                // comes back, it is not new, and reading that forward is how
                // somebody rolls back believing they rolled forward.
                const effects = rolloutEffect(rolloutDiff, isRollback(versions, confirming.version));
                if (liveDefinition.isLoading || targetDefinition.isLoading) {
                  return <Text size="xs" c="dimmed">Comparing with v{live?.version}…</Text>;
                }
                if (effects.length === 0) {
                  return (
                    <Text size="xs" c="dimmed">
                      The steps, their settings and the paths between them are the same as v{live?.version};
                      only the version number changes.
                    </Text>
                  );
                }
                return (
                  <Stack gap={2}>
                    <Text size="xs" fw={600}>What changes for new instances</Text>
                    {effects.map((effect) => (
                      <Text key={effect} size="xs" c="dimmed">• {effect}</Text>
                    ))}
                  </Stack>
                );
              })()}
              <Group gap="xs">
                <Button size="compact-sm" variant="default" onClick={() => setConfirming(null)} disabled={promote.isPending}>
                  Cancel
                </Button>
                <Button
                  size="compact-sm"
                  color={isRollback(versions, confirming.version) ? 'orange' : 'indigo'}
                  loading={promote.isPending}
                  onClick={() => runPromote(confirming)}
                >
                  Yes, make v{confirming.version} live
                </Button>
              </Group>
            </Stack>
          </Alert>
        )}

        <Table verticalSpacing="sm">
          <Table.Thead>
            <Table.Tr>
              <Table.Th>Version</Table.Th>
              <Table.Th>Status</Table.Th>
              <Table.Th ta="right">Still running</Table.Th>
              <Table.Th ta="right">Total run</Table.Th>
              <Table.Th>Deployed</Table.Th>
              <Table.Th ta="right">Actions</Table.Th>
            </Table.Tr>
          </Table.Thead>
          <Table.Tbody>
            {isLoading &&
              [0, 1].map((row) => (
                <Table.Tr key={row}>
                  <Table.Td colSpan={6}><Skeleton height={20} radius="sm" /></Table.Td>
                </Table.Tr>
              ))}

            {!isLoading && versions.length === 0 && !error && (
              <Table.Tr>
                <Table.Td colSpan={6}>
                  <Text size="sm" c="dimmed" ta="center">No versions deployed yet.</Text>
                </Table.Td>
              </Table.Tr>
            )}

            {versions.map((v) => {
              const state = versionState(v, versions);
              const meta = STATE_LABEL[state];
              return (
                <Table.Tr key={v.id}>
                  <Table.Td><Badge color={v.live ? 'blue' : 'gray'}>v{v.version}</Badge></Table.Td>
                  <Table.Td>
                    <Group gap={6} wrap="nowrap">
                      <Tooltip label={meta.hint} multiline w={260}>
                        <Badge color={meta.color} variant="light">{meta.label}</Badge>
                      </Tooltip>
                      {v.scheduled_for && (
                        <Tooltip label={`Takes over on ${dayjs(v.scheduled_for).format('D MMM YYYY, HH:mm')}`}>
                          <Badge color="blue" variant="outline" size="sm" leftSection={<CalendarClock size={10} />}>
                            {dayjs(v.scheduled_for).fromNow()}
                          </Badge>
                        </Tooltip>
                      )}
                    </Group>
                  </Table.Td>
                  <Table.Td ta="right">
                    <Text size="sm" fw={v.running_instances > 0 ? 700 : 400} c={v.running_instances > 0 ? undefined : 'dimmed'}>
                      {v.running_instances}
                    </Text>
                  </Table.Td>
                  <Table.Td ta="right"><Text size="sm" c="dimmed">{v.total_instances}</Text></Table.Td>
                  <Table.Td><Text size="sm">{v.created_at ? dayjs(v.created_at).format('YYYY-MM-DD HH:mm') : '—'}</Text></Table.Td>
                  <Table.Td>
                    <Group justify="flex-end" gap="xs">
                      <Button
                        size="compact-xs"
                        variant="light"
                        leftSection={<Eye size={12} />}
                        onClick={() => onView({ id: v.id, key: v.key, name: v.name, version: v.version })}
                      >
                        View
                      </Button>
                      {!v.live && (
                        <Button
                          size="compact-xs"
                          variant="light"
                          color={isRollback(versions, v.version) ? 'orange' : 'indigo'}
                          leftSection={isRollback(versions, v.version) ? <Undo2 size={12} /> : <CircleDot size={12} />}
                          onClick={() => setConfirming(v)}
                          disabled={promote.isPending}
                        >
                          {isRollback(versions, v.version) ? 'Roll back' : 'Make live'}
                        </Button>
                      )}
                      <Tooltip label={v.live ? 'Start an instance on the live version' : `Try v${v.version} without making it live`}>
                        <Button
                          size="compact-xs"
                          variant="subtle"
                          color="green"
                          leftSection={<Play size={12} />}
                          loading={trying === v.version}
                          onClick={() => runVersion(v.version)}
                        >
                          Run
                        </Button>
                      </Tooltip>
                      {!v.live && v.running_instances > 0 && live && (
                        <Tooltip label={`Move the ${v.running_instances} instances still on v${v.version} onto v${live.version}`}>
                          <Button
                            size="compact-xs"
                            variant="subtle"
                            color="orange"
                            leftSection={<MoveRight size={12} />}
                            onClick={() => setMigrating(v)}
                          >
                            Move work
                          </Button>
                        </Tooltip>
                      )}
                      {canDelete(v) && (
                        <Tooltip label="Nothing has run on this version, so it can be removed">
                          <Button
                            size="compact-xs"
                            variant="subtle"
                            color="red"
                            leftSection={<Trash2 size={12} />}
                            loading={removeVersion.isPending}
                            onClick={() => runDelete(v)}
                          >
                            Delete
                          </Button>
                        </Tooltip>
                      )}
                      {canSchedule(v) && !v.scheduled_for && (
                        <Button
                          size="compact-xs"
                          variant="subtle"
                          color="blue"
                          leftSection={<CalendarClock size={12} />}
                          onClick={() => openScheduler(v)}
                          disabled={schedule.isPending}
                        >
                          Schedule
                        </Button>
                      )}
                    </Group>
                  </Table.Td>
                </Table.Tr>
              );
            })}
          </Table.Tbody>
        </Table>

        {promote.isPending && (
          <Group gap="xs" justify="center">
            <Loader size="xs" />
            <Text size="xs" c="dimmed">Changing the live version…</Text>
          </Group>
        )}
      </Stack>

      {/* Nested rather than a sibling: it is opened from a row in this table and
          closing it should leave the history where it was. */}
      <MigrateInstancesModal
        source={migrating ? { id: migrating.id, version: migrating.version } : null}
        target={liveRow ? { id: liveRow.id, version: liveRow.version } : null}
        processKey={processKey ?? ''}
        onClose={() => setMigrating(null)}
      />
    </Modal>
  );
}
