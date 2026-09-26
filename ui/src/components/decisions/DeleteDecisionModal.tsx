/**
 * "Delete this decision?", with what still depends on it.
 *
 * What depends on a decision is fetched only once the question is asked: it
 * walks every process of the project, which is right for a deliberate question
 * and wrong for every row of a list.
 */
import { Alert, Button, Group, List, Loader, Modal, Stack, Text } from '@mantine/core';
import { notifications } from '@mantine/notifications';
import { AlertTriangle, Trash2 } from 'lucide-react';

import { useDecisionImpact, useDeleteDecision } from '../../hooks/useDecisions';
import { errorMessage } from '../../services/shared/errors';
import type { ApiDecision } from '../../services/types';

export function DeleteDecisionModal({
  decision,
  onClose,
  onDeleted,
}: {
  /** The decision waiting for "yes, delete it"; null when nothing is. */
  decision: Pick<ApiDecision, 'id' | 'key' | 'name'> | null;
  onClose: () => void;
  onDeleted: () => void;
}) {
  const deleteDecision = useDeleteDecision();
  const { data: impact, isLoading: impactLoading } = useDecisionImpact(decision?.id ?? null);

  const confirmDelete = async () => {
    if (!decision) return;
    const { id, name } = decision;
    try {
      await deleteDecision.mutateAsync(id);
      notifications.show({ title: 'Deleted', message: `${name} is gone.`, color: 'green' });
      onDeleted();
    } catch (err: unknown) {
      notifications.show({ title: `Could not delete ${name}`, message: errorMessage(err, 'It was not deleted.'), color: 'red' });
    }
  };

  return (
    <Modal opened={decision !== null} onClose={onClose} title={<Text fw={700}>Delete {decision?.name}?</Text>} radius="lg">
      <Stack gap="md">
        {impactLoading ? (
          <Group gap="xs">
            <Loader size="xs" />
            <Text size="sm" c="dimmed">
              Checking what uses it…
            </Text>
          </Group>
        ) : impact && impact.running_instances > 0 ? (
          <Alert
            color="red"
            icon={<AlertTriangle size={16} />}
            title={`${impact.running_instances} running ${impact.running_instances === 1 ? 'instance is' : 'instances are'} on their way to it`}
          >
            <Text size="sm">They will fail at the step that evaluates it.</Text>
            {(impact.processes ?? []).length > 0 && (
              <List size="sm" mt="xs">
                {(impact.processes ?? []).map((p) => (
                  <List.Item key={`${p.definition_id}-${p.version}`}>
                    {p.definition_name || p.definition_key} v{p.version}
                    {p.running_instances > 0 ? ` — ${p.running_instances} running` : ''}
                  </List.Item>
                ))}
              </List>
            )}
          </Alert>
        ) : (
          <Text size="sm">No running instance is using it right now.</Text>
        )}
        <Text size="sm">
          Any process step that names <b>{decision?.key}</b> fails the next time it runs. This cannot be undone.
        </Text>
        <Group justify="flex-end">
          <Button variant="default" onClick={onClose}>
            Cancel
          </Button>
          <Button
            color="red"
            leftSection={<Trash2 size={14} />}
            onClick={confirmDelete}
            loading={deleteDecision.isPending}
            disabled={impactLoading}
          >
            Delete
          </Button>
        </Group>
      </Stack>
    </Modal>
  );
}
