import { Button, Group, Modal, Stack, Text } from '@mantine/core';
import { useState } from 'react';

import { delegatedBy, reasonReady, reasonToSend, type ReasonRequest } from '../../domain/taskDelegation';
import { useTranslation } from '../../i18n/context';
import type { Task } from '../../services/types';
import { ReasonField } from './ReasonField';

interface ReasonFormProps {
  /** What will happen, in a sentence. */
  body: string;
  confirmLabel: string;
  onConfirm: (reason: string) => void;
  onCancel: () => void;
  /** The request has been sent and not yet answered. */
  busy?: boolean;
}

/** What the dialog holds: what will happen, why, and the two ways out. */
export function ReasonForm({ body, confirmLabel, onConfirm, onCancel, busy = false }: ReasonFormProps) {
  const { t } = useTranslation();
  const [reason, setReason] = useState('');
  return (
    <Stack py="md">
      <Text size="sm">{body}</Text>
      <ReasonField value={reason} onChange={setReason} focusOnOpen />
      <Group justify="flex-end" mt="xl">
        <Button variant="default" onClick={onCancel}>{t('common.cancel')}</Button>
        <Button
          color="indigo"
          onClick={() => onConfirm(reasonToSend(true, reason) ?? '')}
          disabled={!reasonReady(true, reason)}
          loading={busy}
        >
          {confirmLabel}
        </Button>
      </Group>
    </Stack>
  );
}

interface ReasonDialogProps {
  /** What is being asked about, or null when nothing is. */
  request: ReasonRequest<Task> | null;
  onConfirm: (request: ReasonRequest<Task>, reason: string) => void;
  onClose: () => void;
  busy?: boolean;
}

/**
 * Asks why before releasing or handing back a task somebody else holds.
 *
 * Its holder does either at one press. Anybody else — an administrator — is
 * changing whose work it is on somebody's behalf, and the server takes that
 * only with a reason, so the press opens this instead of earning a refusal.
 */
export function ReasonDialog({ request, onConfirm, onClose, busy = false }: ReasonDialogProps) {
  const { t } = useTranslation();
  const task = request?.task;
  const holder = task?.assignee?.username ?? '';
  const handingBack = request?.kind === 'handBack';
  const name = task?.name ?? '';

  return (
    <Modal
      opened={request !== null}
      onClose={onClose}
      title={<Text fw={700}>{t(handingBack ? 'handover.handBackForTitle' : 'handover.releaseTitle', { task: name })}</Text>}
      radius="md"
    >
      {request && task && (
        <ReasonForm
          key={`${request.kind}:${task.id}`}
          body={
            handingBack
              ? t('handover.handBackForBody', { delegate: holder, owner: delegatedBy(task) })
              : t('handover.releaseBody', { holder })
          }
          confirmLabel={t(handingBack ? 'handover.handBack' : 'handover.releaseConfirm')}
          onConfirm={(reason) => onConfirm(request, reason)}
          onCancel={onClose}
          busy={busy}
        />
      )}
    </Modal>
  );
}
