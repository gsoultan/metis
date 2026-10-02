import { Button, Group, Modal, NumberInput, Stack, Text, TextInput } from '@mantine/core';
// Imported here rather than at the root: the date-picker stylesheet is only
// needed where a picker is, and importing it eagerly pulled the whole
// @mantine/dates chunk (~19 kB gzipped) onto the critical path for every
// visitor, picker or not.
import '@mantine/dates/styles.css';
import { DateInput } from '@mantine/dates';
import { Edit2 } from 'lucide-react';
import { useState } from 'react';

import { holdsTask, reasonReady, reasonToSend } from '../../domain/taskDelegation';
import { useTranslation } from '../../i18n/context';
import type { Task } from '../../services/types';
import { ReasonField } from './ReasonField';

interface EditTaskFormProps {
  /** The task as it is being edited. */
  task: Task;
  /** The signed-in person's username. */
  viewer: string;
  onChange: (task: Task) => void;
  /** The reason is undefined for the task's holder, who is not asked for one. */
  onSave: (reason?: string) => void;
  onCancel: () => void;
  saving?: boolean;
}

/**
 * What the dialog holds: the task's name, priority and due date and, from
 * anybody but its holder, why they are changing them.
 */
export function EditTaskForm({ task, viewer, onChange, onSave, onCancel, saving = false }: EditTaskFormProps) {
  const { t } = useTranslation();
  const [reason, setReason] = useState('');
  // They are how the holder orders their own day; anybody else changing them
  // is refused by the server unless they say why.
  const needsReason = !holdsTask(task, viewer);

  return (
    <Stack py="md">
      <TextInput
        label={t('handover.taskName')}
        value={task.name}
        onChange={(event) => onChange({ ...task, name: event.currentTarget.value })}
      />
      <NumberInput
        label={t('handover.priority')}
        value={task.priority}
        onChange={(value) => onChange({ ...task, priority: Number(value) || 0 })}
      />
      <DateInput
        label={t('handover.dueDate')}
        value={task.dueDate ? new Date(task.dueDate) : null}
        onChange={(date: string | Date | null) => {
          onChange({ ...task, dueDate: date ? new Date(date).toISOString() : '' });
        }}
        clearable
      />
      {needsReason && <ReasonField value={reason} onChange={setReason} />}
      <Group justify="flex-end" mt="xl">
        <Button variant="default" onClick={onCancel}>{t('common.cancel')}</Button>
        <Button
          color="blue"
          onClick={() => onSave(reasonToSend(needsReason, reason))}
          disabled={!reasonReady(needsReason, reason)}
          loading={saving}
        >
          {t('handover.saveChanges')}
        </Button>
      </Group>
    </Stack>
  );
}

type EditTaskDialogProps = Omit<EditTaskFormProps, 'task' | 'onCancel'> & {
  /** The task being edited, or null when none is. */
  task: Task | null;
  onClose: () => void;
};

/** Changes a task's name, priority or due date. */
export function EditTaskDialog({ task, onClose, ...form }: EditTaskDialogProps) {
  const { t } = useTranslation();
  return (
    <Modal
      opened={task !== null}
      onClose={onClose}
      title={
        <Group gap="xs">
          <Edit2 size={18} color="var(--mantine-color-blue-6)" aria-hidden />
          <Text fw={700}>{t('handover.editTitle', { task: task?.name ?? '' })}</Text>
        </Group>
      }
      size="md"
      radius="md"
    >
      {task && <EditTaskForm key={task.id} task={task} onCancel={onClose} {...form} />}
    </Modal>
  );
}
