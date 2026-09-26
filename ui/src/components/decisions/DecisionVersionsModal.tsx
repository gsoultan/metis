/**
 * One decision's versions, and the control that changes which one is live.
 *
 * Every save of a decision is a new version and none is ever changed, so the
 * way back from a bad edit is to put an earlier version back into force — not
 * to type the old policy in again. This is where that happens, and where a
 * version nobody needs any more is deleted, one at a time.
 *
 * The same shape as a process's version history, without what a decision does
 * not have: instances do not pin a decision version when they start, they read
 * the live one when a step reaches it, so there is no work draining on an old
 * version and nothing to schedule.
 */
import { Alert, Badge, Button, Group, Modal, Skeleton, Stack, Table, Text, Tooltip } from '@mantine/core';
import { notifications } from '@mantine/notifications';
import dayjs from 'dayjs';
import { AlertTriangle, CircleDot, Eye, History, Trash2, Undo2 } from 'lucide-react';
import { useState } from 'react';

import {
  DECISION_VERSION_STATES,
  canDeleteDecisionVersion,
  decisionPromotionFacts,
  decisionVersionState,
  isDecisionRollback,
  liveDecisionVersion,
} from '../../domain/decisionVersions';
import { useDecisionVersions, usePromoteDecisionVersion } from '../../hooks/useDecisions';
import { errorMessage } from '../../services/shared/errors';
import type { ApiDecisionVersion } from '../../services/types';
import { DeleteDecisionModal } from './DeleteDecisionModal';

export interface DecisionVersionsModalProps {
  /** The decision key whose versions to show, or null when closed. */
  decisionKey: string | null;
  /** What to call the decision; the key when absent. */
  name?: string;
  /** The version open in the editor, so its row can say so. */
  openId?: string;
  onClose: () => void;
  /** Opens one version in the editor. */
  onOpen: (id: string) => void;
  /** After a version is deleted, with how many are left. */
  onDeleted?: (deleted: ApiDecisionVersion, remaining: number) => void;
}

