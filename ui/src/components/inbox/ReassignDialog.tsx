import { Button, Group, Modal, Select, Stack, Text } from '@mantine/core';
import { User } from 'lucide-react';
import { useState } from 'react';

import { reasonReady, reasonToSend, reassignReasonNeed, type Viewer } from '../../domain/taskDelegation';
import { useTranslation } from '../../i18n/context';
import type { Task } from '../../services/types';
import { ReasonField } from './ReasonField';

interface ReassignFormProps {
  task: Task;
  /** The signed-in person: who they are and what roles they hold. */
  viewer: Viewer | null;
  users: { value: string; label: string }[];
  assignee: string | null;
  onAssigneeChange: (assignee: string | null) => void;
  /** The reason is undefined when none was asked for, or none was written. */
  onConfirm: (assignee: string, reason?: string) => void;
  onCancel: () => void;
  /** The reassignment has been sent and not yet answered. */
  busy?: boolean;
}

/**
 * What the dialog holds: who the task goes to, and why.
 *
 * Why is asked of anybody but the task's holder, who is not shown the field —
 * unless they are an administrator, who may send the task to somebody it was
 * not offered to and has to say why when they do.
 */
export function ReassignForm({ task, viewer, users, assignee, onAssigneeChange, onConfirm, onCancel, busy = false }: ReassignFormProps) {
  const { t } = useTranslation();
  const [reason, setReason] = useState('');
  const need = reassignReasonNeed(task, viewer);

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
      {need !== 'none' && <ReasonField value={reason} onChange={setReason} optional={need === 'optional'} />}
      <Group justify="flex-end" mt="xl">
        <Button variant="default" onClick={onCancel}>{t('common.cancel')}</Button>
        <Button
          color="blue"
          onClick={() => {
            if (assignee) onConfirm(assignee, reasonToSend(need, reason));
          }}
          disabled={!assignee || !reasonReady(need, reason)}
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
