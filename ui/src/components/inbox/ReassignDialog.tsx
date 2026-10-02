import { Button, Group, Modal, Select, Stack, Text } from '@mantine/core';
import { User } from 'lucide-react';
import { useState } from 'react';

import { holdsTask, reasonReady, reasonToSend } from '../../domain/taskDelegation';
import { useTranslation } from '../../i18n/context';
import type { Task } from '../../services/types';
import { ReasonField } from './ReasonField';

interface ReassignFormProps {
  task: Task;
  /** The signed-in person's username. */
  viewer: string;
  users: { value: string; label: string }[];
  assignee: string | null;
  onAssigneeChange: (assignee: string | null) => void;
  /** The reason is undefined for the task's holder, who is not asked for one. */
  onConfirm: (assignee: string, reason?: string) => void;
  onCancel: () => void;
  /** The reassignment has been sent and not yet answered. */
  busy?: boolean;
}

/**
 * What the dialog holds: who the task goes to and, from anybody but its
 * holder, why.
 */
export function ReassignForm({ task, viewer, users, assignee, onAssigneeChange, onConfirm, onCancel, busy = false }: ReassignFormProps) {
  const { t } = useTranslation();
  const [reason, setReason] = useState('');
  // Somebody moving their own task need not explain it; the server refuses
  // anybody else who does not.
  const needsReason = !holdsTask(task, viewer);

  return (
    <Stack py="md">
      <Select
        label={t('handover.newAssignee')}
        placeholder={t('handover.selectUser')}
        description={t('handover.newAssigneeHelp')}
        data={users}
        value={assignee}
        onChange={onAssigneeChange}
        searchable
        clearable
      />
      {needsReason && <ReasonField value={reason} onChange={setReason} />}
      <Group justify="flex-end" mt="xl">
        <Button variant="default" onClick={onCancel}>{t('common.cancel')}</Button>
        <Button
          color="blue"
          onClick={() => {
            if (assignee) onConfirm(assignee, reasonToSend(needsReason, reason));
          }}
          disabled={!assignee || !reasonReady(needsReason, reason)}
          loading={busy}
        >
          {t('handover.confirmReassign')}
        </Button>
      </Group>
    </Stack>
  );
}

type ReassignDialogProps = Omit<ReassignFormProps, 'task' | 'onCancel'> & {
  opened: boolean;
  /** The task being reassigned, or null when none is. */
  task: Task | null;
  onClose: () => void;
};

/** Gives a task to somebody else. */
export function ReassignDialog({ opened, task, onClose, ...form }: ReassignDialogProps) {
  const { t } = useTranslation();
  return (
    <Modal
      opened={opened}
      onClose={onClose}
      title={
        <Group gap="xs">
          <User size={18} color="var(--mantine-color-blue-6)" aria-hidden />
          <Text fw={700}>{t('handover.reassignTitle', { task: task?.name ?? '' })}</Text>
        </Group>
      }
      radius="md"
    >
      {task && <ReassignForm key={task.id} task={task} onCancel={onClose} {...form} />}
    </Modal>
  );
}