export function DecisionVersionsModal({ decisionKey, name, openId, onClose, onOpen, onDeleted }: DecisionVersionsModalProps) {
  const { data, isLoading, error } = useDecisionVersions(decisionKey);
  const promote = usePromoteDecisionVersion();
  const [confirming, setConfirming] = useState<ApiDecisionVersion | null>(null);
  const [deleting, setDeleting] = useState<ApiDecisionVersion | null>(null);
  const versions = data ?? [];
  const live = liveDecisionVersion(versions);

  const close = () => {
    setConfirming(null);
    setDeleting(null);
    onClose();
  };

  const runPromote = async (version: ApiDecisionVersion) => {
    if (!decisionKey) return;
    try {
      await promote.mutateAsync({ key: decisionKey, version: version.version });
      notifications.show({
        title: `v${version.version} is live`,
        message: 'Steps that name no version use it from now on.',
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
      opened={decisionKey !== null}
      onClose={close}
      title={
        <Group gap="xs">
          <History size={20} />
          <Text fw={800}>Versions of {name || decisionKey}</Text>
        </Group>
      }
      size="lg"
      radius="lg"
    >
      <Stack gap="md">
        {error && (
          <Alert color="red" icon={<AlertTriangle size={16} />} radius="md">
            <Text size="sm">{errorMessage(error, 'The versions could not be loaded.')}</Text>
          </Alert>
        )}

        {confirming && (
          <PromoteConfirmation
            target={confirming}
            versions={versions}
            pending={promote.isPending}
            onCancel={() => setConfirming(null)}
            onConfirm={() => runPromote(confirming)}
          />
        )}

        <Table verticalSpacing="sm">
          <Table.Thead>
            <Table.Tr>
              <Table.Th>Version</Table.Th>
              <Table.Th>Status</Table.Th>
              <Table.Th>Saved</Table.Th>
              <Table.Th ta="right">Actions</Table.Th>
            </Table.Tr>
          </Table.Thead>
          <Table.Tbody>
            {isLoading &&
              [0, 1].map((row) => (
                <Table.Tr key={row}>
                  <Table.Td colSpan={4}>
                    <Skeleton height={20} radius="sm" />
                  </Table.Td>
                </Table.Tr>
              ))}
            {!isLoading && !error && versions.length === 0 && (
              <Table.Tr>
                <Table.Td colSpan={4}>
                  <Text size="sm" c="dimmed" ta="center">
                    No versions saved yet.
                  </Text>
                </Table.Td>
              </Table.Tr>
            )}
            {versions.map((version) => (
              <VersionRow
                key={version.id}
                version={version}
                versions={versions}
                isOpen={version.id === openId}
                promoting={promote.isPending}
                onOpen={() => onOpen(version.id)}
                onPromote={() => setConfirming(version)}
                onDelete={() => setDeleting(version)}
              />
            ))}
          </Table.Tbody>
        </Table>
      </Stack>

      <DeleteDecisionModal
        decision={deleting}
        liveVersion={live?.version ?? null}
        onlyVersion={versions.length === 1}
        onClose={() => setDeleting(null)}
        onDeleted={() => {
          const deleted = deleting;
          setDeleting(null);
          if (deleted) onDeleted?.(deleted, versions.length - 1);
        }}
      />
    </Modal>
  );
}

function VersionRow({
  version,
  versions,
  isOpen,
  promoting,
  onOpen,
  onPromote,
  onDelete,
}: {
  version: ApiDecisionVersion;
  versions: ApiDecisionVersion[];
  isOpen: boolean;
  promoting: boolean;
  onOpen: () => void;
  onPromote: () => void;
  onDelete: () => void;
}) {
  const meta = DECISION_VERSION_STATES[decisionVersionState(version, versions)];
  const rollback = isDecisionRollback(versions, version.version);
  return (
    <Table.Tr>
      <Table.Td>
        <Badge color={version.live ? 'green' : 'gray'} variant={version.live ? 'filled' : 'light'}>
          v{version.version}
        </Badge>
      </Table.Td>
      <Table.Td>
        <Tooltip label={meta.hint} multiline w={260}>
          <Badge color={meta.color} variant="light">
            {meta.label}
          </Badge>
        </Tooltip>
      </Table.Td>
      <Table.Td>
        <Text size="sm">{version.created_at ? dayjs(version.created_at).format('YYYY-MM-DD HH:mm') : '—'}</Text>
      </Table.Td>
      <Table.Td>
        <Group justify="flex-end" gap="xs" wrap="nowrap">
          {isOpen ? (
            <Text size="xs" c="dimmed">
              Open now
            </Text>
          ) : (
            <Button size="compact-xs" variant="light" leftSection={<Eye size={12} />} onClick={onOpen}>
              Open
            </Button>
          )}
          {!version.live && (
            <Button
              size="compact-xs"
              variant="light"
              color={rollback ? 'orange' : 'indigo'}
              leftSection={rollback ? <Undo2 size={12} /> : <CircleDot size={12} />}
              onClick={onPromote}
              disabled={promoting}
            >
              {rollback ? 'Roll back' : 'Make live'}
            </Button>
          )}
          {canDeleteDecisionVersion(version, versions) && (
            <Button size="compact-xs" variant="subtle" color="red" leftSection={<Trash2 size={12} />} onClick={onDelete}>
              Delete
            </Button>
          )}
        </Group>
      </Table.Td>
    </Table.Tr>
  );
}

/** "Make v2 live?", with exactly what that does. See decisionPromotionFacts. */
function PromoteConfirmation({
  target,
  versions,
  pending,
  onCancel,
  onConfirm,
}: {
  target: ApiDecisionVersion;
  versions: ApiDecisionVersion[];
  pending: boolean;
  onCancel: () => void;
  onConfirm: () => void;
}) {
  const rollback = isDecisionRollback(versions, target.version);
  return (
    <Alert color={rollback ? 'orange' : 'blue'} icon={<CircleDot size={16} />} radius="md">
      <Stack gap="xs">
        <Text size="sm" fw={600}>
          {rollback ? `Roll back to v${target.version}?` : `Make v${target.version} the live version?`}
        </Text>
        <Stack gap={2}>
          {decisionPromotionFacts(versions, target.version).map((fact) => (
            <Text key={fact} size="xs">
              {fact}
            </Text>
          ))}
        </Stack>
        <Group gap="xs">
          <Button size="compact-sm" variant="default" onClick={onCancel} disabled={pending}>
            Cancel
          </Button>
          <Button size="compact-sm" color={rollback ? 'orange' : 'indigo'} loading={pending} onClick={onConfirm}>
            Yes, make v{target.version} live
          </Button>
        </Group>
      </Stack>
    </Alert>
  );
}
